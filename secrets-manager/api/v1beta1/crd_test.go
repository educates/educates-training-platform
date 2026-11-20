package v1beta1

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// chartCRDs is the crds/ directory of the secrets-manager subchart, the
// schema of record of the four kinds.
var chartCRDs = filepath.Join("..", "..", "..", "installer", "charts", "educates-training-platform", "charts",
	"secrets-manager", "crds")

// TestTypesMatchCRDs checks that each kind's Go type has a field for every
// property its CRD declares and none other, optional exactly where the CRD
// does not require the property.
func TestTypesMatchCRDs(t *testing.T) {
	tests := []struct {
		file string
		obj  any
	}{
		{"secretcopier.yaml", SecretCopier{}},
		{"secretexporter.yaml", SecretExporter{}},
		{"secretimporter.yaml", SecretImporter{}},
		{"secretinjector.yaml", SecretInjector{}},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(chartCRDs, test.file))
			if err != nil {
				t.Fatal(err)
			}
			crd := map[string]any{}
			if err := yaml.Unmarshal(raw, &crd); err != nil {
				t.Fatal(err)
			}
			versions := field[[]any](crd, "spec", "versions")
			if len(versions) != 1 {
				t.Fatalf("%s has %d versions, expected 1", test.file, len(versions))
			}
			version, _ := versions[0].(map[string]any)
			schema := field[map[string]any](version, "schema", "openAPIV3Schema")
			for _, mismatch := range compareSchema("", schema, reflect.TypeOf(test.obj), true) {
				t.Error(mismatch)
			}
		})
	}
}

// compareSchema lists the differences between the schema at path and the
// Go type goType. At the root, the type's embedded type and object
// metadata stand for apiVersion, kind and metadata.
func compareSchema(path string, schema map[string]any, goType reflect.Type, root bool) []string {
	for goType.Kind() == reflect.Pointer {
		goType = goType.Elem()
	}
	schemaType, _ := schema["type"].(string)
	switch schemaType {
	case "object":
		properties, hasProperties := schema["properties"].(map[string]any)
		if !hasProperties {
			if goType.Kind() != reflect.Map {
				return []string{path + ": the CRD has a map, the Go type " + goType.String()}
			}
			return nil
		}
		if goType.Kind() != reflect.Struct {
			return []string{path + ": the CRD has an object, the Go type " + goType.String()}
		}
		return compareObject(path, schema, properties, goType, root)
	case "array":
		if goType.Kind() != reflect.Slice {
			return []string{path + ": the CRD has an array, the Go type " + goType.String()}
		}
		items, _ := schema["items"].(map[string]any)
		return compareSchema(path+"[]", items, goType.Elem(), false)
	case "string":
		if goType.Kind() != reflect.String {
			return []string{path + ": the CRD has a string, the Go type " + goType.String()}
		}
	case "boolean":
		if goType.Kind() != reflect.Bool {
			return []string{path + ": the CRD has a boolean, the Go type " + goType.String()}
		}
	default:
		return []string{path + ": the CRD has a property of type " + schemaType + ", which the check does not know"}
	}
	return nil
}

// compareObject lists the differences between the properties of an object
// schema and the JSON fields of a struct.
func compareObject(path string, schema, properties map[string]any, goType reflect.Type, root bool) []string {
	required, _ := schema["required"].([]any)
	fields := jsonFields(goType)
	var mismatches []string
	for name, property := range properties {
		propertySchema, _ := property.(map[string]any)
		f, ok := fields[name]
		if !ok {
			// A free-form object, such as a status the CRD does not
			// describe, needs no Go field.
			if _, described := propertySchema["properties"]; !described &&
				propertySchema["x-kubernetes-preserve-unknown-fields"] == true {
				continue
			}
			mismatches = append(mismatches, path+"."+name+": in the CRD, not in the Go type")
			continue
		}
		if isRequired := slices.Contains(required, any(name)); isRequired == f.optional {
			mismatches = append(mismatches, path+"."+name+": required in the CRD and optional in the Go type, "+
				"or the other way round")
		}
		mismatches = append(mismatches, compareSchema(path+"."+name, propertySchema, f.goType, false)...)
	}
	for name := range fields {
		if _, ok := properties[name]; ok || root && slices.Contains(objectFields, name) {
			continue
		}
		mismatches = append(mismatches, path+"."+name+": in the Go type, not in the CRD")
	}
	return mismatches
}

// objectFields are the fields every Kubernetes object has, which a CRD's
// schema need not declare.
var objectFields = []string{"apiVersion", "kind", "metadata"}

// jsonField is a field of a struct as JSON names it.
type jsonField struct {
	goType   reflect.Type
	optional bool
}

// jsonFields maps the JSON names of a struct's fields, those of embedded
// structs included, to the fields.
func jsonFields(goType reflect.Type) map[string]jsonField {
	fields := map[string]jsonField{}
	for f := range goType.Fields() {
		tag := f.Tag.Get("json")
		name, options, _ := strings.Cut(tag, ",")
		switch {
		case options == "inline":
			maps.Copy(fields, jsonFields(f.Type))
		case name == "" || name == "-":
			continue
		default:
			fields[name] = jsonField{goType: f.Type, optional: strings.Contains(options, "omitempty")}
		}
	}
	return fields
}

// field returns the value at path in obj, or the zero value of T.
func field[T any](obj map[string]any, path ...string) T {
	var current any = obj
	for _, key := range path {
		m, _ := current.(map[string]any)
		current = m[key]
	}
	value, _ := current.(T)
	return value
}
