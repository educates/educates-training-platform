package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

var localConfigGetExample = `
  # Print the whole config file:
  educates local config get

  # Read a single field by dotted path:
  educates local config get ingress.domain
  educates local config get operator.image
`

func (p *ProjectInfo) NewLocalConfigGetCmd() *cobra.Command {
	c := &cobra.Command{
		Args:  maximumArgs(1, "expected at most one PATH argument", "[PATH]"),
		Use:   "get [PATH]",
		Short: "Read a field from <data-home>/config.yaml by dotted path",
		Long: `Print a field from the EducatesLocalConfig. Path is dotted, e.g.
'ingress.domain' or 'operator.image.tag'. With no argument, prints the
whole file.

For scalar fields, the value is printed bare (no quoting). For maps and
lists, YAML-serialised output is emitted.`,
		Example: localConfigGetExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			return runLocalConfigGet(cmd.OutOrStdout(), path)
		},
	}
	return c
}

func runLocalConfigGet(w io.Writer, path string) error {
	cfgPath := defaultLocalCluster().ConfigPath
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", cfgPath, err)
	}

	if path == "" {
		_, err := w.Write(data)
		return err
	}

	// Parse into a generic map so we can walk by string keys. Don't
	// validate against the schema here — get is read-only; the file may
	// already be in an invalid state and we still want to surface its
	// contents.
	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse %s: %w", cfgPath, err)
	}

	val, ok := getByPath(root, path)
	if !ok {
		return fmt.Errorf("path %q not found in %s", path, cfgPath)
	}

	switch v := val.(type) {
	case string:
		fmt.Fprintln(w, v)
	case int, int64, float64, bool, nil:
		fmt.Fprintln(w, v)
	default:
		out, err := yaml.Marshal(v)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", path, err)
		}
		_, err = w.Write(out)
		return err
	}
	return nil
}

// getByPath walks root by dotted path. yaml.v2 produces
// map[interface{}]interface{} for nested objects; normalise on the fly
// rather than pre-walking the whole tree.
func getByPath(root map[string]interface{}, path string) (interface{}, bool) {
	segs := strings.Split(path, ".")
	var cur interface{} = root
	for _, s := range segs {
		switch m := cur.(type) {
		case map[string]interface{}:
			v, ok := m[s]
			if !ok {
				return nil, false
			}
			cur = v
		case map[interface{}]interface{}:
			v, ok := m[s]
			if !ok {
				return nil, false
			}
			cur = v
		default:
			return nil, false
		}
	}
	return cur, true
}
