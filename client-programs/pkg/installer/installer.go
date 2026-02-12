package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/logger"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"

	"github.com/pkg/errors"

	"carvel.dev/imgpkg/pkg/imgpkg/registry"
	imgpkgv1 "carvel.dev/imgpkg/pkg/imgpkg/v1"

	"carvel.dev/kapp/pkg/kapp/cmd/app"

	cmdtpl "carvel.dev/ytt/pkg/cmd/template"
	yttUI "carvel.dev/ytt/pkg/cmd/ui"
	"carvel.dev/ytt/pkg/files"

	kbldcmd "carvel.dev/kbld/pkg/kbld/cmd"
	kbldlog "carvel.dev/kbld/pkg/kbld/logger"

	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// We use a NullWriter to suppress the output of some commands, like kbld
type NullWriter int

func (NullWriter) Write([]byte) (int, error) { return 0, nil }

type Installer struct {
}

func NewInstaller() *Installer {
	return &Installer{}
}

func (inst *Installer) DryRun(version string, packageRepository string, fullConfig *config.InstallationConfig, verbose bool, showPackagesValues bool, skipImageResolution bool) error {
	fmt.Println("⚙️ Running dry run installation...")

	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", constants.EducatesInstallerString)
	if err != nil {
		return err
	}
	// if verbose {
	// 	fmt.Println("Temp dir: ", tempDir)
	// }

	defer os.RemoveAll(tempDir) // clean up

	// Hack for local development. When version=latest, we use:
	// - localhost:5001 as the package repository
	// - 0.0.1 as the version
	// - skipImageResolution=true
	if version == "latest" {
		packageRepository = "localhost:5001"
		version = "0.0.1"
		skipImageResolution = true
	}

	// Fetch
	prevDir, err := inst.fetch(tempDir, version, packageRepository, fullConfig, verbose)
	if err != nil {
		return err
	}

	// Template
	prevDir, err = inst.template(tempDir, prevDir, fullConfig, verbose, showPackagesValues, skipImageResolution)
	if err != nil {
		return err
	}

	// kbld
	if !skipImageResolution {
		prevDir, err = inst.resolve(tempDir, prevDir, verbose)
		if err != nil {
			return err
		}
	}

	err = utils.PrintYamlFilesInDir(prevDir, []string{})
	if err != nil {
		return err
	}

	return nil
}

func (inst *Installer) Run(version string, packageRepository string, fullConfig *config.InstallationConfig, clusterConfig *cluster.ClusterConfig, verbose bool, showPackagesValues bool, skipImageResolution bool, showDiff bool) error {
	fmt.Println("⚙️ Running full installation...")

	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", constants.EducatesInstallerString)
	if err != nil {
		return err
	}
	// if verbose {
	// 	fmt.Println("Temp dir: ", tempDir)
	// }

	defer os.RemoveAll(tempDir) // clean up

	// Hack for local development. When version=latest, we use:
	// - localhost:5001 as the package repository
	// - 0.0.1 as the version
	// - skipImageResolution=true
	if version == "latest" {
		packageRepository = "localhost:5001"
		version = "0.0.1"
		skipImageResolution = true
	}

	// Fetch
	prevDir, err := inst.fetch(tempDir, version, packageRepository, fullConfig, verbose)
	if err != nil {
		return err
	}

	// Template
	prevDir, err = inst.template(tempDir, prevDir, fullConfig, verbose, showPackagesValues, skipImageResolution)
	if err != nil {
		return err
	}

	// kbld for image resolution
	if !skipImageResolution {
		prevDir, err = inst.resolve(tempDir, prevDir, verbose)
		if err != nil {
			return err
		}
	}

	// Deploy
	err = inst.deploy(tempDir, prevDir, clusterConfig, verbose, showDiff)
	if err != nil {
		return err
	}

	return nil
}

func (inst *Installer) Delete(fullConfig *config.InstallationConfig, clusterConfig *cluster.ClusterConfig, verbose bool) error {
	fmt.Println("Deleting educates ...")

	if err := inst.delete(clusterConfig); err != nil {
		return err
	}

	return nil
}

func (inst *Installer) GetValuesFromCluster(kubeconfig string, kubeContext string) (string, error) {
	clusterConfig := cluster.NewClusterConfig(kubeconfig, kubeContext)

	client, err := clusterConfig.GetClient()

	if err != nil {
		return "", errors.Wrapf(err, "unable to create Kubernetes client")
	}

	configMapClient := client.CoreV1().ConfigMaps(constants.EducatesConfigNamespace)

	values, err := configMapClient.Get(context.TODO(), constants.EducatesConfigConfigMapName, metav1.GetOptions{})

	if err != nil {
		return "", errors.Wrap(err, "error querying the cluster")
	}

	valuesData, ok := values.Data[constants.EducatesProcessedValuesKey]

	if !ok {
		return "", errors.New("no platform configuration found")
	}

	return string(valuesData), nil
}

func (inst *Installer) GetConfigFromCluster(kubeconfig string, kubeContext string) (string, error) {
	clusterConfig := cluster.NewClusterConfig(kubeconfig, kubeContext)

	client, err := clusterConfig.GetClient()

	if err != nil {
		return "", errors.Wrapf(err, "unable to create Kubernetes client")
	}

	configMapClient := client.CoreV1().ConfigMaps(constants.EducatesConfigNamespace)

	values, err := configMapClient.Get(context.TODO(), constants.EducatesConfigConfigMapName, metav1.GetOptions{})

	if err != nil {
		return "", errors.Wrap(err, "error querying the cluster")
	}

	valuesData, ok := values.Data[constants.EducatesOriginalConfigKey]

	if !ok {
		return "", errors.New("no platform configuration found")
	}

	return string(valuesData), nil
}

func (inst *Installer) fetch(tempDir string, version string, packageRepository string, fullConfig *config.InstallationConfig, verbose bool) (string, error) {
	if verbose {
		fmt.Println("Running fetch ...")
	}

	pullOptsAsImage := imgpkgv1.PullOpts{
		Logger:   logger.NewNullLogger(),
		AsImage:  true,
		IsBundle: false,
	}
	pullOptsAsBundle := imgpkgv1.PullOpts{
		Logger:   logger.NewNullLogger(),
		AsImage:  false,
		IsBundle: true,
	}
	// TODO: Remove some logging from here
	fetchOutputDir := filepath.Join(tempDir, "fetch")
	installerImageRef := fullConfig.InstallerImages.Bundle
	if installerImageRef == "" {
		installerImageRef = inst.getBundleImageRef(version, packageRepository)
	}
	fmt.Println("Using installer image: ", installerImageRef)
	_, err := imgpkgv1.Pull(installerImageRef, fetchOutputDir, pullOptsAsBundle, registry.Opts{})
	if err != nil {
		// TODO: There might be more potential issues here
		return "", errors.Wrapf(err, "Installer image not found")
	}

	// Fetch installer overlay bundles when configured
	if fullConfig != nil && len(fullConfig.InstallerImages.Overlays) > 0 {
		overlaysDir := filepath.Join(tempDir, "overlays")
		for i, ref := range fullConfig.InstallerImages.Overlays {
			overlayDir := filepath.Join(overlaysDir, strconv.Itoa(i))
			if err := os.MkdirAll(overlayDir, 0755); err != nil {
				return "", errors.Wrapf(err, "create overlay directory for %s", ref)
			}
			if verbose {
				fmt.Println("Using installer overlay image: ", ref)
			}
			status, err := imgpkgv1.Pull(ref, overlayDir, pullOptsAsBundle, registry.Opts{})
			if err != nil {
				if !status.IsBundle {
					_, err := imgpkgv1.Pull(ref, overlayDir, pullOptsAsImage, registry.Opts{})
					if err != nil {
						return "", errors.Wrapf(err, "installer overlay image %d (%s) not found", i, ref)
					}
				} else {
					return "", errors.Wrapf(err, "installer overlay image %d (%s) not found", i, ref)
				}
			}
		}
	}

	return fetchOutputDir, nil
}

func (inst *Installer) template(tempDir string, inputDir string, fullConfig *config.InstallationConfig, verbose bool, showPackagesValues bool, skipImageResolution bool) (string, error) {
	if verbose {
		fmt.Println("Running template ...")
	}

	paths := []string{filepath.Join(inputDir, "config/ytt/")}
	if !showPackagesValues && !skipImageResolution {
		paths = append(paths, filepath.Join(inputDir, "kbld/kbld-bundle.yaml"))
	}
	// Add installer overlay bundle roots as ytt paths (any structure in each bundle works)
	if fullConfig != nil && len(fullConfig.InstallerImages.Overlays) > 0 {
		for i := 0; i < len(fullConfig.InstallerImages.Overlays); i++ {
			overlayPath := filepath.Join(tempDir, "overlays", strconv.Itoa(i))
			if _, err := os.Stat(overlayPath); err == nil {
				paths = append(paths, overlayPath)
			}
		}
	}
	filesToProcess, err := files.NewSortedFilesFromPaths(paths, files.SymlinkAllowOpts{})
	if err != nil {
		return "", err
	}

	// Use ytt to generate the yaml for the cluster packages
	opts := cmdtpl.NewOptions()

	// Debug in ytt schema config is used to output the processed values
	if showPackagesValues {
		fullConfig.Debug = utils.BoolPointer(true)
	}

	yamlBytes, err := yaml.Marshal(fullConfig)
	if err != nil {
		return "", err
	}

	kbldFiles := []*files.File{}
	// TODO: Revisit when this needs to be used
	// if !skipImageResolution {
	kbldFiles, err = files.NewSortedFilesFromPaths([]string{filepath.Join(inputDir, "kbld/kbld-images.yaml")}, files.SymlinkAllowOpts{})
	if err != nil {
		return "", err
	}
	// }

	opts.DataValuesFlags = cmdtpl.DataValuesFlags{
		FromFiles: []string{"values", "images"},
		ReadFilesFunc: func(path string) ([]*files.File, error) {
			switch path {
			case "values":
				return []*files.File{
					files.MustNewFileFromSource(files.NewBytesSource("values/values.yaml", yamlBytes)),
				}, nil
			case "images":
				return kbldFiles, nil
			default:
				return nil, fmt.Errorf("unknown file '%s'", path)
			}
		},
	}

	out := opts.RunWithFiles(cmdtpl.Input{Files: filesToProcess}, yttUI.NewTTY(false))

	// When we get errors in ytt processing, e.g. because of schema validation, out.Err is not nil
	if out.Err != nil {
		fmt.Println(out.Err)
	}
	if out.DocSet == nil {
		return "", errors.New("error processing files")
	}

	// Create a new subdirectory in tempDir
	templateOutputDir := filepath.Join(tempDir, "template")
	err = os.Mkdir(templateOutputDir, 0755)
	if err != nil {
		fmt.Printf("Failed to create subdirectory: %v\n", err)
		return "", err
	}

	// We write the processed output to files
	err = utils.WriteYamlDocSetItemsToDir(out.DocSet, templateOutputDir)
	if err != nil {
		return "", err
	}
	return templateOutputDir, nil
}

func (inst *Installer) resolve(tempDir string, inputDir string, verbose bool) (string, error) {
	if verbose {
		fmt.Println("Running resolve images ...")
	}

	kbldOutputDir := filepath.Join(tempDir, "kbld")
	err := os.Mkdir(kbldOutputDir, 0755)
	if err != nil {
		return "", err
	}

	carvelUI := logger.NewCarvelUI()

	resolveOptions := kbldcmd.NewResolveOptions(carvelUI)
	resolveOptions.FileFlags.Files = []string{inputDir}
	// Apply defaults from CLI
	resolveOptions.ImagesAnnotation = false
	resolveOptions.OriginsAnnotation = false
	resolveOptions.UnresolvedInspect = false
	resolveOptions.AllowedToBuild = false
	resolveOptions.BuildConcurrency = 5
	var logger kbldlog.Logger
	if verbose {
		logger = kbldlog.NewLogger(os.Stderr)
	} else {
		logger = kbldlog.NewLogger(NullWriter(0))
	}
	prefixedLogger := logger.NewPrefixedWriter("resolve | ")
	resBss, err := resolveOptions.ResolveResources(&logger, prefixedLogger)
	if err != nil {
		return "", err
	}
	if verbose {
		fmt.Println("All images have been resolved images")
	}

	err = utils.WriteYamlByteArrayItemsToDir(resBss, kbldOutputDir)
	if err != nil {
		return "", err
	}
	return kbldOutputDir, nil
}

func (inst *Installer) deploy(tempDir string, inputDir string, clusterConfig *cluster.ClusterConfig, verbose bool, showDiff bool) error {
	if verbose {
		fmt.Println("Running deploy ...")
	}

	carvelUI := logger.NewCarvelUI()

	depsFactory := NewKappDepsFactoryImpl(clusterConfig)
	deployOptions := app.NewDeployOptions(carvelUI, depsFactory, logger.NewKappLogger(), nil)
	deployOptions.AppFlags.Name = constants.EducatesInstallerAppString
	deployOptions.AppFlags.AppNamespace = constants.EducatesInstallerString
	deployOptions.FileFlags.Files = []string{inputDir, filepath.Join(tempDir, "fetch/config/kapp/")}
	deployOptions.ApplyFlags.ClusterChangeOpts.Wait = true
	deployOptions.ApplyFlags.ClusterChangeOpts.ApplyIgnored = false
	deployOptions.ApplyFlags.ClusterChangeOpts.WaitIgnored = false

	deployOptions.ApplyFlags.ApplyingChangesOpts.Concurrency = 5

	deployOptions.ApplyFlags.WaitingChangesOpts.CheckInterval = time.Duration(1) * time.Second
	deployOptions.ApplyFlags.WaitingChangesOpts.Timeout = time.Duration(15) * time.Minute
	deployOptions.ApplyFlags.WaitingChangesOpts.Concurrency = 5

	deployOptions.DeployFlags.ExistingNonLabeledResourcesCheck = false
	deployOptions.DeployFlags.ExistingNonLabeledResourcesCheckConcurrency = 100
	deployOptions.DeployFlags.AppChangesMaxToKeep = 5

	deployOptions.DiffFlags.AgainstLastApplied = true
	if showDiff {
		deployOptions.DiffFlags.Changes = true
	}

	err := deployOptions.Run()
	if err != nil {
		return err
	}
	return nil
}

func (inst *Installer) delete(clusterConfig *cluster.ClusterConfig) error {
	fmt.Println("Running delete ...")

	carvelUI := logger.NewCarvelUI()

	depsFactory := NewKappDepsFactoryImpl(clusterConfig)
	deleteOptions := app.NewDeleteOptions(carvelUI, depsFactory, logger.NewKappLogger())
	deleteOptions.AppFlags.Name = constants.EducatesInstallerAppString
	deleteOptions.AppFlags.AppNamespace = constants.EducatesInstallerString
	deleteOptions.ApplyFlags.ClusterChangeOpts.Wait = true
	deleteOptions.ApplyFlags.ApplyingChangesOpts.Concurrency = 5
	deleteOptions.ApplyFlags.WaitingChangesOpts.CheckInterval = time.Duration(1) * time.Second
	deleteOptions.ApplyFlags.WaitingChangesOpts.Timeout = time.Duration(15) * time.Minute
	deleteOptions.ApplyFlags.WaitingChangesOpts.Concurrency = 5

	err := deleteOptions.Run()
	if err != nil {
		return err
	}
	return nil
}

func (inst *Installer) getBundleImageRef(version string, packageRepository string) string {
	bundleImageRef := fmt.Sprintf("%s/%s:%s", packageRepository, constants.EducatesInstallerString, version)
	return bundleImageRef
}
