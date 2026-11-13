package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v2"
)

// Load reads a CLI config file, validates its apiVersion/kind, runs JSON
// schema validation, then strict-unmarshals into the typed struct. The
// returned value implements v1alpha1.Config; callers type-switch to the
// concrete kind.
func Load(path string) (v1alpha1.Config, error) {
	data, err := ReadConfigFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data, path)
}

// ReadConfigFile reads the CLI config file at path. A config of a local
// cluster in the data home that still declares registry mirrors in its
// cluster block has them moved to the registry mirrors file first, once,
// with a note on standard error (MoveClusterRegistryMirrors), so every
// command that reads such a config moves it. A failed read is an error
// naming path and wrapping the cause, such as fs.ErrNotExist.
func ReadConfigFile(path string) ([]byte, error) {
	if err := MoveClusterRegistryMirrors(utils.GetEducatesHomeDir(), path, os.Stderr); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// LoadBytes is the path-free variant — useful for stdin and tests. The
// source string is woven into error messages so users can locate the file.
//
// Single-pass: one yaml.Unmarshal → normalise → json.Marshal. The JSON
// bytes drive both schema validation and the typed strict decode (via
// json.Decoder.DisallowUnknownFields, which is the json equivalent of
// yaml.UnmarshalStrict's behaviour around unknown fields).
func LoadBytes(data []byte, source string) (v1alpha1.Config, error) {
	cfg, err := LoadBytesWithoutDefaults(data, source)
	if err != nil {
		return nil, err
	}
	return ApplyDefaults(cfg), nil
}

// LoadWithoutDefaults is Load without the per-kind static defaults: the
// config holds only what the file declares, so a caller can tell a field
// left out from one set to its default value. ApplyDefaults then gives it
// the defaults Load would have.
func LoadWithoutDefaults(path string) (v1alpha1.Config, error) {
	data, err := ReadConfigFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytesWithoutDefaults(data, path)
}

// LoadBytesWithoutDefaults is LoadBytes without the per-kind static
// defaults, as LoadWithoutDefaults is to Load.
func LoadBytesWithoutDefaults(data []byte, source string) (v1alpha1.Config, error) {
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
	if err := checkNoResolverTargetAddress(raw, kind); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if err := checkNoClusterRegistryMirrors(raw, kind); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}

	switch kind {
	case v1alpha1.KindEducatesLocalConfig:
		return decode(jsonData, schemas.EducatesLocalConfig, source, &v1alpha1.EducatesLocalConfig{}, true)
	case v1alpha1.KindEducatesConfig:
		// Escape-hatch: CR-spec fields are untyped maps; don't reject
		// unknown fields inside them (the typed struct only declares
		// the envelope; the CR specs are map[string]interface{} that
		// json's strict mode would happily accept any keys for anyway).
		return decode(jsonData, schemas.EducatesConfig, source, &v1alpha1.EducatesConfig{}, false)
	case v1alpha1.KindEducatesInlineConfig:
		return decode(jsonData, schemas.EducatesInlineConfig, source, &v1alpha1.EducatesInlineConfig{}, true)
	case v1alpha1.KindEducatesGKEConfig:
		return decode(jsonData, schemas.EducatesGKEConfig, source, &v1alpha1.EducatesGKEConfig{}, true)
	case v1alpha1.KindEducatesEKSConfig:
		return decode(jsonData, schemas.EducatesEKSConfig, source, &v1alpha1.EducatesEKSConfig{}, true)
	default:
		return nil, fmt.Errorf("%s: unknown kind %q for apiVersion %q", source, kind, apiVersion)
	}
}

// checkNoResolverTargetAddress returns an error saying what replaces
// resolver.targetAddress when raw, the top level of a config of kind
// kind, still sets it: resolver.targetAddress in an EducatesLocalConfig,
// or target.resolver.targetAddress in an EducatesConfig. The field was
// removed, so the error comes before the schema's generic one, or in
// place of the EducatesConfig's loose decode dropping it silently.
func checkNoResolverTargetAddress(raw map[string]interface{}, kind string) error {
	var resolverPath []string
	listenAddressField := "cluster.listenAddress"
	switch kind {
	case v1alpha1.KindEducatesLocalConfig:
		resolverPath = []string{"resolver"}
	case v1alpha1.KindEducatesConfig:
		resolverPath = []string{"target", "resolver"}
		listenAddressField = "target.cluster.listenAddress"
	default:
		return nil
	}
	resolver, _ := walkV3(raw, resolverPath...)
	if _, set := asMap(resolver)["targetAddress"]; !set {
		return nil
	}
	resolverField := strings.Join(resolverPath, ".")
	return fmt.Errorf("%s.targetAddress was removed: remove it from the config. A local cluster's names, "+
		"its ingress domain and %s.extraDomains, now resolve to its %s, a specific IP address of the host. "+
		"Names that resolve anywhere else go in "+
		"dnsmasq configuration files of your own, %s",
		resolverField, resolverField, listenAddressField,
		filepath.Join(DnsmasqIncludeDir(utils.GetEducatesHomeDir()), "*.conf"))
}

// checkNoClusterRegistryMirrors returns an error saying where registry
// mirrors are declared now when raw, the top level of a config of kind
// kind, still declares them in its cluster block: cluster.registryMirrors
// in an EducatesLocalConfig, or target.cluster.registryMirrors in an
// EducatesConfig. Every local cluster takes its registry mirrors from the
// registry mirrors file of the data home, which local mirror deploy
// writes, so the error names that file and shows the command that records
// each mirror there, never its password. It comes before the schema's
// generic error, or in place of the EducatesConfig's loose decode
// dropping the field silently.
//
// A config in the data home never gets here, since reading it moves the
// field to the registry mirrors file (MoveClusterRegistryMirrors).
func checkNoClusterRegistryMirrors(raw map[string]interface{}, kind string) error {
	clusterPath := clusterBlockPath(kind)
	if clusterPath == nil {
		return nil
	}
	cluster, _ := walkV3(raw, clusterPath...)
	mirrors, set := asMap(cluster)["registryMirrors"]
	if !set {
		return nil
	}
	field := strings.Join(append(clusterPath, "registryMirrors"), ".")
	var commands, withPassword []string
	list, _ := mirrors.([]interface{})
	for _, m := range list {
		entry := asMap(m)
		name := strMap(entry, "mirror")
		if name == "" {
			continue
		}
		command := "  educates local mirror deploy " + name
		if url := strMap(entry, "url"); url != "" {
			command += " --url " + url
		}
		if username := strMap(entry, "username"); username != "" {
			command += " --username " + username
		}
		commands = append(commands, command)
		if strMap(entry, "password") != "" {
			withPassword = append(withPassword, name)
		}
	}
	removed := fmt.Sprintf("%s was removed: registry mirrors are now declared once, for every local cluster, in %s, "+
		"which 'educates local mirror deploy' and 'educates local mirror delete' write",
		field, LocalMirrorsPath(utils.GetEducatesHomeDir()))
	if len(commands) == 0 {
		return fmt.Errorf("%s. Remove %s from the config, and record any registry mirror there with "+
			"'educates local mirror deploy <name>'", removed, field)
	}
	password := ""
	switch len(withPassword) {
	case 0:
	case 1:
		password = fmt.Sprintf("adding --password <password> to the command of %s, which the config gives a password, ",
			withPassword[0])
	default:
		password = fmt.Sprintf("adding --password <password> to the commands of %s, which the config gives passwords, ",
			strings.Join(withPassword, ", "))
	}
	return fmt.Errorf("%s. Record the registry mirrors of this config there with:\n\n%s\n\n%sand remove %s from the config",
		removed, strings.Join(commands, "\n"), password, field)
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

// decode validates jsonData against schemaBytes, then strict-decodes it
// into target (or loose for the escape kind which holds untyped maps).
func decode(
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
	return target, nil
}

// ApplyDefaults applies the per-kind static defaults Load gives a config
// to cfg, and returns it. An EducatesConfig gets none.
func ApplyDefaults(cfg v1alpha1.Config) v1alpha1.Config {
	switch t := cfg.(type) {
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
	return cfg
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
