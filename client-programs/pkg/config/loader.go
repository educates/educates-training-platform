package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v2"
)

// Load reads a CLI config file, validates its apiVersion/kind, runs JSON
// schema validation, then strict-unmarshals into the typed struct. The
// returned value implements v1alpha1.Config; callers type-switch to the
// concrete kind.
func Load(path string) (v1alpha1.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return LoadBytes(data, path)
}

// LoadBytes is the path-free variant — useful for stdin and tests. The
// source string is woven into error messages so users can locate the file.
//
// Single-pass: one yaml.Unmarshal → normalise → json.Marshal. The JSON
// bytes drive both schema validation and the typed strict decode (via
// json.Decoder.DisallowUnknownFields, which is the json equivalent of
// yaml.UnmarshalStrict's behaviour around unknown fields).
func LoadBytes(data []byte, source string) (v1alpha1.Config, error) {
	jsonData, raw, err := yamlToJSON(data, source)
	if err != nil {
		return nil, err
	}
	apiVersion, _ := raw["apiVersion"].(string)
	kind, _ := raw["kind"].(string)
	if apiVersion == "" || kind == "" {
		return nil, fmt.Errorf("%s: missing required field 'apiVersion' or 'kind'", source)
	}
	if apiVersion != v1alpha1.APIVersion {
		return nil, fmt.Errorf("%s: unsupported apiVersion %q (want %q)", source, apiVersion, v1alpha1.APIVersion)
	}

	switch kind {
	case v1alpha1.KindEducatesLocalConfig:
		return decodeAndDefault(jsonData, schemas.EducatesLocalConfig, source, &v1alpha1.EducatesLocalConfig{}, true)
	case v1alpha1.KindEducatesConfig:
		// Escape-hatch: CR-spec fields are untyped maps; don't reject
		// unknown fields inside them (the typed struct only declares
		// the envelope; the CR specs are map[string]interface{} that
		// json's strict mode would happily accept any keys for anyway).
		return decodeAndDefault(jsonData, schemas.EducatesConfig, source, &v1alpha1.EducatesConfig{}, false)
	case v1alpha1.KindEducatesInlineConfig:
		return decodeAndDefault(jsonData, schemas.EducatesInlineConfig, source, &v1alpha1.EducatesInlineConfig{}, true)
	case v1alpha1.KindEducatesGKEConfig:
		return decodeAndDefault(jsonData, schemas.EducatesGKEConfig, source, &v1alpha1.EducatesGKEConfig{}, true)
	case v1alpha1.KindEducatesEKSConfig:
		return decodeAndDefault(jsonData, schemas.EducatesEKSConfig, source, &v1alpha1.EducatesEKSConfig{}, true)
	default:
		return nil, fmt.Errorf("%s: unknown kind %q for apiVersion %q", source, kind, apiVersion)
	}
}

// LoadLocal is the typed convenience wrapper for callers that only accept
// EducatesLocalConfig (e.g. `educates local config *` commands).
func LoadLocal(path string) (*v1alpha1.EducatesLocalConfig, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		return nil, fmt.Errorf("%s: expected kind %q, got %q",
			path, v1alpha1.KindEducatesLocalConfig, cfg.GetKind())
	}
	return local, nil
}

// yamlToJSON parses YAML once, normalises yaml.v2's
// map[interface{}]interface{} to map[string]interface{}, then marshals
// to JSON. Returns both the normalised top-level map (for cheap
// apiVersion/kind extraction) and the JSON bytes (for schema
// validation + typed decode).
func yamlToJSON(data []byte, source string) ([]byte, map[string]interface{}, error) {
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("%s: parse YAML: %w", source, err)
	}
	normalised := normaliseForJSON(raw)
	rootMap, _ := normalised.(map[string]interface{})
	if rootMap == nil {
		// Empty document or scalar root — keep going; downstream
		// schema/decode steps will produce the actionable error.
		rootMap = map[string]interface{}{}
	}
	jsonBytes, err := json.Marshal(normalised)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: marshal to JSON: %w", source, err)
	}
	return jsonBytes, rootMap, nil
}

// decodeAndDefault validates jsonData against schemaBytes, strict-decodes
// (or loose for the escape kind which holds untyped maps), then applies
// any per-kind WithDefaults.
func decodeAndDefault(
	jsonData []byte,
	schemaBytes []byte,
	source string,
	target v1alpha1.Config,
	strict bool,
) (v1alpha1.Config, error) {
	if err := validateAgainstSchema(jsonData, schemaBytes, source); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(jsonData))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(target); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	switch t := target.(type) {
	case *v1alpha1.EducatesLocalConfig:
		t.WithDefaults()
	case *v1alpha1.EducatesInlineConfig:
		t.WithDefaults()
	case *v1alpha1.EducatesGKEConfig:
		t.WithDefaults()
	case *v1alpha1.EducatesEKSConfig:
		t.WithDefaults()
	case *v1alpha1.EducatesConfig:
		// Escape kind: verbatim passthrough, no defaulting.
	}
	return target, nil
}

// validateAgainstSchema runs gojsonschema against the already-marshalled
// JSON bytes (caller has done the YAML→JSON conversion once).
func validateAgainstSchema(jsonData, schemaBytes []byte, source string) error {
	loader := gojsonschema.NewBytesLoader(schemaBytes)
	docLoader := gojsonschema.NewBytesLoader(jsonData)
	result, err := gojsonschema.Validate(loader, docLoader)
	if err != nil {
		return fmt.Errorf("%s: schema validation error: %w", source, err)
	}
	if result.Valid() {
		return nil
	}
	var msgs []string
	for _, line := range SchemaErrorLines(result.Errors()) {
		msgs = append(msgs, "  - "+line)
	}
	return fmt.Errorf("%s: schema validation failed:\n%s", source, strings.Join(msgs, "\n"))
}

// normaliseForJSON recursively converts yaml.v2's map[interface{}]interface{}
// into map[string]interface{} so the value can be JSON-marshalled.
func normaliseForJSON(v interface{}) interface{} {
	switch x := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, val := range x {
			m[fmt.Sprint(k)] = normaliseForJSON(val)
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
