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

// Package podsecurity holds the Pod Security admission labels the
// operator sets on the namespaces it installs workloads into. A cluster
// can enforce a default level on every namespace without such labels,
// and some clusters, such as Talos, default to baseline.
package podsecurity

import (
	psaapi "k8s.io/pod-security-admission/api"
)

// PrivilegedNamespaceLabels returns the namespace labels that set Pod
// Security admission's enforce, audit and warn levels to privileged. The
// operator sets them on a namespace whose workloads need more than the
// baseline level allows, so that a cluster default stricter than
// privileged neither rejects those pods nor warns and audits about them
// on every install or upgrade.
func PrivilegedNamespaceLabels() map[string]string {
	level := string(psaapi.LevelPrivileged)
	return map[string]string{
		psaapi.EnforceLevelLabel: level,
		psaapi.AuditLevelLabel:   level,
		psaapi.WarnLevelLabel:    level,
	}
}
