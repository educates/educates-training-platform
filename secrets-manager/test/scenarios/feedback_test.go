package scenarios

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("Status, printer columns and Events", Label(areaFeedback, expectedDifference), func() {
	It("reports a SecretCopier Ready and in sync once every copy is made", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		first := s.namespace("a", nil)
		second := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(first, second)))

		eventually(func(g Gomega) {
			expectReady(g, copier, "True", "InSync")
			summary := summaryOf(g, copier)
			expectCounts(g, summary, map[string]int64{
				countTargetNamespaces: 2, countInSync: 2, countConflicts: 0, countAwaitingAuthorization: 0, countFailures: 0,
			})
			g.Expect(summary).To(HaveKeyWithValue("inSyncRatio", inSyncRatio(2, 2)))
			rule := statusRule(g, copier, 0)
			g.Expect(rule).To(HaveKeyWithValue("sourceExists", true))
			expectCounts(g, rule, map[string]int64{countTargetNamespaces: 2, countInSync: 2})
		})
	})

	It("reports SourceMissing while a rule's source does not exist, and InSync once it does", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		present := s.secret(sourceNS.Name, "present", data("token", "p0"))
		pending := newSecret(sourceNS.Name, "pending", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"),
			copierRule(present, inNamespaces(targetNS)), copierRule(pending, inNamespaces(targetNS)))

		eventually(func(g Gomega) {
			expectReady(g, copier, "False", "SourceMissing")
			g.Expect(statusRule(g, copier, 0)).To(HaveKeyWithValue("sourceExists", true))
			g.Expect(statusRule(g, copier, 1)).To(HaveKeyWithValue("sourceExists", false))
		})

		s.secret(sourceNS.Name, pending.Name, pending.Data)

		eventually(func(g Gomega) {
			expectReady(g, copier, "True", "InSync")
			g.Expect(statusRule(g, copier, 1)).To(HaveKeyWithValue("sourceExists", true))
		})
	})

	It("reports Conflict for Secrets it did not create, naming five of their namespaces", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		parts := []string{"a", "b", "c", "d", "e", "f", "g"}
		conflicting := make([]*corev1.Namespace, 0, len(parts))
		for _, part := range parts {
			ns := s.namespace(part, nil)
			s.secret(ns.Name, src.Name, data("password", "unrelated"))
			conflicting = append(conflicting, ns)
		}

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(conflicting...)))

		eventually(func(g Gomega) {
			ready := expectReady(g, copier, "False", "Conflict")
			expectCounts(g, summaryOf(g, copier), map[string]int64{
				countTargetNamespaces: 7, countConflicts: 7, countInSync: 0,
			})
			message, _ := ready["message"].(string)
			named := 0
			for _, ns := range conflicting {
				if strings.Contains(message, ns.Name) {
					named++
				}
			}
			g.Expect(named).To(Equal(5), "namespaces named in %q", message)
		})
	})

	It("reports AwaitingAuthorization while a copyAuthorization rule has no importer", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS), sharedSecret(passphrase)))

		eventually(func(g Gomega) {
			expectReady(g, copier, "False", "AwaitingAuthorization")
			expectCounts(g, summaryOf(g, copier), map[string]int64{countAwaitingAuthorization: 1, countInSync: 0})
		})
	})

	It("reports InvalidSelector before any other problem", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		missing := newSecret(sourceNS.Name, "missing", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"),
			copierRule(missing, inNamespaces(targetNS)),
			copierRule(src, merged(inNamespaces(targetNS), byExpressions(expression(tierLabel, "In")))))

		eventually(func(g Gomega) {
			expectReady(g, copier, "False", "InvalidSelector")
		})
	})

	It("reports a second copier of the same source in sync while the first keeps the copy", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		first := s.secretCopier(s.name("first"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(first))
		})

		second := s.secretCopier(s.name("second"), copierRule(src, inNamespaces(targetNS)))

		eventually(func(g Gomega) {
			expectReady(g, second, "True", "InSync")
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(first))
		})
	})

	It("reports a SecretExporter's source and how many targets are in sync or awaiting authorization", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		accepting := s.namespace("a", nil)
		waiting := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(accepting, waiting)))

		s.secretImporter(accepting.Name, src.Name, sharedSecret(uidOf(exporter)))

		eventually(func(g Gomega) {
			expectReady(g, exporter, "False", "AwaitingAuthorization")
			g.Expect(statusValue(g, exporter, "sourceExists")).To(BeTrue())
			summary := summaryOf(g, exporter)
			expectCounts(g, summary, map[string]int64{
				countTargetNamespaces: 2, countInSync: 1, countAwaitingAuthorization: 1,
			})
			g.Expect(summary).To(HaveKeyWithValue("inSyncRatio", inSyncRatio(1, 2)))
			expectCounts(g, statusRule(g, exporter, 0), map[string]int64{countInSync: 1, countAwaitingAuthorization: 1})
		})
	})

	It("reports a SecretImporter Imported, bound to its exporter", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name,
			exporterRule(inNamespaces(targetNS), shaped(copyShape{Name: copyName})))

		importer := s.secretImporter(targetNS.Name, copyName, sharedSecret(uidOf(exporter)))

		eventually(func(g Gomega) {
			expectReady(g, importer, "True", "Imported")
			g.Expect(statusValue(g, importer, "targetSecretName")).To(Equal(copyName))
			g.Expect(jsonText(g, statusValue(g, importer, "boundTo"))).To(ContainSubstring(exporter.GetName()))
		})
	})

	It("reports a SecretImporter AwaitingAuthorization when its shared secret does not match", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS)))

		importer := s.secretImporter(targetNS.Name, src.Name, sharedSecret("wrong"))

		eventually(func(g Gomega) {
			expectReady(g, importer, "False", "AwaitingAuthorization")
		})
	})

	It("reports a SecretImporter NoMatchingExporter when nothing offers its Secret", func() {
		s := newScenario()
		targetNS := s.namespace("a", nil)

		importer := s.secretImporter(targetNS.Name, "nothing-offers-this", sharedSecret(passphrase))

		eventually(func(g Gomega) {
			expectReady(g, importer, "False", "NoMatchingExporter")
		})
	})

	It("reports a SecretInjector's matched namespaces, service accounts and injections", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		pull := s.pullSecret(ns.Name, pullSecretName)
		s.serviceAccount(ns.Name, builder, nil)
		s.serviceAccount(ns.Name, runner, nil)

		injector := s.secretInjector(s.name("injector"),
			injectorRule(inNamespaces(ns), byName(pull.Name), accounts(builder, runner)))

		eventually(func(g Gomega) {
			expectReady(g, injector, "True", "InSync")
			expectCounts(g, statusRule(g, injector, 0), map[string]int64{
				countTargetNamespaces: 1, "serviceAccountsMatched": 2, "injectionsInSync": 2, countFailures: 0,
			})
			g.Expect(summaryOf(g, injector)).To(HaveKeyWithValue("inSyncRatio", inSyncRatio(2, 2)))
		})
	})

	It("shows Ready, Reason, In Sync and Age columns in kubectl get", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		pull := s.pullSecret(targetNS.Name, pullSecretName)
		s.serviceAccount(targetNS.Name, builder, nil)
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Name: copyName})))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS)))
		importer := s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))
		injector := s.secretInjector(s.name("injector"),
			injectorRule(inNamespaces(targetNS), byName(pull.Name), accounts(builder)))

		eventually(func(g Gomega) {
			for _, entry := range []struct {
				resource *unstructured.Unstructured
				plural   string
				inSync   string
			}{
				{copier, "secretcopiers", inSyncRatio(1, 1)},
				{exporter, "secretexporters", inSyncRatio(1, 1)},
				{injector, "secretinjectors", inSyncRatio(1, 1)},
				{importer, "secretimporters", ""},
			} {
				columns := tableColumns(g, entry.resource, entry.plural)
				g.Expect(columns).To(HaveKeyWithValue("Ready", "True"), "columns of %s", ruleOf(entry.resource))
				g.Expect(columns).To(HaveKey("Reason"), "columns of %s", ruleOf(entry.resource))
				g.Expect(columns).To(HaveKey("Age"), "columns of %s", ruleOf(entry.resource))
				if entry.inSync != "" {
					g.Expect(columns).To(HaveKeyWithValue("In Sync", entry.inSync), "columns of %s", ruleOf(entry.resource))
				}
			}
		})
	})

	It("raises a Warning NotReady Event after the grace period, and a Normal Recovered Event on recovery", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		pending := newSecret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(pending, inNamespaces(targetNS)))

		Eventually(func(g Gomega) {
			events := eventsAbout(g, eventsNamespace, copier, "NotReady")
			g.Expect(events).NotTo(BeEmpty(), "NotReady Events of %s", ruleOf(copier))
			g.Expect(events[0].Type).To(Equal(corev1.EventTypeWarning))
		}).WithTimeout(target.Timing.NotReadyGrace + target.Timing.Converge).WithPolling(target.Timing.Poll).
			Should(Succeed())

		s.secret(sourceNS.Name, pending.Name, pending.Data)

		eventually(func(g Gomega) {
			events := eventsAbout(g, eventsNamespace, copier, "Recovered")
			g.Expect(events).NotTo(BeEmpty(), "Recovered Events of %s", ruleOf(copier))
			g.Expect(events[0].Type).To(Equal(corev1.EventTypeNormal))
		})
	})

	It("raises no Warning for a copier whose source appears within the grace period", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		pending := newSecret(sourceNS.Name, "kubeconfig", data("config", "c0"))
		created := time.Now()
		copier := s.secretCopier(s.name("copier"), copierRule(pending, inNamespaces(targetNS)))
		time.Sleep(target.Timing.NotReadyGrace / 3)

		s.secret(sourceNS.Name, pending.Name, pending.Data)

		eventually(func(g Gomega) {
			expectReady(g, copier, "True", "InSync")
		})
		consistentlyFor(time.Until(created.Add(target.Timing.NotReadyGrace+target.Timing.NotReadyGrace/4)),
			func(g Gomega) {
				g.Expect(eventsAbout(g, eventsNamespace, copier, "NotReady")).To(BeEmpty())
			})
	})

	It("does not reconcile a resource again for a change of its status alone", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			g.Expect(summaryOf(g, copier)).To(HaveKeyWithValue("inSyncRatio", inSyncRatio(1, 1)))
		})

		// Another writer changes the status, and nothing else: a reconcile
		// would write the status back.
		s.patchStatus(copier, func(status map[string]any) {
			Expect(unstructured.SetNestedField(status, "changed", "summary", "inSyncRatio")).To(Succeed())
		})

		consistently(func(g Gomega) {
			g.Expect(summaryOf(g, copier)).To(HaveKeyWithValue("inSyncRatio", "changed"))
		})

		// A change of the spec reconciles the copier, which writes its status
		// anew.
		s.update(copier, func() {
			setRules(copier, copierRule(src, inNamespaces(targetNS), shaped(copyShape{Labels: map[string]string{
				tierLabel: "changed",
			}})))
		})
		eventually(func(g Gomega) {
			g.Expect(summaryOf(g, copier)).To(HaveKeyWithValue("inSyncRatio", inSyncRatio(1, 1)))
		})
	})

	It("raises a Normal Imported Event when an importer imports", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "exported", data("token", "t0"))
		exporter := s.secretExporter(sourceNS.Name, src.Name, exporterRule(inNamespaces(targetNS)))

		importer := s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))

		eventually(func(g Gomega) {
			events := eventsAbout(g, targetNS.Name, importer, "Imported")
			g.Expect(events).NotTo(BeEmpty(), "Imported Events of %s", ruleOf(importer))
			g.Expect(events[0].Type).To(Equal(corev1.EventTypeNormal))
		})
	})
})
