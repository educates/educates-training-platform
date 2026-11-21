package scenarios

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// unrelatedLabel is a label scenarios put on a namespace that no rule
// selects by.
const unrelatedLabel = "example.com/unrelated"

// rejectSecretWrites has the API server reject every write of a Secret in
// the namespaces labelled with the scenario's scope, ns among them, until
// the function it returns is called. Lifting the rejection changes nothing
// the secrets-manager watches.
func (s *scenario) rejectSecretWrites(ns *corev1.Namespace) (lift func()) {
	GinkgoHelper()
	fail := admissionregistrationv1.Fail
	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: s.name("reject-secret-writes")},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: &fail,
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{{
					RuleWithOperations: admissionregistrationv1.RuleWithOperations{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create, admissionregistrationv1.Update,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"},
						},
					},
				}},
			},
			Validations: []admissionregistrationv1.Validation{{
				Expression: "false",
				Message:    "Secrets are not written in this namespace",
			}},
		},
	}
	s.create(policy)
	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: s.name("reject-secret-writes")},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName:        policy.Name,
			ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Deny},
			MatchResources: &admissionregistrationv1.MatchResources{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: s.scope()},
			},
		},
	}
	s.create(binding)

	eventually(func(g Gomega) {
		probe := newSecret(ns.Name, s.name("probe"), data("probe", "p"))
		g.Expect(target.Client.Create(context.Background(), probe, client.DryRunAll)).NotTo(Succeed(),
			"writes of Secrets in %s should be rejected", ns.Name)
	})
	return func() {
		GinkgoHelper()
		s.deleteObject(binding)
	}
}

var _ = Describe("Convergence", Label(areaConvergence), func() {
	It("makes no API calls reconciling a copier whose sources, copies and spec have not changed",
		Label(expectedDifference), func() {
			requireAPICalls()
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			a := s.namespace("a", nil)
			b := s.namespace("b", nil)
			other := s.namespace("other", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(a, b)))
			eventually(func(g Gomega) {
				expectCopy(g, a.Name, src.Name, src, ruleOf(copier))
				expectCopy(g, b.Name, src.Name, src, ruleOf(copier))
			})
			calls := settledAPICalls(s.id)

			// Events that reconcile the copier, changing none of its
			// sources, copies or spec: a selected namespace relabelled,
			// still selected, and a Secret of the copy's name created where
			// the rule does not copy.
			s.update(a, func() { setMapEntry(&a.Labels, unrelatedLabel, "yes") })
			s.secret(other.Name, src.Name, data("token", "unrelated"))

			consistently(func(g Gomega) {
				g.Expect(target.APICalls.About(s.id)).To(Equal(calls))
			})
		})

	It("makes no API calls reconciling an exporter whose source, copies, importers and spec have not changed",
		Label(expectedDifference), func() {
			requireAPICalls()
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			accepting := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
			exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(accepting)))
			s.secretImporter(accepting.Name, src.Name, sharedSecret(uidOf(exporter)))
			eventually(func(g Gomega) {
				expectCopy(g, accepting.Name, src.Name, src, ruleOf(exporter))
			})
			calls := settledAPICalls(s.id)

			// An event that reconciles the exporter, changing none of its
			// source, copies, importers or spec: the target namespace
			// relabelled, still selected.
			s.update(accepting, func() { setMapEntry(&accepting.Labels, unrelatedLabel, "yes") })

			consistently(func(g Gomega) {
				g.Expect(target.APICalls.About(s.id)).To(Equal(calls))
			})
		})

	It("makes no API calls reconciling an injector whose Secrets, service accounts and spec have not changed",
		Label(expectedDifference), func() {
			requireAPICalls()
			s := newScenario()
			ns := s.namespace("a", nil)
			pull := s.pullSecret(ns.Name, pullSecretName)
			s.serviceAccount(ns.Name, builder, nil)
			s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byName(pull.Name), accounts(builder)))
			eventually(func(g Gomega) {
				g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(pull.Name))
			})
			calls := settledAPICalls(s.id)

			// An event that reconciles the injector, changing none of its
			// Secrets, service accounts or spec: the namespace relabelled,
			// still selected.
			s.update(ns, func() { setMapEntry(&ns.Labels, unrelatedLabel, "yes") })

			consistently(func(g Gomega) {
				g.Expect(target.APICalls.About(s.id)).To(Equal(calls))
			})
		})

	It("retries a failing target within the backoff cap, though nothing it watches changes", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		failing := s.namespace("a", s.scope())
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		lift := s.rejectSecretWrites(failing)
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(failing)))

		// The target fails long enough for a backoff without a cap to
		// outgrow the cap.
		consistentlyFor(2*target.Timing.Hold, func(g Gomega) {
			expectNoSecret(g, failing.Name, src.Name)
		})
		lift()

		Eventually(func(g Gomega) {
			expectCopy(g, failing.Name, src.Name, src, ruleOf(copier))
		}).WithTimeout(target.Timing.BackoffCap + 5*target.Timing.Poll).WithPolling(target.Timing.Poll).
			Should(Succeed())
	})

	It("repairs a copy whose change it missed within the backstop interval", Label(expectedDifference), func() {
		requireMissedEvents()
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		var copied *corev1.Secret
		eventually(func(g Gomega) {
			copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		DeferCleanup(target.MissedEvents.Miss(func(obj client.Object) bool {
			return obj.GetNamespace() == targetNS.Name
		}))

		s.update(copied, func() { copied.Data["token"] = []byte("tampered") })

		consistently(func(g Gomega) {
			g.Expect(secretIn(g, targetNS.Name, src.Name).Data).To(Equal(data("token", "tampered")))
		})
		Eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		}).WithTimeout(target.Timing.Backstop + target.Timing.Converge).WithPolling(target.Timing.Poll).
			Should(Succeed())
	})

	It("records on each copy the version of the source it was last written from", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(copied.Annotations).To(HaveKeyWithValue(annotationSourceVersion, src.ResourceVersion))
		})

		s.update(src, func() { src.Data["token"] = []byte("t1") })

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(copied.Annotations).To(HaveKeyWithValue(annotationSourceVersion, src.ResourceVersion))
		})
		written := src.ResourceVersion

		s.update(src, func() { setMapEntry(&src.Annotations, "example.com/touched", "yes") })

		consistently(func(g Gomega) {
			g.Expect(secretIn(g, targetNS.Name, src.Name).Annotations).
				To(HaveKeyWithValue(annotationSourceVersion, written))
		})
	})

	It("compares its copies once, without rewriting them, when a source change alters nothing they hold",
		Label(expectedDifference), func() {
			requireAPICalls()
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			a := s.namespace("a", nil)
			b := s.namespace("b", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(a, b)))
			eventually(func(g Gomega) {
				expectCopy(g, a.Name, src.Name, src, ruleOf(copier))
				expectCopy(g, b.Name, src.Name, src, ruleOf(copier))
			})
			copiedA := settledSecret(a.Name, src.Name)
			copiedB := settledSecret(b.Name, src.Name)
			before := settledAPICalls(s.id)

			s.update(src, func() { setMapEntry(&src.Annotations, "example.com/touched", "yes") })

			eventually(func(g Gomega) {
				g.Expect(target.APICalls.About(s.id).Reads).To(BeNumerically(">", before.Reads))
			})
			compared := settledAPICalls(s.id)
			s.update(a, func() { setMapEntry(&a.Labels, unrelatedLabel, "yes") })
			consistently(func(g Gomega) {
				g.Expect(target.APICalls.About(s.id)).To(Equal(compared))
				g.Expect(compared.Writes).To(Equal(before.Writes))
				g.Expect(secretIn(g, a.Name, src.Name).ResourceVersion).To(Equal(copiedA.ResourceVersion))
				g.Expect(secretIn(g, b.Name, src.Name).ResourceVersion).To(Equal(copiedB.ResourceVersion))
			})
		})

	It("compares each copy once more after a restart, without rewriting it", Label(expectedDifference), func() {
		requireAPICalls()
		requireRestart()
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		a := s.namespace("a", nil)
		b := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(a, b)))
		eventually(func(g Gomega) {
			expectCopy(g, a.Name, src.Name, src, ruleOf(copier))
			expectCopy(g, b.Name, src.Name, src, ruleOf(copier))
		})
		copiedA := settledSecret(a.Name, src.Name)
		copiedB := settledSecret(b.Name, src.Name)
		before := settledAPICalls(s.id)
		copyA, copyB := "/namespaces/"+a.Name+"/", "/namespaces/"+b.Name+"/"
		readsA, readsB := target.APICalls.About(copyA).Reads, target.APICalls.About(copyB).Reads

		Expect(target.Restart()).To(Succeed())

		eventually(func(g Gomega) {
			g.Expect(target.APICalls.About(copyA).Reads).To(BeNumerically(">", readsA))
			g.Expect(target.APICalls.About(copyB).Reads).To(BeNumerically(">", readsB))
		})
		compared := settledAPICalls(s.id)
		consistently(func(g Gomega) {
			g.Expect(target.APICalls.About(s.id)).To(Equal(compared))
			g.Expect(compared.Writes).To(Equal(before.Writes))
			g.Expect(secretIn(g, a.Name, src.Name).ResourceVersion).To(Equal(copiedA.ResourceVersion))
			g.Expect(secretIn(g, b.Name, src.Name).ResourceVersion).To(Equal(copiedB.ResourceVersion))
		})
	})
})
