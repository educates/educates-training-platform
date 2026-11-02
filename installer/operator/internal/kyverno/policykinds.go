/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package kyverno answers whether the cluster serves the Kyverno policy
// kinds Educates creates. Workshop security is enforced by Kyverno: the
// session-manager chart installs the bundled cluster-wide policies, and
// the session-manager runtime creates the per-workshop policies for each
// workshop environment. Both fail when Kyverno is absent, so whenever a
// policy engine resolves to Kyverno the operator asks this package first,
// whether the operator installed Kyverno or the administrator did.
package kyverno

import (
	"errors"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PolicyKinds are the Kyverno policy kinds and API versions the
// session-manager chart's bundled policies are written in.
var PolicyKinds = []schema.GroupVersionKind{
	{Group: "policies.kyverno.io", Version: "v1alpha1", Kind: "ValidatingPolicy"},
	{Group: "policies.kyverno.io", Version: "v1", Kind: "ValidatingPolicy"},
}

// PresencePollInterval is how often a reconciler waiting for Kyverno
// checks again. Nothing a reconciler watches changes when Kyverno is
// installed, so it polls.
const PresencePollInterval = 15 * time.Second

// GroupVersionDiscovery is the part of a discovery client the presence
// check needs. discovery.DiscoveryInterface satisfies it; it should be an
// uncached client, so that a Kyverno installed after the operator started
// is seen.
type GroupVersionDiscovery interface {
	ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error)
}

// MissingPolicyKinds returns the PolicyKinds the API server does not
// serve, in PolicyKinds order; none means Kyverno is present. A group
// version the server does not serve counts as missing, not as an error.
// Any other discovery failure is returned, so a transient API error is
// retried instead of being reported as a missing Kyverno.
func MissingPolicyKinds(d GroupVersionDiscovery) ([]schema.GroupVersionKind, error) {
	if d == nil {
		return nil, errors.New("no Kyverno discovery client configured")
	}
	served := map[string]map[string]bool{}
	var missing []schema.GroupVersionKind
	for _, gvk := range PolicyKinds {
		gv := gvk.GroupVersion().String()
		kinds, ok := served[gv]
		if !ok {
			list, err := d.ServerResourcesForGroupVersion(gv)
			switch {
			case apierrors.IsNotFound(err):
				kinds = map[string]bool{}
			case err != nil:
				return nil, fmt.Errorf("discover %s: %w", gv, err)
			default:
				kinds = map[string]bool{}
				for _, res := range list.APIResources {
					kinds[res.Kind] = true
				}
			}
			served[gv] = kinds
		}
		if !kinds[gvk.Kind] {
			missing = append(missing, gvk)
		}
	}
	return missing, nil
}

// NotServedMessage explains a non-empty MissingPolicyKinds result. It
// holds for a cluster without Kyverno and for one whose Kyverno is too old
// to serve every kind.
func NotServedMessage(missing []schema.GroupVersionKind) string {
	names := make([]string, 0, len(missing))
	for _, gvk := range missing {
		names = append(names, fmt.Sprintf("%s (%s)", gvk.Kind, gvk.GroupVersion()))
	}
	return "the API server does not serve " + strings.Join(names, ", ") +
		", which Educates needs to enforce workshop security: install Kyverno, or upgrade it to a release that serves them"
}
