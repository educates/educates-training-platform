package scenarios

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// unrelatedLabel is a label scenarios put on a namespace that no rule
// selects by.
const unrelatedLabel = "example.com/unrelated"

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
