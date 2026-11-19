// Package crds applies the four platform CRDs from the embedded
// operator chart to the cluster before helm-install.
//
// Why this exists: helm intentionally only installs CRDs from a
// chart's crds/ directory on first install — never on upgrade. After
// a CRD shape change, users would have to run
// `kubectl apply -f installer/charts/educates-installer/crds/` by
// hand for the new schema to land. Owning CRD lifecycle from deploy
// removes that step.
//
// The deploy flow passes SkipCRDs=true to helm so the two paths
// don't fight over CRD ownership.
package crds

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"

	helmchart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
)

// GVK is apiextensions.k8s.io/v1 CustomResourceDefinition — used to
// poll Established=True on freshly-applied CRDs, and to read the live
// CRD a deploy preview compares with.
var GVK = schema.GroupVersionKind{
	Group:   "apiextensions.k8s.io",
	Version: "v1",
	Kind:    "CustomResourceDefinition",
}

// establishedPollInterval and establishedTimeout bound the post-apply
// wait. CRD establishment is normally sub-second on a healthy apiserver;
// the timeout is just a safety net against a wedged apiserver.
const (
	establishedPollInterval = 250 * time.Millisecond
	establishedTimeout      = 60 * time.Second
)

// Apply pushes every CRD found in chrt.CRDObjects() via SSA. Returns
// the list of GroupKind+Name strings applied so the deploy summary can
// log them. Idempotent: re-runs converge on the latest schema.
func Apply(ctx context.Context, applier *apply.Client, chrt *helmchart.Chart) ([]string, error) {
	objects, err := Objects(chrt)
	if err != nil {
		return nil, err
	}
	var applied []string
	for _, u := range objects {
		if _, err := applier.Apply(ctx, u); err != nil {
			return nil, fmt.Errorf("apply CRD %s: %w", u.GetName(), err)
		}
		applied = append(applied, u.GetName())
	}
	if err := waitEstablished(ctx, applier, applied); err != nil {
		return applied, err
	}
	// The mapper's discovery cache was populated before these CRDs
	// existed. Invalidate it so the next CR apply re-fetches /apis and
	// resolves the new kinds.
	applier.InvalidateDiscovery()
	return applied, nil
}

// Objects returns the CRDs in chrt.CRDObjects(), the ones Apply applies,
// in the order the chart holds them.
func Objects(chrt *helmchart.Chart) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	for _, crdEntry := range chrt.CRDObjects() {
		if crdEntry.File == nil || len(crdEntry.File.Data) == 0 {
			continue
		}
		docs, err := splitYAMLDocs(crdEntry.File.Data)
		if err != nil {
			return nil, fmt.Errorf("split CRD file %s: %w", crdEntry.Filename, err)
		}
		for _, doc := range docs {
			u := &unstructured.Unstructured{}
			if err := yaml.Unmarshal(doc, &u.Object); err != nil {
				return nil, fmt.Errorf("parse CRD doc in %s: %w", crdEntry.Filename, err)
			}
			if u.GetKind() != "CustomResourceDefinition" {
				// Defensive: helm's CRDObjects() should only return
				// objects from crds/, but a malformed chart could
				// slip something through.
				continue
			}
			objects = append(objects, u)
		}
	}
	return objects, nil
}

// waitEstablished polls each applied CRD until its Established=True
// condition flips. The apply call returns as soon as the CRD object
// lands, but the apiserver needs an extra moment to wire up the
// discovery endpoint for the new kind. Without this gate, the very next
// CR apply races discovery and surfaces as
// "no matches for kind ... in version ...".
func waitEstablished(ctx context.Context, applier *apply.Client, names []string) error {
	deadline := time.Now().Add(establishedTimeout)
	for _, name := range names {
		for {
			obj, err := applier.Get(ctx, GVK, "", name)
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("get CRD %s: %w", name, err)
			}
			if err == nil && isEstablished(obj) {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for CRD %s to become Established", name)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(establishedPollInterval):
			}
		}
	}
	return nil
}

// isEstablished returns true when the CRD's Established condition is True.
// NamesAccepted is implicit — Established only flips after NamesAccepted.
func isEstablished(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
	conds, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == "Established" && m["status"] == "True" {
			return true
		}
	}
	return false
}

// splitYAMLDocs handles multi-document YAML files (---  separated).
// helm doesn't promise that each chart file holds exactly one CRD —
// the upstream convention is one-per-file but the format allows
// stacking.
func splitYAMLDocs(data []byte) ([][]byte, error) {
	dec := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	var out [][]byte
	for {
		doc, err := dec.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		out = append(out, doc)
	}
	return out, nil
}
