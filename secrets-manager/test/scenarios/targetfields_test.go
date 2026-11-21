package scenarios

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
)

// usernameKey is a key of a scenario's source that targetSecret.keys
// selects.
const usernameKey = "username"

var _ = Describe("targetSecret.annotations and targetSecret.keys", Label(areaTargetFields), func() {
	It("copies every key when the rule lists no keys", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data(usernameKey, "user", "password", "secret", "ca.crt", "ca"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(copied.Data).To(HaveLen(3))
		})
	})

	It("sets targetSecret.annotations on every copy", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		first := s.namespace("a", nil)
		second := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(first, second),
			shaped(copyShape{Annotations: map[string]string{"argocd.argoproj.io/compare-options": "IgnoreExtraneous"}})))

		eventually(func(g Gomega) {
			for _, ns := range []*corev1.Namespace{first, second} {
				copied := expectCopy(g, ns.Name, src.Name, src, ruleOf(copier))
				g.Expect(copied.Annotations).To(HaveKeyWithValue("argocd.argoproj.io/compare-options", "IgnoreExtraneous"))
			}
		})
	})

	It("removes an annotation dropped from targetSecret.annotations from the copies",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
				shaped(copyShape{Annotations: map[string]string{"example.com/keep": "kept", "example.com/drop": "dropped"}})))
			eventually(func(g Gomega) {
				copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
				g.Expect(copied.Annotations).To(HaveKey("example.com/drop"))
			})

			s.update(copier, func() {
				setRules(copier, copierRule(src, inNamespaces(targetNS),
					shaped(copyShape{Annotations: map[string]string{"example.com/keep": "kept"}})))
			})

			eventually(func(g Gomega) {
				copied := secretIn(g, targetNS.Name, src.Name)
				g.Expect(copied.Annotations).To(HaveKeyWithValue("example.com/keep", "kept"))
				g.Expect(copied.Annotations).NotTo(HaveKey("example.com/drop"))
			})
		})

	It("leaves an annotation another writer adds when targetSecret.annotations change",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
				shaped(copyShape{Annotations: map[string]string{"example.com/drop": "dropped"}})))
			var copied *corev1.Secret
			eventually(func(g Gomega) {
				copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
				g.Expect(copied.Annotations).To(HaveKey("example.com/drop"))
			})

			s.update(copied, func() { setMapEntry(&copied.Annotations, "example.com/note", "added by hand") })
			s.update(copier, func() { setRules(copier, copierRule(src, inNamespaces(targetNS))) })

			eventually(func(g Gomega) {
				g.Expect(secretIn(g, targetNS.Name, src.Name).Annotations).NotTo(HaveKey("example.com/drop"))
			})
			consistently(func(g Gomega) {
				g.Expect(secretIn(g, targetNS.Name, src.Name).Annotations).
					To(HaveKeyWithValue("example.com/note", "added by hand"))
			})
		})

	It("copies only the keys targetSecret.keys lists that the source has", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data(usernameKey, "user", "password", "secret", "ca.crt", "ca"))

		s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Keys: []string{"ca.crt", usernameKey, "missing"}})))

		eventually(func(g Gomega) {
			g.Expect(secretIn(g, targetNS.Name, src.Name).Data).To(Equal(data("ca.crt", "ca", usernameKey, "user")))
		})
	})

	It("removes keys outside targetSecret.keys from existing copies", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data(usernameKey, "user", "password", "secret"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.update(copier, func() {
			setRules(copier, copierRule(src, inNamespaces(targetNS), shaped(copyShape{Keys: []string{usernameKey}})))
		})

		eventually(func(g Gomega) {
			g.Expect(secretIn(g, targetNS.Name, src.Name).Data).To(Equal(data(usernameKey, "user")))
		})
	})

	It("applies targetSecret.annotations and targetSecret.keys on SecretExporter rules",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "exported", data(usernameKey, "user", "password", "secret"))
			exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS),
				shaped(copyShape{Annotations: map[string]string{"example.com/note": "exported"}, Keys: []string{usernameKey}})))

			s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))

			eventually(func(g Gomega) {
				copied := secretIn(g, targetNS.Name, src.Name)
				g.Expect(copied.Data).To(Equal(data(usernameKey, "user")))
				g.Expect(copied.Annotations).To(HaveKeyWithValue("example.com/note", "exported"))
			})
		})

	It("fails only the target whose copy the API server rejects", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		rejected := s.namespace("a", nil)
		healthy := s.namespace("b", nil)
		tls := s.secret(sourceNS.Name, "ingress-tls", data("tls.crt", "cert", "tls.key", "key", "ca.crt", "ca"),
			withType(corev1.SecretTypeTLS))
		other := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"),
			copierRule(tls, inNamespaces(rejected), shaped(copyShape{Keys: []string{"ca.crt"}})),
			copierRule(other, inNamespaces(rejected, healthy)))

		eventually(func(g Gomega) {
			expectCopy(g, rejected.Name, other.Name, other, ruleOf(copier))
			expectCopy(g, healthy.Name, other.Name, other, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, rejected.Name, tls.Name)
		})
	})
})

var _ = Describe("Admission", Label(areaAdmission), func() {
	It("accepts a SecretCopier using every field Educates 3.x defines", func() {
		s := newScenario()
		owner := s.namespace("owner", nil)
		src := newSecret(s.name("src"), "credentials", nil)

		s.create(resource(kindSecretCopier, "", s.name("copier"), rulesSpec(copierRule(src,
			merged(
				byName(s.name("*"), "!kube-*"),
				byUIDs(owner.UID),
				byOwners(namespaceOwner(owner)),
				byLabelSelector(s.scope(), expression(tierLabel, "In", tierGold)),
			),
			shaped(copyShape{Name: copyName, Labels: map[string]string{teamLabel: teamValue}}),
			sharedSecret(passphrase),
			reclaim(retain),
		))))
	})

	It("rejects a SecretInjector rule that selects no source Secrets", func() {
		s := newScenario()
		s.createRejected(resource(kindSecretInjector, "", s.name("injector"),
			rulesSpec(injectorRule(byName(s.name("a")), object{}))))
	})

	DescribeTable("rejects at admission", Label(expectedDifference),
		func(kind string, rules func(s *scenario) []object) {
			s := newScenario()
			namespace := ""
			if kind == kindSecretExporter {
				namespace = s.namespace("src", nil).Name
			}
			s.createRejected(resource(kind, namespace, s.name("rejected"), rulesSpec(rules(s)...)))
		},
		Entry("a SecretCopier annotation under secrets.educates.dev/", kindSecretCopier,
			func(s *scenario) []object {
				return []object{unmatchedCopierRule(s,
					shaped(copyShape{Annotations: map[string]string{annotationCopierRule: "secretcopier/forged"}}))}
			}),
		Entry("a SecretExporter annotation under secrets.educates.dev/", kindSecretExporter,
			func(s *scenario) []object {
				return []object{exporterRule(byName(s.name("none")),
					shaped(copyShape{Annotations: map[string]string{"secrets.educates.dev/source-version": "1"}}))}
			}),
		Entry("a SecretCopier rule with 33 annotations", kindSecretCopier, func(s *scenario) []object {
			annotations := map[string]string{}
			for i := range 33 {
				annotations[fmt.Sprintf("example.com/a%d", i)] = "x"
			}
			return []object{unmatchedCopierRule(s, shaped(copyShape{Annotations: annotations}))}
		}),
		Entry("a SecretCopier with 101 rules", kindSecretCopier, func(s *scenario) []object {
			rules := make([]object, 0, 101)
			for range 101 {
				rules = append(rules, unmatchedCopierRule(s))
			}
			return rules
		}),
		Entry("a SecretExporter with 101 rules", kindSecretExporter, func(s *scenario) []object {
			rules := make([]object, 0, 101)
			for range 101 {
				rules = append(rules, exporterRule(byName(s.name("none"))))
			}
			return rules
		}),
	)
})

// unmatchedCopierRule is a SecretCopier rule copying a Secret of the
// scenario into a namespace that does not exist, with the other fields of
// extra.
func unmatchedCopierRule(s *scenario, extra ...object) object {
	return copierRule(newSecret(s.name("src"), "credentials", nil), byName(s.name("none")), extra...)
}
