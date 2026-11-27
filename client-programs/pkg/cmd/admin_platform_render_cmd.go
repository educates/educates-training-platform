package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const adminPlatformRenderExample = `
  # Render the install for a checked-in config file (GitOps mode), the
  # operator chart values for helm install -f and the platform CRs for
  # kubectl apply -f, each to a file of its own.
  #   - operator.image is defaulted from this CLI's compiled-in version.
  #   - an EducatesLocalConfig MUST set ingress.domain; render errors if
  #     it is empty.
  educates admin platform render --config env.yaml \
    --values-file values.yaml --resources-file resources.yaml

  # Render the same install to stdout as one YAML stream, for inspection:
  educates admin platform render --config env.yaml

  # Render the install for laptop convenience (uses the default local
  # cluster's <data-home>/config.yaml). When ingress.domain is empty, it
  # falls back to the nip.io name of the router address.
  educates admin platform render --local-config

  # Render the install of the named local cluster test2 from its own config:
  educates admin platform render --local-config --local-cluster test2
`

type PlatformRenderOptions struct {
	Config      string
	LocalConfig bool

	// ValuesFile and ResourcesFile, when set, name the files the operator
	// chart values and the platform CRs are written to, in place of
	// stdout.
	ValuesFile    string
	ResourcesFile string

	// LocalCluster selects the local cluster whose config LocalConfig
	// reads.
	LocalCluster localclusters.Selection
}

func (p *ProjectInfo) NewAdminPlatformRenderCmd() *cobra.Command {
	var o PlatformRenderOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "render",
		Short: "Render the install plan (operator chart values + 4 platform CRs) to stdout or files",
		Long: `Render the install plan that "educates admin platform deploy" would apply,
without touching the cluster. Useful for inspection, GitOps, and diffing.

The install plan has two parts, in deploy order:
  - the operator chart values, for 'helm install -f <file> educates-installer'
  - the four platform CRs, for 'kubectl apply -f <file>'

By default both go to stdout as one YAML stream: the values are its first
document and each CR a document after it. Helm reads every document of a
values file, so the stream as a whole suits neither command.
--values-file and --resources-file write each part to a file of its own
instead, and stdout keeps the part not sent to a file, so either part can
also be piped to its command with '-f -'.

Defaulting is flag-driven:
  --config <file>   Explicit / GitOps mode. operator.image defaults from
                    this CLI binary, for every kind but EducatesConfig,
                    which is rendered as declared. An EducatesLocalConfig
                    MUST set ingress.domain. Render is deterministic for a
                    given (input, CLI version) pair.
  --local-config    Laptop convenience mode. Same defaults, plus the
                    rules of a local cluster's config: the nip.io name
                    of the router address, with a named local cluster's
                    name under it, as the ingress.domain fallback when
                    empty.

--local-config reads the config of the local cluster that --local-cluster
or EDUCATES_LOCAL_CLUSTER selects: <data-home>/config.yaml for the default
local cluster, <data-home>/clusters/<name>/config.yaml for a named one.`,
		Example: adminPlatformRenderExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.LocalCluster = localClusterSelection(cmd)
			return p.runRender(cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false,
		"use the selected local cluster's config in the data home; applies the nip.io fallback for ingress.domain")
	c.Flags().StringVar(&o.ValuesFile, "values-file", "",
		"write the operator chart values to this file instead of stdout")
	c.Flags().StringVar(&o.ResourcesFile, "resources-file", "",
		"write the platform CRs to this file instead of stdout")
	addLocalClusterFlag(c)
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsMutuallyExclusive("config", localclusters.SelectionFlagName)
	c.MarkFlagsOneRequired("config", "local-config")

	return c
}

func (p *ProjectInfo) runRender(w io.Writer, o *PlatformRenderOptions) error {
	cfg, path, err := loadPlatformConfig(o.Config, o.LocalConfig, o.LocalCluster)
	if err != nil {
		return err
	}
	valuesFile, resourcesFile := stdoutUnlessNamed(o.ValuesFile), stdoutUnlessNamed(o.ResourcesFile)
	if err := checkRenderOutputFiles(valuesFile, resourcesFile, path); err != nil {
		return err
	}

	p.applyCLIDefaults(cfg)

	var notes yamlComments
	opts := translator.Options{}
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		// The local cluster rules of --local-config have already given
		// an empty ingress.domain a nip.io name.
		if !o.LocalConfig && c.Ingress.Domain == "" {
			return fmt.Errorf(`ingress.domain is required when using --config.
Set it explicitly in %s, or use --local-config to fall back to the nip.io
name of the router address.`, path)
		}
		// The certificates come from the local certificate resolution
		// step. Failing here (before any rendering) gives the user a
		// remediation path before they pipe output to GitOps. Render
		// only reports the plan: it never commits it, so the local
		// secrets cache is left untouched.
		plan, err := localcerts.Resolve(c)
		if err != nil {
			return err
		}
		opts = plan.TranslatorOptions()
		plan.Report(&notes)
		// The resources reference cached secrets for the cluster's
		// certificate, for the CA trust of a certificate the router
		// presents, and for the image pull secrets.
		routerTrust := plan.Certificates.None != nil && plan.Certificates.None.CACertificateSecretRef != nil
		if plan.Certificates.Provider != translator.CertificatesProviderNone || routerTrust ||
			len(c.ImageRegistry.PullSecrets) > 0 {
			notes.Note(fmt.Sprintf("Before 'helm install', run 'educates local secrets sync' to copy the cached secrets\n"+
				"these resources reference into the operator namespace, %s.", deployer.OperatorNamespace))
		}
	case *v1alpha1.EducatesConfig:
		// Escape-hatch: pure passthrough. No CLI-side defaults. User owns
		// the full surface; missing required fields are caught by the
		// CRD-derived schema at load time or by the apiserver at apply.
	}

	if names := pullSecretsToCheck(cfg); len(names) > 0 {
		noun := "secret"
		if len(names) > 1 {
			noun = "secrets"
		}
		quoted := make([]string, len(names))
		for i, name := range names {
			quoted[i] = strconv.Quote(name)
		}
		notes.Note(fmt.Sprintf("Before 'helm install', create the image pull %s %s, of type\n"+
			"kubernetes.io/dockerconfigjson, in the operator namespace, %s.", noun, strings.Join(quoted, ", "),
			deployer.OperatorNamespace))
	}

	out, err := translator.Translate(cfg, opts)
	if err != nil {
		return err
	}

	return writeRender(w, out, notes.String(), valuesFile, resourcesFile)
}

// applyCLIDefaults gives cfg the defaults that come from this CLI binary,
// its version and image repository, such as the operator image, for every
// kind but EducatesConfig, which render and deploy pass through as
// declared. Render and deploy both call it, so the two install the same
// operator image for the same config.
func (p *ProjectInfo) applyCLIDefaults(cfg v1alpha1.Config) {
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
	case *v1alpha1.EducatesGKEConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
	case *v1alpha1.EducatesEKSConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
	case *v1alpha1.EducatesInlineConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
	}
}

// yamlComments is a localcerts.Reporter that collects each reported line
// as a YAML comment, for render to put above its output.
type yamlComments struct{ strings.Builder }

func (c *yamlComments) Note(msg string) {
	for _, line := range strings.Split(msg, "\n") {
		c.WriteString(strings.TrimRight("# "+line, " ") + "\n")
	}
}

// loadPlatformConfig loads the config admin platform render and deploy
// install from, and returns it with the path it was read from: the file
// configFile when localConfig is not set, otherwise the config of the
// local cluster sel selects, in the data home, which is
// $EDUCATES_CLI_DATA_HOME or $XDG_DATA_HOME/educates.
//
// A missing default local cluster's config.yaml is migrated from a v3
// install when there is one, and is otherwise an error that says how to
// write it. A named local cluster's config follows the rules of
// localclusters.LoadNamedConfig.
func loadPlatformConfig(configFile string, localConfig bool, sel localclusters.Selection) (v1alpha1.Config, string, error) {
	if !localConfig {
		if configFile == "" {
			return nil, "", fmt.Errorf("internal: neither --config nor --local-config set (should have been caught by cobra)")
		}
		cfg, err := config.Load(configFile)
		return cfg, configFile, err
	}

	id, err := localclusters.Resolve(sel, utils.GetEducatesHomeDir())
	if err != nil {
		return nil, "", err
	}
	if id.IsDefault() {
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return nil, id.ConfigPath, err
		}
	}
	loaded, err := localclusters.LoadConfig(id)
	return loaded.Config, id.ConfigPath, err
}

// stdoutUnlessNamed returns file, or "" for stdout when file is "-",
// as for kubectl and helm.
func stdoutUnlessNamed(file string) string {
	if file == "-" {
		return ""
	}
	return file
}

// checkRenderOutputFiles refuses output files that would overwrite each
// other, or the config file render reads, configPath.
func checkRenderOutputFiles(valuesFile, resourcesFile, configPath string) error {
	if valuesFile != "" && resourcesFile != "" && sameFile(valuesFile, resourcesFile) {
		return fmt.Errorf("--values-file and --resources-file name the same file, %s", valuesFile)
	}
	for _, f := range []struct{ flag, file string }{
		{"--values-file", valuesFile},
		{"--resources-file", resourcesFile},
	} {
		if f.file != "" && sameFile(f.file, configPath) {
			return fmt.Errorf("%s names the config file being rendered, %s", f.flag, configPath)
		}
	}
	return nil
}

// sameFile reports whether paths a and b name the same file: the same
// path once made absolute, or, when both exist, the same file on disk.
func sameFile(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil && absA == absB {
		return true
	}
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

// writeRender emits the rendered install plan in its two parts, in
// deploy order: the operator chart values, then the platform CRs. Each
// part goes to the file named for it, valuesFile or resourcesFile, or
// else to w. Whatever w receives starts with hostHeader, and when w
// receives both parts, each starts with a comment naming it and a "---"
// separates them, so the values are a YAML document of their own.
//
// Both parts are encoded, and each file's content staged in a temporary
// file beside it, before either file is replaced, so a file that cannot
// be written leaves the other as it was, and each is replaced whole.
func writeRender(w io.Writer, out *translator.Output, hostHeader, valuesFile, resourcesFile string) error {
	values, err := translator.RenderOperatorValues(out)
	if err != nil {
		return fmt.Errorf("render operator values: %w", err)
	}
	crs, err := translator.RenderCRs(out)
	if err != nil {
		return fmt.Errorf("render CRs: %w", err)
	}

	var files []stagedFile
	defer func() {
		for _, f := range files {
			os.Remove(f.staged)
		}
	}()
	for _, part := range []struct {
		file, what string
		content    []byte
	}{
		{valuesFile, "operator chart values", values},
		{resourcesFile, "platform CRs", crs},
	} {
		if part.file == "" {
			continue
		}
		f, err := stageFile(part.file, part.content)
		if err != nil {
			return fmt.Errorf("write %s: %w", part.what, err)
		}
		files = append(files, f)
	}
	for _, f := range files {
		if err := os.Rename(f.staged, f.path); err != nil {
			return fmt.Errorf("write %s: %w", f.path, err)
		}
	}

	var stream bytes.Buffer
	stream.WriteString(hostHeader)
	switch {
	case valuesFile == "" && resourcesFile == "":
		stream.WriteString("# === operator chart values (educates-installer chart) ===\n")
		stream.Write(values)
		stream.WriteString("---\n# === platform CRs ===\n")
		stream.Write(crs)
	case valuesFile == "":
		stream.Write(values)
	case resourcesFile == "":
		stream.Write(crs)
	}
	_, err = w.Write(stream.Bytes())
	return err
}

// stagedFile is a temporary file holding the content for path, renamed
// over path once every file render writes is staged.
type stagedFile struct{ path, staged string }

// stageFile writes content to a new temporary file in the directory of
// path, with the permissions path has, or else readable by all. When
// path is a symbolic link, the file it links to is the one staged for,
// so the link stays.
func stageFile(path string, content []byte) (stagedFile, error) {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		// Name the file asked for, not the temporary one.
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		return stagedFile{}, &fs.PathError{Op: "create", Path: path, Err: err}
	}
	f := stagedFile{path: path, staged: tmp.Name()}
	_, err = tmp.Write(content)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(f.staged, mode)
	}
	if err != nil {
		os.Remove(f.staged)
		return stagedFile{}, err
	}
	return f, nil
}
