package renderer

import (
	"archive/tar"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

//go:embed all:files/*
var hugoFiles embed.FS

func copyFiles(fs embed.FS, src string, dst string) error {
	files, err := hugoFiles.ReadDir(src)

	if err != nil {
		return errors.Wrapf(err, "unable to open files directory %q", src)
	}

	for _, file := range files {
		srcFile := path.Join(src, file.Name())
		dstFile := path.Join(dst, file.Name())

		if file.IsDir() {
			if err = os.MkdirAll(dstFile, 0775); err != nil {
				return errors.Wrapf(err, "unable to create workshop directory %q", dstFile)
			}

			if err = copyFiles(fs, srcFile, dstFile); err != nil {
				return err
			}
		} else {
			input, err := fs.ReadFile(srcFile)

			if err != nil {
				return errors.Wrapf(err, "unable to open source file %q", srcFile)
			}

			err = ioutil.WriteFile(dstFile, input, 0644)

			if err != nil {
				return errors.Wrapf(err, "unable to create source file %q", dstFile)
			}
		}
	}

	return nil
}

type WorkshopParamsConfig struct {
	Name    string   `yaml:"name"`
	Value   string   `yaml:"value"`
	Aliases []string `yaml:"aliases,omitempty"`
}

type WorkshopPathConfig struct {
	Title       string                 `yaml:"title,omitempty"`
	Description string                 `yaml:"description,omitempty"`
	Params      []WorkshopParamsConfig `yaml:"params,omitempty"`
	Steps       []string               `yaml:"steps,omitempty"`
}

type WorkshopModuleConfig struct {
	Title    string `yaml:"title"`
	Path     string `yaml:"path"`
	PrevPage string `yaml:"prev_page"`
	NextPage string `yaml:"next_page"`
	Step     int    `yaml:"step"`
}

type WorkshopPathwaysConfig struct {
	Default string                          `yaml:"default,omitempty"`
	Paths   map[string]WorkshopPathConfig   `yaml:"paths,omitempty"`
	Modules map[string]WorkshopModuleConfig `yaml:"modules,omitempty"`
}

type WorkshopConfig struct {
	Pathways WorkshopPathwaysConfig `yaml:"pathways,omitempty"`
	Params   []WorkshopParamsConfig `yaml:"params,omitempty"`
}

// WorkshopParams returns the Hugo parameters a workshop session takes from
// the Workshop definition and from the workshop configuration, config.yaml in
// workshopDir, as the workshop-variables.yaml template of the workshop base
// image sets them: the top-level params and those of the selected pathway,
// the name of that pathway, and the workshop title and description, replaced
// by those of the pathway when it gives them. Values only a running session
// has are left out. A non-empty requestedPathway selects that pathway as a
// session started with PATHWAY_NAME set to it would, in place of PATHWAY_NAME
// from the Workshop definition and the default pathway. It must name a pathway
// the workshop configuration defines.
func WorkshopParams(workshopDir string, workshop *unstructured.Unstructured, requestedPathway string) (map[string]string, error) {
	workshopConfig, _, err := readWorkshopConfig(workshopDir)

	if err != nil {
		return nil, err
	}

	environ := sessionEnvironment(workshop)

	if requestedPathway != "" {
		if _, found := workshopConfig.Pathways.Paths[requestedPathway]; !found {
			return nil, undefinedPathwayError(workshopConfig, requestedPathway)
		}

		environ["PATHWAY_NAME"] = requestedPathway
	}

	pathwayName := workshopConfig.selectedPathway(environ["PATHWAY_NAME"])

	pathway := workshopConfig.Pathways.Paths[pathwayName]

	params := map[string]string{}

	for _, items := range [][]WorkshopParamsConfig{workshopConfig.Params, pathway.Params} {
		for _, item := range items {
			// A param listing aliases takes its value from the first of them
			// that is set, in place of the variable named like the param.

			sources := []string{item.Name}

			if item.Aliases != nil {
				sources = item.Aliases
			}

			params[item.Name] = item.Value

			for _, source := range sources {
				if value, found := environ[source]; found {
					params[item.Name] = value
					break
				}
			}
		}
	}

	workshopTitle, found, _ := unstructured.NestedString(workshop.Object, "spec", "title")
	workshopDescription, _, _ := unstructured.NestedString(workshop.Object, "spec", "description")

	if !found {
		workshopTitle = "Workshop"
	}

	if pathway.Title != "" {
		workshopTitle = pathway.Title
	}

	if pathway.Description != "" {
		workshopDescription = pathway.Description
	}

	params["pathway_name"] = pathwayName
	params["workshop_title"] = workshopTitle
	params["workshop_description"] = workshopDescription

	return params, nil
}

// readWorkshopConfig reads the workshop configuration, config.yaml in
// workshopDir, and reports whether there is one. A file which can't be read
// is taken not to exist.
func readWorkshopConfig(workshopDir string) (WorkshopConfig, bool, error) {
	workshopConfig := WorkshopConfig{}

	workshopConfigData, err := os.ReadFile(filepath.Join(workshopDir, "config.yaml"))

	if err != nil {
		return workshopConfig, false, nil
	}

	if err = yaml.Unmarshal(workshopConfigData, &workshopConfig); err != nil {
		return workshopConfig, false, errors.Wrapf(err, "unable to unpack workshop config")
	}

	return workshopConfig, true, nil
}

// undefinedPathwayError reports that the workshop configuration defines no
// pathway named pathwayName, listing the pathways it does define.
func undefinedPathwayError(workshopConfig WorkshopConfig, pathwayName string) error {
	if len(workshopConfig.Pathways.Paths) == 0 {
		return errors.Errorf("pathway %q is not defined in config.yaml, which defines no pathways", pathwayName)
	}

	names := make([]string, 0, len(workshopConfig.Pathways.Paths))

	for name := range workshopConfig.Pathways.Paths {
		names = append(names, name)
	}

	sort.Strings(names)

	return errors.Errorf("pathway %q is not defined in config.yaml, which defines the pathways: %s", pathwayName, strings.Join(names, ", "))
}

// selectedPathway returns the name of the pathway a workshop session selects
// given the name it is asked for: that name, or when empty and there are
// pathways, the name of the default pathway.
func (c *WorkshopConfig) selectedPathway(pathwayName string) string {
	if pathwayName == "" && len(c.Pathways.Paths) != 0 {
		return c.Pathways.Default
	}

	return pathwayName
}

// sessionEnvironment returns the environment variables spec.session.env of
// the Workshop definition gives a workshop session. An entry taking its value
// from valueFrom, or whose value references a variable as $(name), is left
// out, since only a running session can work out its value. It still
// replaces an earlier entry of the same name, as it would in a session.
func sessionEnvironment(workshop *unstructured.Unstructured) map[string]string {
	environ := map[string]string{}

	env, _, _ := unstructured.NestedSlice(workshop.Object, "spec", "session", "env")

	for _, entry := range env {
		variable, ok := entry.(map[string]interface{})

		if !ok {
			continue
		}

		name, _, _ := unstructured.NestedString(variable, "name")
		value, _, _ := unstructured.NestedString(variable, "value")

		_, hasValueFrom := variable["valueFrom"]

		if hasValueFrom || strings.Contains(value, "$(") {
			delete(environ, name)
			continue
		}

		environ[name] = value
	}

	return environ
}

var workshopSessionResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshopsessions"}

func fetchWorkshopSessionAndValidate(kubeconfig string, kubeContext string, workshop string, portal string, session string) (string, string, error) {
	// Returns session URL, config password and error.

	var err error

	clusterConfig := cluster.NewClusterConfig(kubeconfig, kubeContext)

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return "", "", errors.Wrapf(err, "unable to create Kubernetes client")
	}

	workshopSessionClient := dynamicClient.Resource(workshopSessionResource)

	workshopSession, err := workshopSessionClient.Get(context.TODO(), session, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return "", "", errors.New("no workshop session can be found")
	}

	linkedWorkshop, _, _ := unstructured.NestedString(workshopSession.Object, "spec", "workshop", "name")

	if linkedWorkshop != workshop {
		return "", "", errors.New("workshop session not linked to target workshop")
	}

	linkedPortal, _, _ := unstructured.NestedString(workshopSession.Object, "spec", "portal", "name")

	if linkedPortal != portal {
		return "", "", errors.New("workshop session not linked to target portal")
	}

	client, err := clusterConfig.GetClient()

	if err != nil {
		return "", "", errors.Wrapf(err, "unable to create Kubernetes client")
	}

	password, err := sessionConfigPassword(context.TODO(), client, workshopSession)

	if err != nil {
		return "", "", err
	}

	sessionURL, _, _ := unstructured.NestedString(workshopSession.Object, "status", "educates", "url")

	if password == "" {
		return "", "", errors.New("cannot determine config password for session")
	}

	if sessionURL == "" {
		return "", "", errors.New("cannot determine url for accessing workshop session")
	}

	return sessionURL, password, nil
}

func fetchSessionVariables(sessionURL string, password string) (map[string]string, error) {
	var err error

	params := make(map[string]string)

	url := fmt.Sprintf("%s/config/variables", sessionURL)

	req, err := http.NewRequest("GET", url, nil)

	if err != nil {
		return params, errors.Wrapf(err, "cannot construct request to query workshop session config")
	}

	q := req.URL.Query()
	q.Add("token", password)
	req.URL.RawQuery = q.Encode()

	res, err := http.DefaultClient.Do(req)

	if err != nil {
		return params, errors.Wrapf(err, "cannot query workshop session config")
	}

	if res.StatusCode != 200 {
		return params, errors.New("unexpected failure querying workshop session config")
	}

	resBody, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return params, errors.Wrapf(err, "failed to read workshop session config")
	}

	err = json.Unmarshal(resBody, &params)

	if err != nil {
		return params, errors.Wrapf(err, "unable to unpack workshop session parameters")
	}

	return params, nil
}

// portalThemeFiles are the portal theme files the educates Hugo theme uses in
// workshop instructions.
var portalThemeFiles = []string{
	"workshop-instructions.css",
	"workshop-instructions.js",
	"workshop-instructions.html",
}

// fetchPortalTheme downloads the portal theme files from the workshop session
// at sessionURL into dir. The gateway serves them without authentication. A
// file the session doesn't have is written empty, and every file is emptied
// before fetching starts, so that no file from an earlier session remains.
func fetchPortalTheme(sessionURL string, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.Wrapf(err, "unable to create portal theme directory")
	}

	for _, name := range portalThemeFiles {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			return errors.Wrapf(err, "unable to write portal theme file %s", name)
		}
	}

	// The gateway answers a theme file it doesn't have with a redirect to
	// its login page, so redirects are not followed.

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, name := range portalThemeFiles {
		res, err := client.Get(fmt.Sprintf("%s/static/theme/%s", sessionURL, name))

		if err != nil {
			return errors.Wrapf(err, "cannot query portal theme file %s", name)
		}

		var data []byte

		switch {
		case res.StatusCode == http.StatusOK:
			data, err = io.ReadAll(res.Body)

			if err != nil {
				err = errors.Wrapf(err, "failed to read portal theme file %s", name)
			}
		case res.StatusCode == http.StatusNotFound, res.StatusCode >= 300 && res.StatusCode < 400:
		default:
			err = errors.Errorf("unexpected status %d querying portal theme file %s", res.StatusCode, name)
		}

		res.Body.Close()

		if err != nil {
			return err
		}

		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return errors.Wrapf(err, "unable to write portal theme file %s", name)
		}
	}

	return nil
}

// GenerateHugoConfiguration writes the Hugo configuration for workshopDir to
// hugo.yaml in target. When themeDir is not empty it holds the portal theme
// files, which are mounted into the site as they are in a workshop session.
func GenerateHugoConfiguration(workshopDir string, target string, params map[string]string, sessionURL string, themeDir string) error {
	var err error

	// Read user workshop config with details of any pathways.

	workshopConfig, workshopConfigExists, err := readWorkshopConfig(workshopDir)

	if err != nil {
		return err
	}

	activeModules := map[string]*WorkshopModuleConfig{}

	if workshopConfigExists {
		// Use the pathway name calculated from the workshop session and if
		// not defined fallback to the using default pathway name if specified.

		pathwayName := workshopConfig.selectedPathway(params["pathway_name"])

		params["__first_page__"] = ""

		pathway, pathwayExists := workshopConfig.Pathways.Paths[pathwayName]

		if pathwayName != "" && pathwayExists && len(pathway.Steps) != 0 {
			modules := workshopConfig.Pathways.Modules

			firstPage := ""
			prevPage := ""

			for index, step := range pathway.Steps {
				if firstPage == "" {
					firstPage = step
				}

				module, moduleExists := modules[step]

				if !moduleExists {
					module = WorkshopModuleConfig{}
				}

				module.Path = step

				if prevPage != "" {
					module.PrevPage = prevPage
					activeModules[prevPage].NextPage = step
				} else {
					module.PrevPage = ""
				}

				module.NextPage = ""
				module.Step = index + 1

				prevPage = step

				activeModules[step] = &module
			}

			params["__first_page__"] = firstPage
		}
	}

	type OutputFormatConfig struct {
		BaseName  string `yaml:"baseName"`
		MediaType string `yaml:"mediaType"`
		IsHTML    bool   `yaml:"isHTML"`
	}

	type MountConfig struct {
		Source string `yaml:"source"`
		Target string `yaml:"target"`
	}

	type ModuleConfig struct {
		Mounts []MountConfig `yaml:"mounts"`
	}

	type HugoConfig struct {
		BaseURL       string                        `yaml:"baseURL"`
		Params        map[string]interface{}        `yaml:"params"`
		OutputFormats map[string]OutputFormatConfig `yaml:"outputFormats"`
		Outputs       map[string][]string           `yaml:"outputs"`
		Module        *ModuleConfig                 `yaml:"module,omitempty"`
	}

	config := HugoConfig{Params: make(map[string]interface{})}

	// Mount the portal theme the same way as hugo-configuration.yaml does in
	// the workshop image. Declaring a static mount replaces Hugo's default
	// one, so the workshop's own static directory is mounted again.

	if themeDir != "" {
		config.Module = &ModuleConfig{
			Mounts: []MountConfig{
				{Source: "static", Target: "static"},
				{Source: themeDir, Target: "static/static/theme"},
				{Source: themeDir, Target: "assets/theme"},
			},
		}
	}

	config.BaseURL = fmt.Sprintf("%s/workshop/content/", sessionURL)

	config.OutputFormats = map[string]OutputFormatConfig{
		"print": {
			BaseName:  "print",
			MediaType: "text/html",
			IsHTML:    true,
		},
	}

	config.Outputs = map[string][]string{
		"home": {"HTML", "print"},
	}

	for paramName, paramValue := range params {
		config.Params[paramName] = paramValue
	}

	config.Params["__modules__"] = activeModules

	configData, err := yaml.Marshal(config)

	if err != nil {
		return errors.Wrapf(err, "unable to marshal hugo configuration")
	}

	if err != nil {
		return errors.Wrapf(err, "unable to create hugo files directory")
	}

	targetFile := filepath.Join(target, "hugo.yaml")
	workingFile := filepath.Join(target, "hugo.yaml.tmp")

	configFile, err := os.Create(workingFile)

	if err != nil {
		return errors.Wrapf(err, "unable to create working hugo config file")
	}

	_, err = configFile.Write(configData)

	if err != nil {
		return errors.Wrapf(err, "unable to write working hugo config file")
	}

	configFile.Close()

	err = os.Rename(workingFile, targetFile)

	if err != nil {
		return errors.Wrapf(err, "unable to update hugo config file")
	}

	return nil
}

func startHugoServer(workshopDir string, tempDir string, port int, sessionURL string) error {
	// Run this in a go routine.

	wsPort := 80

	if strings.HasPrefix(sessionURL, "https://") {
		wsPort = 443
	}

	commandArgs := []string{
		"server",
		"--source", workshopDir,
		"--port", strconv.Itoa(port),
		"--disableFastRender",
		"--liveReloadPort", fmt.Sprintf("%d", wsPort),
		"--config", filepath.Join(tempDir, "hugo.yaml"),
		"--themesDir", filepath.Join(tempDir, "themes"),
		"--theme", "educates",
		"--watch",
	}

	commandPath, err := exec.LookPath("hugo")

	if err != nil {
		fmt.Println("ERROR: Unable to find hugo program")
		return errors.Wrapf(err, "unable to find hugo program")
	}

	command := exec.Command(commandPath, commandArgs...)

	stdout, err := command.StdoutPipe()
	command.Stderr = command.Stdout

	if err != nil {
		return errors.Wrapf(err, "unable to create command output pipe")
	}

	if err = command.Start(); err != nil {
		return errors.Wrapf(err, "failed to execute hugo program")
	}

	for {
		tmp := make([]byte, 1024)
		_, err := stdout.Read(tmp)
		fmt.Print(string(tmp))
		if err != nil {
			break
		}
	}

	return nil
}

func PopulateTemporaryDirectory() (string, error) {
	tempDir, err := ioutil.TempDir("", "educates")

	if err != nil {
		return "", errors.Wrapf(err, "unable to create hugo files directory")
	}

	err = copyFiles(hugoFiles, "files", tempDir)

	if err != nil {
		return "", errors.Wrapf(err, "failed to copy hugo files")
	}

	return tempDir, nil
}

type ServerCleanupFunc func()

func RunHugoServer(workshopRoot string, kubeconfig string, context string, workshop string, portal string, localHost string, localPort int, hugoPort int, token string, files bool, cleanupFunc ServerCleanupFunc) error {
	var err error
	var tempDir string

	workshopDir := filepath.Join(workshopRoot, "workshop")

	// First create directory to hold unpacked files for Hugo to use.

	if tempDir, err = PopulateTemporaryDirectory(); err != nil {
		return err
	}

	defer os.RemoveAll(tempDir)

	// Also catch signals so we can try and cleanup temporary directory.

	c := make(chan os.Signal, 1)

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("Cleaning up...")

		os.RemoveAll(tempDir)

		if cleanupFunc != nil {
			cleanupFunc()
		}

		os.Exit(1)
	}()

	// Now need to create a mini HTTP server to handle requests.

	var serverDetailsLock sync.Mutex

	var hugoStarted bool = false
	var lastSessionName = ""

	proxyHandler := func(w http.ResponseWriter, r *http.Request) {
		// If an access token is provided validate it.

		if token != "" {
			accessToken := r.Header.Get("X-Access-Token")

			if accessToken != token {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte("403 - Invalid access token"))

				return
			}
		}

		// Request must provide session name via header.

		sessionName := r.Header.Get("X-Session-Name")

		if sessionName == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("400 - Session name required"))

			return
		}

		// If session name not the same as last seen, regenerate files.

		serverDetailsLock.Lock()

		if sessionName != lastSessionName {
			// First validate that can access workshop session.

			sessionURL, password, err := fetchWorkshopSessionAndValidate(kubeconfig, context, workshop, portal, sessionName)

			if err != nil {
				fmt.Println("Error validating workshop session:", err)

				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - Unable to validate workshop session"))

				serverDetailsLock.Unlock()

				return
			}

			// Then fetch back the session variables for the workshop session.

			params, err := fetchSessionVariables(sessionURL, password)

			if err != nil {
				fmt.Println("Error fetching session variables:", err)

				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - Unable to fetch workshop session config"))

				serverDetailsLock.Unlock()

				return
			}

			// Fetch the portal theme files of the workshop session. The
			// instructions render without them, so a failure only warns.

			themeDir := filepath.Join(tempDir, "theme")

			if err := fetchPortalTheme(sessionURL, themeDir); err != nil {
				fmt.Println("Unable to fetch portal theme:", err)
			}

			// Generate (or regenerate) the Hugo configuration.

			err = GenerateHugoConfiguration(workshopDir, tempDir, params, sessionURL, themeDir)

			if err != nil {
				fmt.Println("Unable to generate Hugo configuration:", err)

				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - Unable to generate server configuration"))

				serverDetailsLock.Unlock()

				return
			}

			// If Hugo server is not already running it, start it. Add short
			// delays to give the Hugo server time to start or reload.

			if !hugoStarted {
				fmt.Println("Starting Hugo server")

				go startHugoServer(workshopDir, tempDir, hugoPort, sessionURL)

				time.Sleep(4 * time.Second)

				hugoStarted = true
			} else {
				time.Sleep(2 * time.Second)
			}

			// Update last seen session name.

			lastSessionName = sessionName
		}

		serverDetailsLock.Unlock()

		hugoServerURL := fmt.Sprintf("http://localhost:%d", hugoPort)
		target, _ := url.Parse(hugoServerURL)

		proxy := httputil.NewSingleHostReverseProxy(target)

		proxyPass := func(res http.ResponseWriter, req *http.Request) {
			proxy.ServeHTTP(w, req)
		}

		proxyPass(w, r)
	}

	http.HandleFunc("/workshop/content/", proxyHandler)

	filesHandler := func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			accessToken := r.URL.Query().Get("token")

			if accessToken != token {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte("403 - Invalid access token"))

				return
			}
		}

		w.Header().Set("Content-Type", "application/x-tar")

		tw := tar.NewWriter(w)

		filepath.Walk(workshopRoot, func(file string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			header, err := tar.FileInfoHeader(fi, file)
			if err != nil {
				return err
			}

			header.Name, err = filepath.Rel(workshopRoot, filepath.ToSlash(file))

			if err != nil {
				return err
			}

			if header.Name == ".git" || strings.HasPrefix(header.Name, ".git/") {
				return nil
			}

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if !fi.IsDir() {
				data, err := os.Open(file)
				if err != nil {
					return err
				}
				if _, err := io.Copy(tw, data); err != nil {
					return err
				}
			}

			return nil
		})
	}

	if files {
		http.HandleFunc("/workshop/files.tar", filesHandler)
	}

	portString := fmt.Sprintf("%s:%d", localHost, localPort)

	fmt.Println("Proxy listening on:", portString)

	log.Fatal(http.ListenAndServe(portString, nil))

	return nil
}

func RenderHugoStaticHTML(workshopDir string, tempDir string) error {
	commandArgs := []string{
		"build",
		"--source", workshopDir,
		"--config", filepath.Join(tempDir, "hugo.yaml"),
		"--themesDir", filepath.Join(tempDir, "themes"),
		"--theme", "educates",
		"--destination", filepath.Join(tempDir, "public"),
		"--logLevel", "debug",
		"--baseURL", "",
		"--ignoreCache",
		"--cleanDestinationDir",
		// "--minify",
	}

	commandPath, err := exec.LookPath("hugo")

	if err != nil {
		fmt.Println("ERROR: Unable to find hugo program")
		return errors.Wrapf(err, "unable to find hugo program")
	}

	command := exec.Command(commandPath, commandArgs...)

	stdout, err := command.StdoutPipe()
	command.Stderr = command.Stdout

	if err != nil {
		return errors.Wrapf(err, "unable to create command output pipe")
	}

	if err = command.Start(); err != nil {
		return errors.Wrapf(err, "failed to execute hugo program")
	}

	for {
		tmp := make([]byte, 1024)
		_, err := stdout.Read(tmp)
		fmt.Print(string(tmp))
		if err != nil {
			break
		}
	}

	command.Wait()

	if err != nil {
		return errors.Wrapf(err, "failed to execute hugo program")
	}

	status := command.ProcessState.ExitCode()

	if status != 0 {
		return errors.New("hugo build failed")
	}

	return nil
}
