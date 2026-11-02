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

package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/discovery"

	"github.com/educates/educates-training-platform/installer/operator/internal/kyverno"
)

// The reconcilers' specs answer the Kyverno presence check with a fake,
// since envtest serves no Kyverno. This one asks the API server itself,
// through the discovery client production wires, that a cluster without
// Kyverno reads as Kyverno missing rather than as a discovery failure.
var _ = Describe("Kyverno presence check against an API server", func() {
	It("reports every policy kind missing, without an error, when Kyverno is not installed", func() {
		disc, err := discovery.NewDiscoveryClientForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		missing, err := kyverno.MissingPolicyKinds(disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(Equal(kyverno.PolicyKinds))
	})
})
