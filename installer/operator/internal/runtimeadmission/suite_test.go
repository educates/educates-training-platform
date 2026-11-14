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

package runtimeadmission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"

	"github.com/educates/educates-training-platform/installer/operator/internal/crds"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	"github.com/educates/educates-training-platform/installer/operator/internal/platformnamespace"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

const (
	// trainingPortalRole is the ClusterRole session-manager binds each
	// training portal's service account to.
	trainingPortalRole = "educates:training-portal"

	// sessionManagerCoreRole holds session-manager's own rules on the
	// training resources. The chart binds session-manager to an aggregate
	// role that collects it, but envtest runs no controller manager to
	// aggregate roles, so the suite binds session-manager to it directly.
	sessionManagerCoreRole = "educates:session-manager:core"
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestRuntimeAdmission(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Runtime Admission Policy Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	Expect(apiextensionsv1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{}
	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	By("applying the session-manager chart's CRDs, admission policies and roles")
	chrt, err := vendoredcharts.SessionManager()
	Expect(err).NotTo(HaveOccurred())
	Expect(crds.ApplyChart(ctx, k8sClient, chrt)).To(Succeed())
	expectCRDsEstablished(chrt)

	for _, obj := range renderedObjects(chrt) {
		if !appliedFromChart(obj) {
			continue
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed(), "create %s %s", obj.GetKind(), obj.GetName())
	}

	bindServiceAccount(platformnamespace.Name, "session-manager", sessionManagerCoreRole)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

// renderedObjects installs the session-manager chart into a memory-backed
// Helm client, as the operator's chart tests do, and returns every object
// of the resulting manifest.
func renderedObjects(chrt *chart.Chart) []*unstructured.Unstructured {
	GinkgoHelper()

	hc, err := helm.NewMemoryClient(platformnamespace.Name)
	Expect(err).NotTo(HaveOccurred())
	values := map[string]any{
		"clusterIngress": map[string]any{"domain": "educates.invalid"},
	}
	rel, err := hc.Install(ctx, chrt.Metadata.Name, chrt, values)
	Expect(err).NotTo(HaveOccurred())

	var objs []*unstructured.Unstructured
	for doc := range strings.SplitSeq(rel.Manifest, "\n---") {
		obj := &unstructured.Unstructured{}
		Expect(yaml.Unmarshal([]byte(doc), &obj.Object)).To(Succeed())
		if len(obj.Object) == 0 {
			continue
		}
		objs = append(objs, obj)
	}
	return objs
}

// appliedFromChart reports whether the suite applies obj: every admission
// policy and binding the chart ships, and the roles of the identities the
// specs impersonate.
func appliedFromChart(obj *unstructured.Unstructured) bool {
	switch obj.GetKind() {
	case "ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding":
		return true
	case "ClusterRole":
		return obj.GetName() == trainingPortalRole || obj.GetName() == sessionManagerCoreRole
	}
	return false
}

// expectCRDsEstablished waits until the API server serves every CRD the
// chart ships.
func expectCRDsEstablished(chrt *chart.Chart) {
	GinkgoHelper()

	for _, obj := range chrt.CRDObjects() {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(yaml.Unmarshal(obj.File.Data, crd)).To(Succeed())
		Eventually(func(g Gomega) {
			live := &apiextensionsv1.CustomResourceDefinition{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: crd.Name}, live)).To(Succeed())
			established := false
			for _, cond := range live.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					established = true
				}
			}
			g.Expect(established).To(BeTrue(), "CRD %s not established", crd.Name)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	}
}

// bindServiceAccount binds the service account to the ClusterRole, as
// session-manager binds a training portal's service account and the chart
// binds session-manager's.
func bindServiceAccount(namespace, name, role string) {
	GinkgoHelper()

	binding := &rbacv1.ClusterRoleBinding{}
	binding.Name = role + ":" + namespace + ":" + name
	binding.RoleRef = rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role}
	binding.Subjects = []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Namespace: namespace, Name: name}}
	Expect(k8sClient.Create(ctx, binding)).To(Succeed())
}

// impersonating returns a client that sends every request as user.
// Impersonating a service account's user name gives the request that
// account's groups as well, as its token would.
func impersonating(user string) client.Client {
	GinkgoHelper()

	impersonated := rest.CopyConfig(cfg)
	impersonated.Impersonate = rest.ImpersonationConfig{UserName: user}
	cl, err := client.New(impersonated, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	return cl
}

// serviceAccountUser is the user name a service account authenticates as.
func serviceAccountUser(namespace, name string) string {
	return "system:serviceaccount:" + namespace + ":" + name
}

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
