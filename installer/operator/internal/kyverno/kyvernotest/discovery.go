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

// Package kyvernotest provides a kyverno.GroupVersionDiscovery for
// reconciler tests, whose envtest API server serves no Kyverno.
package kyvernotest

import (
	"sync/atomic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/educates/educates-training-platform/installer/operator/internal/kyverno"
)

// Discovery answers as a cluster with Kyverno installed, serving every
// kyverno.PolicyKinds kind, until SetServed(false) makes it answer as a
// cluster without Kyverno. It is safe to flip while a manager runs.
type Discovery struct {
	notServed atomic.Bool
}

// SetServed sets whether the Kyverno policy kinds are served.
func (d *Discovery) SetServed(served bool) {
	d.notServed.Store(!served)
}

// ServerResourcesForGroupVersion implements kyverno.GroupVersionDiscovery.
func (d *Discovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if d.notServed.Load() {
		return nil, apierrors.NewNotFound(schema.GroupResource{}, groupVersion)
	}
	list := &metav1.APIResourceList{GroupVersion: groupVersion}
	for _, gvk := range kyverno.PolicyKinds {
		if gvk.GroupVersion().String() == groupVersion {
			list.APIResources = append(list.APIResources, metav1.APIResource{Kind: gvk.Kind})
		}
	}
	return list, nil
}
