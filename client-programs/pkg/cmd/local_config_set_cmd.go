package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localConfigSetExample = `
  # Set a string field:
  educates local config set ingress.domain workshop.test

  # Set an enum field:
  educates local config set operator.logLevel debug

  # Set a boolean field:
  educates local config set lookupService false

  # Switch the image pre-puller off:
  educates local config set imagePrePuller.enabled false
`

func (p *ProjectInfo) NewLocalConfigSetCmd() *cobra.Command {
	c := &cobra.Command{
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return utils.CmdErrorFullUsage(cmd, "PATH and VALUE are both required", "PATH VALUE")
			}
			return nil
		},
		Use:   "set PATH VALUE",
		Short: "Set a field in <data-home>/config.yaml by dotted path",
		Long: `Modify a field in the EducatesLocalConfig file. Path is dotted, e.g.
'ingress.domain' or 'operator.logLevel'. Intermediate maps are auto-
created as needed.

VALUE is coerced based on its raw form:
  - 'true' / 'false'          → boolean
  - integer-looking strings   → integer
  - everything else           → string

The full config is re-validated against the EducatesLocalConfig schema
after the change. The write only happens when validation passes, so
type / enum errors are caught up front with the offending field path.`,
		Example: localConfigSetExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocalConfigSet(cmd.OutOrStdout(), args[0], args[1])
		},
	}
	return c
}

func runLocalConfigSet(w io.Writer, path, rawValue string) error {
	cfgPath := defaultLocalCluster().ConfigPath
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read %s (run 'educates local config init' first?): %w", cfgPath, err)
	}

	// Edit the YAML as a yaml.v3 node tree so the file's comments (the
	// yaml-language-server modeline in particular) and key order survive
	// the round-trip. Marshalling a decoded map instead would sort keys
	// alphabetically and drop every comment.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", cfgPath, err)
	}
	root := rootMappingNode(&doc)

	if err := setNodeByPath(root, strings.Split(path, "."), coerce(rawValue)); err != nil {
		// A scalar where the path needs a map is often a value the
		// schema rejects, so a file that does not validate as it stands
		// reports its schema errors instead.
		if errors.As(err, new(*notAMapError)) {
			lines, verr := fileSchemaErrorLines(data)
			if verr != nil {
				return fmt.Errorf("%s: schema validation: %w", cfgPath, verr)
			}
			if len(lines) > 0 {
				return fmt.Errorf("path %q: %s does not validate as it stands ('educates local config edit' opens the file):\n%s",
					path, cfgPath, indentLines(lines))
			}
		}
		return err
	}

	// Validate the edited document against the schema before writing.
	var asMap map[string]interface{}
	if err := root.Decode(&asMap); err != nil {
		return fmt.Errorf("decode edited %s: %w", cfgPath, err)
	}
	if asMap == nil {
		asMap = map[string]interface{}{}
	}
	if err := validateAgainstLocalSchema(asMap, cfgPath, path); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(cfgPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cfgPath, err)
	}
	fmt.Fprintf(w, "%s.%s = %v\n", filepath.Base(cfgPath), path, coerce(rawValue))
	return nil
}

// rootMappingNode returns the top-level mapping node of a parsed
// document, creating an empty document + mapping when the file was empty
// so a first 'set' on a blank file still works.
func rootMappingNode(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	doc.Kind = yaml.DocumentNode
	doc.Content = []*yaml.Node{root}
	return root
}

// setNodeByPath walks the mapping node by dotted path, creating
// intermediate mapping nodes as needed, and sets the leaf to value.
// Existing key and value nodes are updated in place so any comments
// attached to them are preserved.
func setNodeByPath(root *yaml.Node, segs []string, value interface{}) error {
	if len(segs) == 0 || segs[0] == "" {
		return fmt.Errorf("empty path")
	}
	cur := root
	for i, s := range segs[:len(segs)-1] {
		v := mappingValue(cur, s)
		if v == nil {
			child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			cur.Content = append(cur.Content, scalarNode("!!str", s), child)
			cur = child
			continue
		}
		if v.Kind != yaml.MappingNode {
			return &notAMapError{path: strings.Join(segs, "."), segment: strings.Join(segs[:i+1], ".")}
		}
		cur = v
	}
	leaf := segs[len(segs)-1]
	tag, sval := scalarValue(value)
	if existing := mappingValue(cur, leaf); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = tag
		existing.Value = sval
		existing.Style = 0
		existing.Content = nil
	} else {
		cur.Content = append(cur.Content, scalarNode("!!str", leaf), scalarNode(tag, sval))
	}
	return nil
}

// notAMapError reports a path segment whose value is not a map although
// the path goes on below it.
type notAMapError struct {
	path, segment string
}

func (e *notAMapError) Error() string {
	return fmt.Sprintf("path %q: segment %q is not a map", e.path, e.segment)
}

// mappingValue returns the value node paired with key in a mapping node,
// or nil when the key is absent.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalarNode(tag, value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
}

// scalarValue maps a coerced value to its YAML tag and string form so it
// renders unquoted for bools and ints.
func scalarValue(value interface{}) (tag, sval string) {
	switch v := value.(type) {
	case bool:
		return "!!bool", strconv.FormatBool(v)
	case int:
		return "!!int", strconv.Itoa(v)
	default:
		return "!!str", fmt.Sprint(v)
	}
}

// coerce maps the raw CLI string to a typed value. Conservative: only
// bools and pure integers are recognised; anything else stays a string
// so the schema's "type: string" enforcement does the rest.
func coerce(raw string) interface{} {
	switch raw {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	return raw
}

// validateAgainstLocalSchema runs the in-memory map through the embedded
// EducatesLocalConfig schema. On failure, errors mention the offending
// JSON path so the user can see which field rejected the value.
//
// We deliberately validate the WHOLE config, not just the field we set:
// catches cases where the new value collides with another field's
// constraint (e.g. setting `mode` would always fail since the schema
// rejects mode at the top level).
func validateAgainstLocalSchema(root map[string]interface{}, source, attemptedPath string) error {
	lines, err := localSchemaErrorLines(root)
	if err != nil {
		return fmt.Errorf("%s: schema validation: %w", source, err)
	}
	if len(lines) == 0 {
		return nil
	}
	return fmt.Errorf(`set %q rejected by schema validation. %s would become invalid:
%s`, attemptedPath, source, indentLines(lines))
}

// fileSchemaErrorLines validates the file's contents, as they stand,
// against the embedded EducatesLocalConfig schema and returns its errors
// one line each, or none when it validates.
func fileSchemaErrorLines(data []byte) ([]string, error) {
	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]interface{}{}
	}
	return localSchemaErrorLines(root)
}

// localSchemaErrorLines validates root against the embedded
// EducatesLocalConfig schema and returns its errors one line each, or none
// when it validates.
func localSchemaErrorLines(root map[string]interface{}) ([]string, error) {
	// gojsonschema needs JSON-compatible types; yaml.v2 returns
	// map[interface{}]interface{} for objects, which json.Marshal
	// rejects. Normalise.
	normalised := normaliseForJSON(root)

	loader := gojsonschema.NewBytesLoader(schemas.EducatesLocalConfig)
	docLoader := gojsonschema.NewGoLoader(normalised)
	result, err := gojsonschema.Validate(loader, docLoader)
	if err != nil {
		return nil, err
	}
	return config.SchemaErrorLines(result.Errors()), nil
}

// indentLines writes lines as an indented list, one item a line.
func indentLines(lines []string) string {
	items := make([]string, len(lines))
	for i, line := range lines {
		items[i] = "  - " + line
	}
	return strings.Join(items, "\n")
}

// normaliseForJSON converts yaml.v2's map[interface{}]interface{} to
// map[string]interface{} recursively. Duplicates the helper in the
// loader; keeping it local avoids exposing it from pkg/config.
func normaliseForJSON(v interface{}) interface{} {
	switch x := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, val := range x {
			m[fmt.Sprint(k)] = normaliseForJSON(val)
		}
		return m
	case map[string]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, val := range x {
			m[k] = normaliseForJSON(val)
		}
		return m
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = normaliseForJSON(val)
		}
		return out
	default:
		return v
	}
}
