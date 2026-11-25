package scenarios

import (
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A label scenarios put on namespaces, with its values.
const (
	tierLabel  = "tier"
	tierGold   = "gold"
	tierSilver = "silver"
	tierNone   = "none"
)

var _ = Describe("Namespace selection", Label(areaSelection), func() {
	It("selects namespaces by name globs, excluding names under !", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		first := s.namespace("in-a", nil)
		second := s.namespace("in-b", nil)
		skipped := s.namespace("in-skip", nil)
		outside := s.namespace("out", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, byName(s.name("in-*"), "!"+skipped.Name)))

		eventually(func(g Gomega) {
			expectCopy(g, first.Name, src.Name, src, ruleOf(copier))
			expectCopy(g, second.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, skipped.Name, src.Name)
			expectNoSecret(g, outside.Name, src.Name)
		})
	})

	It("excludes kube-* namespaces when the rule has no nameSelector", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		selected := s.namespace("a", s.scope())
		system := s.namespaceNamed("kube-"+s.id, s.scope())
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, byLabels(s.scope())))

		eventually(func(g Gomega) {
			expectCopy(g, selected.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, system.Name, src.Name)
		})
	})

	It("keeps excluding kube-* namespaces when matchNames is empty", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		selected := s.namespace("a", s.scope())
		system := s.namespaceNamed("kube-"+s.id, s.scope())
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, merged(byName(), byLabels(s.scope()))))

		eventually(func(g Gomega) {
			expectCopy(g, selected.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, system.Name, src.Name)
		})
	})

	It("selects namespaces by matchLabels", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		labelled := s.namespace("a", map[string]string{scenarioLabel: s.id, tierLabel: tierGold})
		other := s.namespace("b", map[string]string{scenarioLabel: s.id, tierLabel: tierSilver})
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src,
			byLabels(map[string]string{scenarioLabel: s.id, tierLabel: tierGold})))

		eventually(func(g Gomega) {
			expectCopy(g, labelled.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, other.Name, src.Name)
		})
	})

	DescribeTable("selects namespaces by matchExpressions",
		func(selector object, selectedTiers ...string) {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			namespaces := map[string]*corev1.Namespace{
				tierGold:   s.namespace(tierGold, map[string]string{tierLabel: tierGold}),
				tierSilver: s.namespace(tierSilver, map[string]string{tierLabel: tierSilver}),
				tierNone:   s.namespace(tierNone, nil),
			}
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

			copier := s.secretCopier(s.name("copier"), copierRule(src, merged(
				byName(s.name(tierGold), s.name(tierSilver), s.name(tierNone)), selector)))

			eventually(func(g Gomega) {
				for _, tier := range selectedTiers {
					expectCopy(g, namespaces[tier].Name, src.Name, src, ruleOf(copier))
				}
			})
			consistently(func(g Gomega) {
				for tier, ns := range namespaces {
					if !slices.Contains(selectedTiers, tier) {
						expectNoSecret(g, ns.Name, src.Name)
					}
				}
			})
		},
		Entry("with In", byExpressions(expression(tierLabel, "In", tierGold)), tierGold),
		Entry("with NotIn", byExpressions(expression(tierLabel, "NotIn", tierGold)), tierSilver, tierNone),
		Entry("with Exists", byExpressions(expression(tierLabel, "Exists")), tierGold, tierSilver),
		Entry("with DoesNotExist", byExpressions(expression(tierLabel, "DoesNotExist")), tierNone),
	)

	DescribeTable("selects no namespace, and reports InvalidSelector, with a labelSelector Kubernetes rejects",
		Label(expectedDifference),
		func(invalid object) {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			first := s.namespace("a", map[string]string{tierLabel: tierGold})
			second := s.namespace("b", nil)
			valid := s.namespace("c", nil)
			src := s.secret(sourceNS.Name, s.name("credentials"), data("token", "t0"))

			copier := s.secretCopier(s.name("copier"),
				copierRule(src, merged(inNamespaces(first, second), invalid)),
				copierRule(src, inNamespaces(valid)))

			eventually(func(g Gomega) {
				expectCopy(g, valid.Name, src.Name, src, ruleOf(copier))
				expectReady(g, copier, "False", "InvalidSelector")
			})
			consistently(func(g Gomega) {
				expectNoSecret(g, first.Name, src.Name)
				expectNoSecret(g, second.Name, src.Name)
			})
		},
		Entry("In without values", byExpressions(expression(tierLabel, "In"))),
		Entry("NotIn without values", byExpressions(expression(tierLabel, "NotIn"))),
		Entry("Exists with values", byExpressions(expression(tierLabel, "Exists", tierGold))),
		Entry("DoesNotExist with values", byExpressions(expression(tierLabel, "DoesNotExist", tierGold))),
		Entry("a matchLabels key that is no label key", byLabels(map[string]string{"not a label key": tierGold})),
		Entry("a matchLabels value that is no label value", byLabels(map[string]string{tierLabel: "not a value"})),
	)

	It("copies into a namespace given an owner the rule selects after its creation", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		owner := s.namespace("owner", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, s.name("credentials"), data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, byOwners(namespaceOwner(owner))))
		consistentlyFor(5*target.Timing.Poll, func(g Gomega) {
			expectNoSecret(g, targetNS.Name, src.Name)
		})

		s.update(targetNS, func() {
			targetNS.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "v1", Kind: "Namespace", Name: owner.Name, UID: owner.UID,
			}}
		})

		promptly(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("selects namespaces by UID", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		selected := s.namespace("a", nil)
		other := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src,
			merged(inNamespaces(selected, other), byUIDs(selected.UID))))

		eventually(func(g Gomega) {
			expectCopy(g, selected.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, other.Name, src.Name)
		})
	})

	It("selects namespaces by owner", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		owner := s.namespace("owner", nil)
		owned := s.ownedNamespace("a", owner)
		other := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src,
			merged(inNamespaces(owned, other), byOwners(namespaceOwner(owner)))))

		eventually(func(g Gomega) {
			expectCopy(g, owned.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, other.Name, src.Name)
		})
	})

	It("does not update a copy in a terminating namespace", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		var copied *corev1.Secret
		eventually(func(g Gomega) {
			copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		before := src.DeepCopy()
		s.holdSource(copied)
		s.terminate(targetNS)

		s.update(src, func() { src.Data = data("token", "t1") })

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, before, ruleOf(copier))
		})
	})

	It("copies into a namespace relabelled into the rule's scope", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		later := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, byLabels(s.scope())))
		consistentlyFor(5*target.Timing.Poll, func(g Gomega) {
			expectNoSecret(g, later.Name, src.Name)
		})

		s.update(later, func() { setMapEntry(&later.Labels, scenarioLabel, s.id) })

		eventually(func(g Gomega) {
			expectCopy(g, later.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("keeps the copy in a namespace relabelled out of the rule's scope", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		leaving := s.namespace("a", s.scope())
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, byLabels(s.scope())))
		eventually(func(g Gomega) {
			expectCopy(g, leaving.Name, src.Name, src, ruleOf(copier))
		})

		s.update(leaving, func() { delete(leaving.Labels, scenarioLabel) })

		consistently(func(g Gomega) {
			expectCopy(g, leaving.Name, src.Name, src, ruleOf(copier))
		})
	})
})
