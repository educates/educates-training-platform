package scenarios

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("Copy ownership and reclaim", Label(areaOwnership), func() {
	It("owns its copies under reclaimPolicy Delete, the default", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			expectOwnedBy(g, copied, copier)
		})
	})

	It("leaves its copies unowned under reclaimPolicy Retain", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS), reclaim(retain)))

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(copied.OwnerReferences).To(BeEmpty())
		})
	})

	It("has Kubernetes delete its copies with it under reclaimPolicy Delete", Label(liveOnly), func() {
		requireGarbageCollector()
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.deleteObject(copier)

		eventually(func(g Gomega) {
			expectNoSecret(g, targetNS.Name, src.Name)
		})
	})

	It("keeps its copies when it is deleted under reclaimPolicy Retain", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS), reclaim(retain)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.deleteObject(copier)

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("takes back its retained copies when it is recreated under the same name", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		rule := copierRule(src, inNamespaces(targetNS), reclaim(retain))
		copier := s.secretCopier(s.name("copier"), rule)
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		s.deleteObject(copier)
		eventually(func(g Gomega) {
			g.Expect(isGone(copier)).To(BeTrue())
		})

		s.update(src, func() { src.Data = data("token", "t1") })
		recreated := s.secretCopier(copier.GetName(), rule)

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(recreated))
		})
	})

	It("lets another copier adopt a copy retained by a deleted copier", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		retained := s.secretCopier(s.name("first"), copierRule(src, inNamespaces(targetNS), reclaim(retain)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(retained))
		})
		s.deleteObject(retained)
		eventually(func(g Gomega) {
			g.Expect(isGone(retained)).To(BeTrue())
		})

		s.update(src, func() { src.Data = data("token", "t1") })
		adopter := s.secretCopier(s.name("second"), copierRule(src, inNamespaces(targetNS)))

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(adopter))
			expectOwnedBy(g, copied, adopter)
		})
	})

	It("leaves a Secret it did not create untouched, even under a copy's name", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		unrelated := s.secret(targetNS.Name, src.Name, data("password", "unrelated"))

		s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))

		consistently(func(g Gomega) {
			latest := secretIn(g, targetNS.Name, unrelated.Name)
			g.Expect(latest.Data).To(Equal(unrelated.Data))
			g.Expect(latest.Annotations).NotTo(HaveKey(annotationCopierRule))
			g.Expect(latest.OwnerReferences).To(BeEmpty())
		})
	})

	It("leaves a copy another copier of a different source manages", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		firstSource := s.secret(sourceNS.Name, "first", data("token", "f0"))
		secondSource := s.secret(sourceNS.Name, "second", data("token", "s0"))
		first := s.secretCopier(s.name("first"), copierRule(firstSource, inNamespaces(targetNS),
			shaped(copyShape{Name: copyName})))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, copyName, firstSource, ruleOf(first))
		})

		s.secretCopier(s.name("second"), copierRule(secondSource, inNamespaces(targetNS),
			shaped(copyShape{Name: copyName})))

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, copyName, firstSource, ruleOf(first))
		})
	})

	It("lets a second copier of the same source agree with the first, and take over once it is deleted", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		first := s.secretCopier(s.name("first"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Labels: map[string]string{"chart": "first"}})))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(first))
		})

		second := s.secretCopier(s.name("second"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Labels: map[string]string{"chart": "second"}})))

		consistently(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(first))
			g.Expect(copied.Labels).To(HaveKeyWithValue("chart", "first"))
		})

		s.deleteObject(first)

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(second))
			g.Expect(copied.Labels).To(HaveKeyWithValue("chart", "second"))
		})
	})

	It("removes the owner reference from its copies when reclaimPolicy changes to Retain",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
			eventually(func(g Gomega) {
				expectOwnedBy(g, expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier)), copier)
			})

			s.update(copier, func() {
				setRules(copier, copierRule(src, inNamespaces(targetNS), reclaim(retain)))
			})

			eventually(func(g Gomega) {
				g.Expect(secretIn(g, targetNS.Name, src.Name).OwnerReferences).To(BeEmpty())
			})
		})

	It("adds the owner reference to its copies when reclaimPolicy changes to Delete",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS), reclaim(retain)))
			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			})

			s.update(copier, func() {
				setRules(copier, copierRule(src, inNamespaces(targetNS), reclaim(deleteWithOwner)))
			})

			eventually(func(g Gomega) {
				expectOwnedBy(g, secretIn(g, targetNS.Name, src.Name), copier)
			})
		})

	It("does not copy a source whose namespace is terminating into a new namespace",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			s.holdSource(src)
			s.terminate(sourceNS)
			targetNS := s.namespace("a", nil)

			s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))

			consistently(func(g Gomega) {
				expectNoSecret(g, targetNS.Name, src.Name)
			})
		})

	It("does not update copies from a source whose namespace is terminating", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		s.holdSource(src)
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		before := src.DeepCopy()

		s.terminate(sourceNS)
		s.update(src, func() { src.Data = data("token", "t1") })

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, before, ruleOf(copier))
		})
	})

	It("adds no finalizer to any of the four kinds", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		pull := s.pullSecret(targetNS.Name, "registry-credentials")
		s.serviceAccount(targetNS.Name, "builder", nil)
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Name: copyName})))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS)))
		importer := s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))
		injector := s.secretInjector(s.name("injector"), injectorRule(inNamespaces(targetNS), byName(pull.Name)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, copyName, src, ruleOf(copier))
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(exporter))
			g.Expect(pullSecretNames(serviceAccountIn(g, targetNS.Name, "builder"))).To(ContainElement(pull.Name))
		})

		consistently(func(g Gomega) {
			for _, u := range []*unstructured.Unstructured{copier, exporter, importer, injector} {
				g.Expect(get(g, u).GetFinalizers()).To(BeEmpty(), "finalizers of %s", ruleOf(u))
			}
		})
	})
})
