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

// Package ownership names how the operator marks the resources it
// creates: the server-side apply field manager it applies them under
// and the managed-by label it stamps on them. Every reconciler uses
// these, so SSA conflicts surface predictably, `kubectl get -o yaml`
// shows a single owner for operator-created resources, and
// `kubectl get -l app.kubernetes.io/managed-by=educates-installer`
// lists the operator's footprint.
package ownership

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// FieldManager is the server-side apply field manager the operator
	// asserts ownership under.
	FieldManager = "educates-installer"

	// ManagedByLabel and ManagedByValue are the label the operator
	// stamps on what it creates.
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "educates-installer"
)

// RestoreLabels sets each of labels on obj, adding missing keys and
// restoring changed values, and leaves obj's other labels alone. It
// reports whether it changed obj, so the caller patches only then.
func RestoreLabels(obj metav1.Object, labels map[string]string) bool {
	current := obj.GetLabels()
	changed := false
	for k, v := range labels {
		if existing, ok := current[k]; ok && existing == v {
			continue
		}
		if current == nil {
			current = map[string]string{}
		}
		current[k] = v
		changed = true
	}
	if changed {
		obj.SetLabels(current)
	}
	return changed
}
