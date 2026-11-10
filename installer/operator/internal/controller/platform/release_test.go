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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// expectChartCRDsEstablished waits until the API server serves every CRD the
// chart ships, so a later chart's resources of those kinds resolve.
func expectChartCRDsEstablished(chrt *chart.Chart) {
	GinkgoHelper()
	for _, want := range chartCRDs(chrt) {
		Eventually(func(g Gomega) {
			live := &apiextensionsv1.CustomResourceDefinition{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: want.Name}, live)).To(Succeed())
			established := false
			for _, c := range live.Status.Conditions {
				if c.Type == apiextensionsv1.Established && c.Status == apiextensionsv1.ConditionTrue {
					established = true
				}
			}
			g.Expect(established).To(BeTrue(), "%s is not established", want.Name)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	}
}

// normalisedCRDName names the CRD normalisedCRDChart ships.
const normalisedCRDName = "widgets.normalised.example.com"

// normalisedCRDChart ships a CRD whose printer column sets priority: 0, the
// default, which the API server drops on storage, so the stored CRD differs
// from the chart's file.
func normalisedCRDChart() *chart.Chart {
	return &chart.Chart{
		Metadata: &chart.Metadata{
			Name:       "normalised-crds",
			Version:    "1.0.0",
			APIVersion: chart.APIVersionV2,
		},
		Files: []*common.File{{
			Name: "crds/widgets.yaml",
			Data: []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ` + normalisedCRDName + `
spec:
  group: normalised.example.com
  scope: Cluster
  names:
    plural: widgets
    kind: Widget
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
    additionalPrinterColumns:
    - name: Age
      type: date
      jsonPath: .metadata.creationTimestamp
      priority: 0
`),
		}},
		Templates: []*common.File{{
			Name: "templates/cm.yaml",
			Data: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}
`),
		}},
	}
}

// The reconciler specs drive Helm through its in-memory client, which skips a
// chart's crds/ directory and applies nothing to the API server. These specs
// install charts that ship CRDs through the production client instead, so
// Helm meets the CRDs convergeRelease has applied.
var _ = Describe("Runtime release first install", func() {
	It("installs a chart whose CRDs the API server normalises on storage", func() {
		ensureNamespace(platformNamespace)
		chrt := normalisedCRDChart()

		hc, err := helm.NewClient(cfg, platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(hc.Uninstall(chrt.Name())).To(Succeed())
			crd := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: normalisedCRDName}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, crd))).To(Succeed())
		})

		res, err := convergeRelease(ctx, k8sClient, hc, chrt.Name(), chrt, map[string]any{})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Action).To(Equal(helm.ActionInstalled))
	})

	It("installs each runtime chart through Helm's production client", func() {
		ensureNamespace(platformNamespace)

		// PodSecurityStandards and no workshop engine keep session-manager
		// from rendering Kyverno policies, whose CRDs envtest lacks.
		cc := &configv1alpha1.EducatesClusterConfig{
			Status: configv1alpha1.EducatesClusterConfigStatus{
				Ingress: &configv1alpha1.StatusIngress{
					Domain:           "test.example.com",
					IngressClassName: "contour",
					Protocol:         configv1alpha1.IngressProtocolHTTPS,
					WildcardCertificateSecretRef: &configv1alpha1.NamespacedSecretRef{
						Namespace: testOperatorNamespace,
						Name:      "wildcard-tls",
					},
				},
				PolicyEnforcement: &configv1alpha1.StatusPolicyEnforcement{
					ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEnginePodSecurityStandards,
					WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineNone,
				},
			},
		}

		// A dry-run create returns each component resource with the CRD's
		// defaults applied, as the reconciler reads it, and stores nothing.
		smgr := &platformv1alpha1.SecretsManager{ObjectMeta: metav1.ObjectMeta{Name: singletonName}}
		Expect(k8sClient.Create(ctx, smgr, client.DryRunAll)).To(Succeed())
		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "educates-api"},
			},
		}
		Expect(k8sClient.Create(ctx, ls, client.DryRunAll)).To(Succeed())
		sm := &platformv1alpha1.SessionManager{ObjectMeta: metav1.ObjectMeta{Name: singletonName}}
		Expect(k8sClient.Create(ctx, sm, client.DryRunAll)).To(Succeed())

		releases := []struct {
			name   string
			load   func() (*chart.Chart, error)
			values map[string]any
		}{
			{secretsManagerReleaseName, vendoredcharts.SecretsManager, renderSecretsManagerValues(smgr, cc, testImageNamespace)},
			{lookupServiceReleaseName, vendoredcharts.LookupService, renderLookupServiceValues(ls, cc, testImageNamespace)},
			{sessionManagerReleaseName, vendoredcharts.SessionManager, renderSessionManagerValues(sm, cc, testOperatorNamespace, testImageNamespace)},
		}

		for _, rel := range releases {
			chrt, err := rel.load()
			Expect(err).NotTo(HaveOccurred())

			// The operator builds a Helm client for every reconcile.
			hc, err := helm.NewClient(cfg, platformNamespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				Expect(hc.Uninstall(rel.name)).To(Succeed())
			})

			res, err := convergeRelease(ctx, k8sClient, hc, rel.name, chrt, rel.values)
			Expect(err).NotTo(HaveOccurred(), "first install of %s", rel.name)
			Expect(res.Action).To(Equal(helm.ActionInstalled))

			expectChartCRDsEstablished(chrt)
		}
	})
})
