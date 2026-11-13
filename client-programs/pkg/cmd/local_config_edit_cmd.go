package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// localConfigEditHeader is prepended to the editor buffer. Every line starts
// with '##' so stripDoubleHashLines removes the whole block before validating
// or saving — single-'#' YAML comments in the user's content are preserved.
// The trailing "##\n" is the separator before the configuration body.
const localConfigEditHeader = `## Edit the Educates local configuration below.
## Lines beginning with '##' are informational and are removed before saving.
## Saving an empty file — or quitting the editor without saving — aborts the
## edit and leaves the current configuration unchanged. If validation fails,
## this file reopens with the error shown inline so you can correct it.
##
`

var localConfigEditExample = `
  # Edit the local configuration in $EDITOR, validating on save:
  educates local config edit

  # Edit the config of the named local cluster test2:
  educates local config edit --local-cluster test2
`

func (p *ProjectInfo) NewLocalConfigEditCmd() *cobra.Command {
	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "edit",
		Short: "Open <data-home>/config.yaml in $EDITOR; validate against the schema on save",
		Long: `Opens <data-home>/config.yaml in $EDITOR (or vi if unset) in a temp
copy. On editor exit the temp copy is validated against the
EducatesLocalConfig schema. A passing validation atomically replaces the
canonical file. A failing validation reopens the editor with the error
shown inline (kubectl-edit style), preserving your in-progress changes,
until the configuration is valid or you abort. On save it is also
validated against the rules of a local cluster's config, such as a
cluster.listenAddress that is a specific IP address of the host.

--local-cluster <name>, or EDUCATES_LOCAL_CLUSTER, edits the config of a
named local cluster instead, <data-home>/clusters/<name>/config.yaml,
which 'educates local config init --local-cluster <name>' writes first.`,
		Example: localConfigEditExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return runLocalConfigEdit(id)
		},
	}
	addLocalClusterFlag(c)
	return c
}

// runLocalConfigEdit edits the config of the local cluster id in the
// user's editor. A named local cluster's config must exist already: it is
// written with 'local config init', which takes its listen address.
func runLocalConfigEdit(id localclusters.Identity) error {
	if err := checkNamedConfigExists(id); err != nil {
		return err
	}
	dataHome := utils.GetEducatesHomeDir()
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		return fmt.Errorf("create data home %q: %w", dataHome, err)
	}
	canonical := id.ConfigPath
	tmp := fmt.Sprintf("%s.%d", canonical, os.Getpid())

	// Keep the temp file on exit only when we abort after the user has made
	// edits, so their work isn't silently discarded.
	preserve := false
	defer func() {
		if preserve {
			return
		}
		if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: could not remove temp file %q: %v\n", tmp, err)
		}
	}()

	// Seed with current contents, or the minimal init stub when no canonical
	// file exists yet — so 'edit' on a pristine data home is a working flow.
	seed, err := config.ReadConfigFile(canonical)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if seed, err = localclusters.ConfigYAML(id, localclusters.ConfigFlags{}); err != nil {
			return err
		}
	}

	editorPath, err := resolveEditor()
	if err != nil {
		return err
	}

	header := localConfigEditHeader
	body := seed

	firstEdit := true
	var lastBody []byte // last '##'-stripped user content, for recovery on abort

	for {
		buffer := []byte(header + string(body))
		if err := os.WriteFile(tmp, buffer, 0o644); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}

		cmd := exec.Command(editorPath, tmp)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("editor: %w", err)
		}

		edited, err := os.ReadFile(tmp)
		if err != nil {
			return fmt.Errorf("read temp file: %w", err)
		}

		// Byte-identical to what we wrote means the user quit without saving.
		if string(edited) == string(buffer) {
			if firstEdit {
				fmt.Println("Edit cancelled, no changes made.")
				return nil
			}
			return abortWithRecovery(tmp, lastBody, &preserve)
		}

		stripped := stripDoubleHashLines(edited)

		// An empty buffer aborts the edit (kubectl-edit convention).
		if strings.TrimSpace(string(stripped)) == "" {
			if firstEdit {
				fmt.Println("Edit cancelled, no changes made.")
				return nil
			}
			return abortWithRecovery(tmp, lastBody, &preserve)
		}

		lastBody = stripped

		// Write the stripped body to the temp path and validate it through
		// the shared loader (schema + strict typed decode + defaulting). A
		// successful pass renames this same file over the canonical one, so
		// what gets persisted is exactly the validated, comment-free body.
		if err := os.WriteFile(tmp, stripped, 0o644); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}

		if verr := validateEditedConfig(id, tmp); verr != nil {
			// Reopen with the (possibly multi-line) error inline. Each error
			// line is '## '-prefixed so it can't be parsed as YAML and is
			// stripped on the next save.
			header = localConfigEditHeader + commentEachLine("## ", verr.Error()) + "\n##\n"
			body = stripped
			firstEdit = false
			continue
		}

		if err := os.Rename(tmp, canonical); err != nil {
			return fmt.Errorf("replace %s: %w", canonical, err)
		}
		fmt.Println("Configuration updated successfully.")
		return nil
	}
}

// validateEditedConfig checks that the edited config at path loads as the
// config of the local cluster id, following the rules of a local
// cluster's config, and for the default local cluster, that it is an
// EducatesLocalConfig. A rule it breaks is reported against the cluster's
// config, which the edit replaces.
func validateEditedConfig(id localclusters.Identity, path string) error {
	if id.IsDefault() {
		if _, err := config.LoadLocal(path); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return localclusters.CheckConfig(id, data, id.ConfigPath)
}

// resolveEditor picks $VISUAL, then $EDITOR, then vi, and resolves it on PATH.
func resolveEditor() (string, error) {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		editor = "vi"
	}
	path, err := exec.LookPath(editor)
	if err != nil {
		return "", fmt.Errorf("locate editor %q: %w", editor, err)
	}
	return path, nil
}

// abortWithRecovery preserves the user's last content in the temp file and
// returns an error, so edits made before the abort aren't lost.
func abortWithRecovery(tmp string, lastBody []byte, preserve *bool) error {
	if err := os.WriteFile(tmp, lastBody, 0o644); err == nil {
		*preserve = true
		fmt.Printf("A copy of your changes has been kept at %q\n", tmp)
	}
	return fmt.Errorf("edit cancelled, no valid changes were saved")
}

// stripDoubleHashLines removes lines whose first non-space characters are
// '##'. Single-'#' YAML comments are preserved.
func stripDoubleHashLines(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "##") {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

// commentEachLine prefixes every line of s with prefix, so a multi-line
// validation error can be embedded in the editor buffer without any line
// leaking into the YAML.
func commentEachLine(prefix, s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
