package scenarios

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// passphrase is the shared secret of scenarios that name one.
const passphrase = "open-sesame"

var _ = Describe("SecretExporter and SecretImporter", Label(areaExportImport), func() {
	It("copies an exported Secret where an importer holds the exporter's UID, owned by the importer", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		accepting := s.namespace("a", nil)
		without := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(accepting, without)))

		importer := s.secretImporter(accepting.Name, src.Name, sharedSecret(uidOf(exporter)))

		eventually(func(g Gomega) {
			copied := expectCopy(g, accepting.Name, src.Name, src, ruleOf(exporter))
			expectOwnedBy(g, copied, importer)
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, without.Name, src.Name)
		})
	})

	It("copies only where the importer holds the shared secret the exporter's rule names", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		matching := s.namespace("a", nil)
		mismatched := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name,
			exporterRule(inNamespaces(matching, mismatched), sharedSecret(passphrase)))

		s.secretImporter(matching.Name, src.Name, sharedSecret(passphrase))
		s.secretImporter(mismatched.Name, src.Name, sharedSecret("wrong"))

		eventually(func(g Gomega) {
			expectCopy(g, matching.Name, src.Name, src, ruleOf(exporter))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, mismatched.Name, src.Name)
		})
	})

	It("copies once an importer is created after the exporter", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS)))
		consistentlyFor(5*target.Timing.Poll, func(g Gomega) {
			expectNoSecret(g, targetNS.Name, src.Name)
		})

		s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(exporter))
		})
	})

	It("names the exported copy targetSecret.name, accepted by an importer of that name", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS),
			shaped(copyShape{Name: copyName, Labels: map[string]string{teamLabel: teamValue}})))

		s.secretImporter(targetNS.Name, copyName, sharedSecret(uidOf(exporter)))

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, copyName, src, ruleOf(exporter))
			g.Expect(copied.Labels).To(HaveKeyWithValue(teamLabel, teamValue))
		})
	})

	It("copies only where the importer's sourceNamespaces admit the exporter's namespace", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		admitting := s.namespace("a", nil)
		refusing := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(admitting, refusing)))

		s.secretImporter(admitting.Name, src.Name, sharedSecret(uidOf(exporter)), sourceNamespaces(s.id+"-*"))
		s.secretImporter(refusing.Name, src.Name, sharedSecret(uidOf(exporter)),
			sourceNamespaces("*", "!"+sourceNS.Name))

		eventually(func(g Gomega) {
			expectCopy(g, admitting.Name, src.Name, src, ruleOf(exporter))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, refusing.Name, src.Name)
		})
	})

	It("has Kubernetes delete an exported copy with its importer", Label(liveOnly), func() {
		requireGarbageCollector()
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS)))
		importer := s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(exporter))
		})

		s.deleteObject(importer)

		eventually(func(g Gomega) {
			expectNoSecret(g, targetNS.Name, src.Name)
		})
	})

	It("leaves the copy in place, no longer updated, when the importer's shared secret changes", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS)))
		importer := s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(exporter))
		})
		before := src.DeepCopy()

		s.update(importer, func() {
			Expect(unstructured.SetNestedField(importer.Object, "changed", "spec", "copyAuthorization", "sharedSecret")).
				To(Succeed())
		})
		s.update(src, func() { src.Data = data("token", "t1") })

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, before, ruleOf(exporter))
		})
	})

	It("leaves the copies in place, no longer updated, when the exporter is deleted", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS)))
		s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(exporter))
		})
		before := src.DeepCopy()

		s.deleteObject(exporter)
		eventually(func(g Gomega) {
			g.Expect(isGone(exporter)).To(BeTrue())
		})
		s.update(src, func() { src.Data = data("token", "t1") })

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, before, ruleOf(exporter))
		})
	})

	It("accepts an exporter rule that selects namespaces only by owner", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		owner := s.namespace("owner", nil)
		owned := s.ownedNamespace("a", owner)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))

		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(byOwners(namespaceOwner(owner))))
		s.secretImporter(owned.Name, src.Name, sharedSecret(uidOf(exporter)))

		eventually(func(g Gomega) {
			expectCopy(g, owned.Name, src.Name, src, ruleOf(exporter))
		})
	})
})

var _ = Describe("SecretImporter gating SecretCopier rules", Label(areaExportImport), func() {
	It("copies a copyAuthorization rule only where an importer holds its shared secret", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		matching := s.namespace("a", nil)
		mismatched := s.namespace("b", nil)
		without := s.namespace("c", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		s.secretImporter(matching.Name, src.Name, sharedSecret(passphrase))
		s.secretImporter(mismatched.Name, src.Name, sharedSecret("wrong"))

		copier := s.secretCopier(s.name("copier"),
			copierRule(src, inNamespaces(matching, mismatched, without), sharedSecret(passphrase)))

		eventually(func(g Gomega) {
			copied := expectCopy(g, matching.Name, src.Name, src, ruleOf(copier))
			expectOwnedBy(g, copied, copier)
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, mismatched.Name, src.Name)
			expectNoSecret(g, without.Name, src.Name)
		})
	})

	It("lets an importer refuse a copier's copy from a namespace its sourceNamespaces exclude", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		refusing := s.namespace("a", nil)
		open := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		s.secretImporter(refusing.Name, src.Name, sourceNamespaces("!"+sourceNS.Name))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(refusing, open)))

		eventually(func(g Gomega) {
			expectCopy(g, open.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, refusing.Name, src.Name)
		})
	})

	It("leaves the copy in place, no longer updated, when the importer gating a copier rule is deleted", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		importer := s.secretImporter(targetNS.Name, src.Name, sharedSecret(passphrase))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS), sharedSecret(passphrase)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		before := src.DeepCopy()

		s.deleteObject(importer)
		s.update(src, func() { src.Data = data("token", "t1") })

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, before, ruleOf(copier))
		})
	})
})
