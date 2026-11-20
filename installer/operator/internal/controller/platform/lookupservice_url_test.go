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

package platform

import (
	"testing"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
)

func TestLookupServiceURL_UsesThePublishedProtocol(t *testing.T) {
	for _, tc := range []struct {
		name     string
		protocol configv1alpha1.IngressProtocol
		want     string
	}{
		{"https", configv1alpha1.IngressProtocolHTTPS, "https://lookup.educates.example.com"},
		{"http", configv1alpha1.IngressProtocolHTTP, "http://lookup.educates.example.com"},
		{"a cluster config that publishes no protocol", "", "https://lookup.educates.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := &platformv1alpha1.LookupService{}
			obj.Spec.Ingress.Prefix = "lookup"
			cfg := &configv1alpha1.EducatesClusterConfig{}
			cfg.Status.Ingress = &configv1alpha1.StatusIngress{Domain: "educates.example.com", Protocol: tc.protocol}

			if got := lookupServiceURL(obj, cfg); got != tc.want {
				t.Errorf("lookupServiceURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
