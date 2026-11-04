// gen-cli-schemas regenerates EducatesConfig.schema.json from the four
// platform CRDs. The escape-hatch kind mirrors the CRDs verbatim, so its
// schema is derived from controller-gen output rather than hand-authored.
//
// Inputs:
//   - installer/charts/educates-installer/crds/*.yaml
//
// Output:
//   - client-programs/pkg/config/v1alpha1/schemas/EducatesConfig.schema.json
//
// Run from the repo root:
//
//	go run ./client-programs/hack/gen-cli-schemas
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"
)

const (
	crdDir    = "installer/charts/educates-installer/crds"
	schemaOut = "client-programs/pkg/config/v1alpha1/schemas/EducatesConfig.schema.json"
)

// crdSources maps the CRD filename to (envelope-key, $defs-name). Envelope
// key is the camelCase top-level field in EducatesConfig; $defs name is the
// JSON schema definition handle.
var crdSources = []struct {
	file    string
	envKey  string
	defName string
}{
	{"config.educates.dev_educatesclusterconfigs.yaml", "educatesClusterConfig", "EducatesClusterConfigSpec"},
	{"platform.educates.dev_secretsmanagers.yaml", "secretsManager", "SecretsManagerSpec"},
	{"platform.educates.dev_lookupservices.yaml", "lookupService", "LookupServiceSpec"},
	{"platform.educates.dev_sessionmanagers.yaml", "sessionManager", "SessionManagerSpec"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-cli-schemas:", err)
		os.Exit(1)
	}
}

func run() error {
	defs := map[string]interface{}{}
	envelopeProps := envelopeProperties()
	envelopeRequired := []string{"apiVersion", "kind"}

	for _, src := range crdSources {
		spec, err := extractSpec(filepath.Join(crdDir, src.file))
		if err != nil {
			return fmt.Errorf("%s: %w", src.file, err)
		}
		defs[src.defName] = sanitise(spec)
		envelopeProps[src.envKey] = map[string]interface{}{
			"$ref": "#/$defs/" + src.defName,
		}
	}

	root := map[string]interface{}{
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"$id":                  "https://schemas.educates.dev/cli/v1alpha1/EducatesConfig.json",
		"title":                "EducatesConfig",
		"description":          "Escape-hatch CLI config kind. Mirrors the platform CRDs verbatim; CLI hand-wraps apiVersion/kind/metadata at translate time.",
		"type":                 "object",
		"additionalProperties": false,
		"required":             envelopeRequired,
		"properties":           envelopeProps,
		"$defs":                defs,
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.WriteFile(schemaOut, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes)\n", schemaOut, len(out))
	return nil
}

// envelopeProperties returns the hand-authored CLI-side envelope fields
// (apiVersion, kind, target, operator). CR-spec fields are added by the
// caller after extracting them from the CRDs.
func envelopeProperties() map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": map[string]interface{}{"const": "cli.educates.dev/v1alpha1"},
		"kind":       map[string]interface{}{"const": "EducatesConfig"},

		// target is optional; when absent the CLI skips side effects and
		// just applies the declared CRs. provider drives which side
		// effects run (kind cluster bootstrap, macOS resolver); kind is
		// the only provider.
		"target": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"provider": map[string]interface{}{
					"type": "string",
					"enum": []string{"kind"},
				},
				"cluster":  map[string]interface{}{"type": "object"},
				"resolver": map[string]interface{}{"type": "object"},
			},
		},

		"operator": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"image": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"repository": map[string]interface{}{"type": "string"},
						"tag":        map[string]interface{}{"type": "string"},
					},
				},
				"imagePullSecrets": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string", "minLength": 1},
				},
				"logLevel": map[string]interface{}{
					"type":    "string",
					"enum":    []string{"debug", "info", "warn", "error"},
					"default": "info",
				},
			},
		},
	}
}

// extractSpec navigates a CRD YAML and returns the openAPIV3Schema subtree
// for the v1alpha1 version's spec property. Returned value is a JSON-ready
// nested map.
func extractSpec(path string) (interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	normalised := normalise(raw)

	root, ok := normalised.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("root is not a map")
	}
	spec, ok := root["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf(".spec not a map")
	}
	versions, ok := spec["versions"].([]interface{})
	if !ok {
		return nil, fmt.Errorf(".spec.versions not a list")
	}
	for _, v := range versions {
		ver, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if ver["name"] != "v1alpha1" {
			continue
		}
		schema, ok := ver["schema"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("v1alpha1.schema not a map")
		}
		oas, ok := schema["openAPIV3Schema"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("openAPIV3Schema not a map")
		}
		props, ok := oas["properties"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("openAPIV3Schema.properties not a map")
		}
		specProp, ok := props["spec"]
		if !ok {
			return nil, fmt.Errorf("openAPIV3Schema.properties.spec missing")
		}
		return specProp, nil
	}
	return nil, fmt.Errorf("no v1alpha1 version found")
}

// sanitise walks a CRD-derived schema subtree and removes keys that
// gojsonschema (draft-07) cannot handle: x-kubernetes-* extensions and the
// OpenAPI-only `nullable` keyword. Recursively applied.
func sanitise(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			if strings.HasPrefix(k, "x-kubernetes-") || k == "nullable" {
				continue
			}
			out[k] = sanitise(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = sanitise(val)
		}
		return out
	default:
		return v
	}
}

// normalise converts yaml.v2's map[interface{}]interface{} to
// map[string]interface{} so the structure can be JSON-marshalled. Keys are
// sorted on traversal so the output schema is deterministic regardless of
// YAML key order.
func normalise(v interface{}) interface{} {
	switch x := v.(type) {
	case map[interface{}]interface{}:
		keys := make([]string, 0, len(x))
		strMap := make(map[string]interface{}, len(x))
		for k, val := range x {
			ks := fmt.Sprint(k)
			keys = append(keys, ks)
			strMap[ks] = normalise(val)
		}
		sort.Strings(keys)
		out := make(map[string]interface{}, len(x))
		for _, k := range keys {
			out[k] = strMap[k]
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = normalise(val)
		}
		return out
	default:
		return v
	}
}
