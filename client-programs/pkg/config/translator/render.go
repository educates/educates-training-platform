package translator

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// RenderCRs serialises the four (or three) platform CRs in Output as a
// single multi-document YAML stream, in deploy order:
//  1. EducatesClusterConfig
//  2. SecretsManager
//  3. LookupService (omitted when nil)
//  4. SessionManager
//
// The order matches the controller dependency chain: ECC must be Ready
// before SecretsManager reconciles; SecretsManager must be Ready before
// SessionManager. LookupService is independent of SessionManager.
//
// yaml.v3 is used so the output has stable, alphabetical key ordering
// (yaml.v2 emits Go-map iteration order, which is randomised).
func RenderCRs(out *Output) ([]byte, error) {
	docs := []map[string]interface{}{out.EducatesClusterConfig, out.SecretsManager}
	if out.LookupService != nil {
		docs = append(docs, out.LookupService)
	}
	docs = append(docs, out.SessionManager)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return nil, fmt.Errorf("encode CR %q: %w", doc["kind"], err)
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderOperatorValues serialises OperatorChartValues as a YAML values
// file, suitable for `helm install -f`. Empty map renders as "{}\n".
func RenderOperatorValues(out *Output) ([]byte, error) {
	if out.OperatorChartValues == nil {
		return []byte("{}\n"), nil
	}
	// Round-trip through JSON so key ordering is alphabetical (Go maps
	// in YAML emit in iteration order; round-trip through encoding/json
	// then yaml.v3 gives us a stable shape).
	raw, err := json.Marshal(out.OperatorChartValues)
	if err != nil {
		return nil, err
	}
	var generic interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(generic); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
