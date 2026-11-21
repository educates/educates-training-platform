package scenarios

import (
	"context"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// configMap creates an empty ConfigMap in namespace ns, for a scenario that
// needs an object to own a Secret.
func (s *scenario) configMap(ns, name string) *corev1.ConfigMap {
	GinkgoHelper()
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	s.create(configMap)
	return configMap
}

// orphanedCopyOf marks a Secret as a copy of src that the SecretCopier
// named copier wrote, as a copy retained after that SecretCopier was
// deleted is marked.
func orphanedCopyOf(src *corev1.Secret, copier string) func(*corev1.Secret) {
	return func(secret *corev1.Secret) {
		setMapEntry(&secret.Annotations, annotationCopierRule, "secretcopier/"+copier)
		setMapEntry(&secret.Annotations, annotationSecretName, src.Namespace+"/"+src.Name)
	}
}

// ownedBy gives a Secret an owner reference to owner, marking owner its
// controller when controller is true.
func ownedBy(owner *corev1.ConfigMap, controller bool) func(*corev1.Secret) {
	return func(secret *corev1.Secret) {
		secret.OwnerReferences = append(secret.OwnerReferences, metav1.OwnerReference{
			APIVersion: "v1", Kind: "ConfigMap", Name: owner.Name, UID: owner.UID, Controller: &controller,
		})
	}
}

// ownerUIDs lists the UIDs a Secret's owner references name.
func ownerUIDs(secret *corev1.Secret) []types.UID {
	result := make([]types.UID, 0, len(secret.OwnerReferences))
	for _, ref := range secret.OwnerReferences {
		result = append(result, ref.UID)
	}
	return result
}

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

	It("keeps an owner reference another writer set on a copy it adopts", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		holder := s.configMap(targetNS.Name, "holder")
		s.secret(targetNS.Name, src.Name, data("token", "stale"),
			orphanedCopyOf(src, s.name("deleted")), ownedBy(holder, false))

		adopter := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(adopter))
			expectOwnedBy(g, copied, adopter)
			g.Expect(ownerUIDs(copied)).To(ContainElement(holder.UID))
		})
	})

	It("fails only the target whose orphaned copy another writer gave a controller", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		// The namespaces sort with the failing one first.
		failing := s.namespace("a", nil)
		healthy := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		holder := s.configMap(failing.Name, "holder")
		orphan := s.secret(failing.Name, src.Name, data("token", "stale"),
			orphanedCopyOf(src, s.name("deleted")), ownedBy(holder, true))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(failing, healthy)))

		eventually(func(g Gomega) {
			expectCopy(g, healthy.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			latest := secretIn(g, failing.Name, src.Name)
			g.Expect(latest.UID).To(Equal(orphan.UID))
			g.Expect(latest.Data).To(Equal(orphan.Data))
			g.Expect(ownerUIDs(latest)).To(ConsistOf(holder.UID))
		})
	})

	It("never deletes a copy another writer made immutable, when its data must change", func() {
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

		immutable := true
		s.update(copied, func() { copied.Immutable = &immutable })
		s.update(src, func() { src.Data = data("token", "t1") })

		consistently(func(g Gomega) {
			latest := expectCopy(g, targetNS.Name, src.Name, before, ruleOf(copier))
			g.Expect(latest.UID).To(Equal(copied.UID))
		})
	})

	It("reports a copy another writer made immutable as not updated, with one Warning for each source change",
		Label(expectedDifference, areaFeedback), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
			var copied *corev1.Secret
			eventually(func(g Gomega) {
				copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			})
			immutable := true
			s.update(copied, func() { copied.Immutable = &immutable })
			copyKey := targetNS.Name + "/" + src.Name
			sourceKey := sourceNS.Name + "/" + src.Name

			s.update(src, func() { src.Data = data("token", "t1") })

			eventually(func(g Gomega) {
				ready := expectReady(g, copier, "False", "TargetErrors")
				g.Expect(ready["message"]).To(And(ContainSubstring("immutable"), ContainSubstring("not updated")))
				events := eventsAbout(g, eventsNamespace, copier, "CopyNotUpdated")
				g.Expect(events).To(HaveLen(1), "CopyNotUpdated Events of %s", ruleOf(copier))
				g.Expect(events[0].Type).To(Equal(corev1.EventTypeWarning))
				g.Expect(events[0].Note).To(And(ContainSubstring(copyKey), ContainSubstring(sourceKey),
					ContainSubstring("data")))
			})
			// The copy keeps failing, and is retried with backoff.
			consistently(func(g Gomega) {
				events := eventsAbout(g, eventsNamespace, copier, "CopyNotUpdated")
				g.Expect(events).To(HaveLen(1), "CopyNotUpdated Events of %s", ruleOf(copier))
				g.Expect(events[0].Series).To(BeNil(), "CopyNotUpdated Event of %s recorded again", ruleOf(copier))
			})

			s.deleteObject(src)
			s.secret(sourceNS.Name, src.Name, src.Data, withType("example.com/token"))

			eventually(func(g Gomega) {
				events := eventsAbout(g, eventsNamespace, copier, "CopyNotUpdated")
				g.Expect(events).To(HaveLen(2), "CopyNotUpdated Events of %s", ruleOf(copier))
				g.Expect(events).To(ContainElement(HaveField("Note", And(ContainSubstring(copyKey),
					ContainSubstring(sourceKey), ContainSubstring("type")))))
			})
			consistently(func(g Gomega) {
				events := eventsAbout(g, eventsNamespace, copier, "CopyNotUpdated")
				g.Expect(events).To(HaveLen(2), "CopyNotUpdated Events of %s", ruleOf(copier))
				for _, event := range events {
					g.Expect(event.Series).To(BeNil(), "CopyNotUpdated Event of %s recorded again", ruleOf(copier))
				}
				g.Expect(secretIn(g, targetNS.Name, src.Name).UID).To(Equal(copied.UID))
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

	It("lets a second copier of the same source take over the copy of a copier that is being deleted",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, s.name("credentials"), data("token", "t0"))
			first := s.secretCopier(s.name("first"), copierRule(src, inNamespaces(targetNS)))
			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, src.Name, src, ruleOf(first))
			})
			second := s.secretCopier(s.name("second"), copierRule(src, inNamespaces(targetNS)))
			eventually(func(g Gomega) {
				expectReady(g, second, "True", "InSync")
				expectCopy(g, targetNS.Name, src.Name, src, ruleOf(first))
			})
			s.update(first, func() { first.SetFinalizers([]string{holdFinalizer}) })
			DeferCleanup(func() { s.update(first, func() { first.SetFinalizers(nil) }) })

			s.deleteObject(first)

			eventually(func(g Gomega) {
				g.Expect(get(g, first).GetDeletionTimestamp()).NotTo(BeNil())
				copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(second))
				expectOwnedBy(g, copied, second)
			})
		})

	It("lets a second copier of the same source take over a copy the first copier's rule stops selecting",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			narrowed := s.namespace("a", nil)
			kept := s.namespace("b", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			first := s.secretCopier(s.name("first"), copierRule(src, inNamespaces(narrowed, kept)))
			eventually(func(g Gomega) {
				expectCopy(g, narrowed.Name, src.Name, src, ruleOf(first))
			})
			second := s.secretCopier(s.name("second"), copierRule(src, inNamespaces(narrowed)))
			eventually(func(g Gomega) {
				expectReady(g, second, "True", "InSync")
			})
			expectCopyUnwritten(narrowed.Name, src.Name, src, ruleOf(first))

			s.update(first, func() { setRules(first, copierRule(src, inNamespaces(kept))) })

			eventually(func(g Gomega) {
				copied := expectCopy(g, narrowed.Name, src.Name, src, ruleOf(second))
				expectOwnedBy(g, copied, second)
				g.Expect(ownerUIDs(copied)).NotTo(ContainElement(first.GetUID()))
				expectCopy(g, kept.Name, src.Name, src, ruleOf(first))
			})
		})

	It("lets a copier of another source take over a copy, clearing its conflict, once the copier holding it "+
		"stops selecting its namespace", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		other := s.namespace("b", nil)
		firstSource := s.secret(sourceNS.Name, "first", data("token", "f0"))
		secondSource := s.secret(sourceNS.Name, "second", data("token", "s0"))
		first := s.secretCopier(s.name("first"), copierRule(firstSource, inNamespaces(targetNS),
			shaped(copyShape{Name: copyName})))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, copyName, firstSource, ruleOf(first))
		})
		second := s.secretCopier(s.name("second"), copierRule(secondSource, inNamespaces(targetNS),
			shaped(copyShape{Name: copyName})))
		eventually(func(g Gomega) {
			expectReady(g, second, "False", "Conflict")
		})

		s.update(first, func() {
			setRules(first, copierRule(firstSource, inNamespaces(other), shaped(copyShape{Name: copyName})))
		})

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, copyName, secondSource, ruleOf(second))
			expectOwnedBy(g, copied, second)
			expectReady(g, second, "True", "InSync")
		})
	})

	It("lets a second copier of the same source take over a copy whose rule the first copier drops",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			unrelated := s.secret(sourceNS.Name, "unrelated", data("token", "u0"))
			first := s.secretCopier(s.name("first"),
				copierRule(unrelated, inNamespaces(targetNS)), copierRule(src, inNamespaces(targetNS)))
			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, src.Name, src, ruleOf(first))
			})
			second := s.secretCopier(s.name("second"), copierRule(src, inNamespaces(targetNS)))
			eventually(func(g Gomega) {
				expectReady(g, second, "True", "InSync")
			})

			s.update(first, func() { setRules(first, copierRule(unrelated, inNamespaces(targetNS))) })

			eventually(func(g Gomega) {
				copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(second))
				expectOwnedBy(g, copied, second)
			})
		})

	It("keeps a copy with the one of its rules whose source it holds, when two copy different Secrets under one name",
		func() {
			s := newScenario()
			firstNS := s.namespace("src-a", nil)
			secondNS := s.namespace("src-b", nil)
			targetNS := s.namespace("a", nil)
			first := s.secret(firstNS.Name, "credentials", data("token", "first"))
			second := s.secret(secondNS.Name, "credentials", data("token", "second"))

			copier := s.secretCopier(s.name("copier"),
				copierRule(first, inNamespaces(targetNS)), copierRule(second, inNamespaces(targetNS)))

			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, first.Name, first, ruleOf(copier))
			})
			expectCopyUnwritten(targetNS.Name, first.Name, first, ruleOf(copier))

			// The rule copying the Secret the copy holds keeps it once it
			// comes second.
			s.update(copier, func() {
				setRules(copier, copierRule(second, inNamespaces(targetNS)), copierRule(first, inNamespaces(targetNS)))
			})
			s.update(first, func() { first.Data = data("token", "first-1") })

			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, first.Name, first, ruleOf(copier))
			})
			expectCopyUnwritten(targetNS.Name, first.Name, first, ruleOf(copier))
		})

	It("shapes a copy as the first of a SecretExporter's rules that select its namespace, without rewriting it",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", s.scope())
			src := s.secret(sourceNS.Name, "exported", data("token", "t0"))

			exporter := s.secretExporter(sourceNS.Name, src.Name,
				exporterRule(inNamespaces(targetNS), shaped(copyShape{Labels: map[string]string{"rule": "first"}})),
				exporterRule(byLabels(s.scope()), shaped(copyShape{Labels: map[string]string{"rule": "second"}})))
			s.secretImporter(targetNS.Name, src.Name, sharedSecret(uidOf(exporter)))

			eventually(func(g Gomega) {
				copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(exporter))
				g.Expect(copied.Labels).To(HaveKeyWithValue("rule", "first"))
				expectReady(g, exporter, "True", "InSync")
				expectCounts(g, statusRule(g, exporter, 1), map[string]int64{countInSync: 1})
			})
			copied := expectCopyUnwritten(targetNS.Name, src.Name, src, ruleOf(exporter))
			Expect(copied.Labels).To(HaveKeyWithValue("rule", "first"))
		})

	It("leaves a Secret another writer creates under a copy's name, as the copy is first written, untouched",
		Label(expectedDifference), func() {
			requireRacedWrites()
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			unrelated := newSecret(targetNS.Name, src.Name, data("password", "unrelated"))
			race := target.RacedWrites.Before(func(_, path string) bool {
				return strings.HasPrefix(path, "/api/v1/namespaces/"+targetNS.Name+"/secrets")
			}, func() error {
				return target.Client.Create(context.Background(), unrelated.DeepCopy())
			})
			DeferCleanup(race.Cancel)

			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))

			eventually(func(g Gomega) {
				ran, err := race.Ran()
				g.Expect(ran).To(BeTrue(), "the other writer has created its Secret")
				g.Expect(err).NotTo(HaveOccurred())
				expectReady(g, copier, "False", "Conflict")
			})
			consistently(func(g Gomega) {
				latest := secretIn(g, targetNS.Name, src.Name)
				g.Expect(latest.Data).To(Equal(unrelated.Data))
				g.Expect(latest.Annotations).NotTo(HaveKey(annotationCopierRule))
				g.Expect(latest.OwnerReferences).To(BeEmpty())
			})
		})

	It("leaves a Secret another writer puts in a copy's place, as the copy is updated, untouched",
		Label(expectedDifference), func() {
			requireRacedWrites()
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, s.name("credentials"), data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			})
			copied := settledSecret(targetNS.Name, src.Name)
			unrelated := newSecret(targetNS.Name, src.Name, data("password", "unrelated"))
			race := target.RacedWrites.Before(func(method, path string) bool {
				return method == http.MethodPatch && path == "/api/v1/namespaces/"+targetNS.Name+"/secrets/"+src.Name
			}, func() error {
				if err := target.Client.Delete(context.Background(), copied); err != nil {
					return err
				}
				return target.Client.Create(context.Background(), unrelated.DeepCopy())
			})
			DeferCleanup(race.Cancel)

			s.update(src, func() { src.Data = data("token", "t1") })

			eventually(func(g Gomega) {
				ran, err := race.Ran()
				g.Expect(ran).To(BeTrue(), "the other writer has put its Secret in the copy's place")
				g.Expect(err).NotTo(HaveOccurred())
				expectReady(g, copier, "False", "Conflict")
			})
			consistently(func(g Gomega) {
				latest := secretIn(g, targetNS.Name, src.Name)
				g.Expect(latest.Data).To(Equal(unrelated.Data))
				g.Expect(latest.Annotations).NotTo(HaveKey(annotationCopierRule))
				g.Expect(latest.OwnerReferences).To(BeEmpty())
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
