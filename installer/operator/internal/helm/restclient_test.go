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

package helm

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// Helm's resource builder sets GroupVersion and APIPath on the config
// ToRESTConfig returns before it builds each REST client. Every Helm client
// the operator creates shares one *rest.Config, so a returned config the
// caller may change must be its own: on a shared one, two Helm operations
// running at once, such as cert-manager and session-manager upgrading
// together, build clients for each other's API groups.
func TestRESTClientGetter_ToRESTConfigReturnsAConfigOfItsOwn(t *testing.T) {
	shared := &rest.Config{Host: "https://cluster.example.com"}
	getter := newRESTClientGetter(shared, "default")

	first, err := getter.ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig: %v", err)
	}
	first.GroupVersion = &schema.GroupVersion{Group: "rbac.authorization.k8s.io", Version: "v1"}
	first.APIPath = "/apis"

	second, err := getter.ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig: %v", err)
	}
	if second.GroupVersion != nil || second.APIPath != "" {
		t.Errorf("second config carries the first caller's GroupVersion %v and APIPath %q", second.GroupVersion, second.APIPath)
	}
	if shared.GroupVersion != nil || shared.APIPath != "" {
		t.Errorf("shared config carries a caller's GroupVersion %v and APIPath %q", shared.GroupVersion, shared.APIPath)
	}
	if second.Host != shared.Host {
		t.Errorf("Host = %q, want %q", second.Host, shared.Host)
	}
}
