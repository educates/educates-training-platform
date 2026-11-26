package scenarios

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// copyName is the targetSecret.name of scenarios that rename a copy.
	copyName = "copied"

	// appLabel is a label scenarios put on a source, with its value.
	appLabel = "app"
	appValue = "source"

	// teamLabel is a label scenarios set through targetSecret.labels,
	// with its value.
	teamLabel = "team"
	teamValue = "platform"
)

var _ = Describe("SecretCopier copying", Label(areaCopying), func() {
	It("copies the source into every namespace its rule selects, and no other", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		first := s.namespace("a", nil)
		second := s.namespace("b", nil)
		other := s.namespace("other", nil)
		src := s.secret(sourceNS.Name, "credentials", data("username", "user", "password", "secret"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(first, second)))

		eventually(func(g Gomega) {
			expectCopy(g, first.Name, src.Name, src, ruleOf(copier))
			expectCopy(g, second.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, other.Name, src.Name)
		})
	})

	It("never copies the source into its own namespace", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(sourceNS, targetNS),
			shaped(copyShape{Name: copyName})))

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, copyName, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			expectNoSecret(g, sourceNS.Name, copyName)
		})
	})

	It("names the copy targetSecret.name and sets targetSecret.labels on it", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Name: "renamed", Labels: map[string]string{teamLabel: teamValue}})))

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, "renamed", src, ruleOf(copier))
			g.Expect(copied.Labels).To(HaveKeyWithValue(teamLabel, teamValue))
			expectNoSecret(g, targetNS.Name, src.Name)
		})
	})

	It("gives the copy the source's labels merged with targetSecret.labels, the rule winning", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"),
			withLabels(map[string]string{appLabel: appValue, "shared": "source"}))

		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Labels: map[string]string{"shared": "rule", teamLabel: teamValue}})))

		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(copied.Labels).To(HaveKeyWithValue(appLabel, appValue))
			g.Expect(copied.Labels).To(HaveKeyWithValue("shared", "rule"))
			g.Expect(copied.Labels).To(HaveKeyWithValue(teamLabel, teamValue))
		})
	})

	It("gives the copy the source's labels from the moment it creates it", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"),
			withLabels(map[string]string{appLabel: appValue}))

		s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Labels: map[string]string{teamLabel: teamValue}})))

		var first *corev1.Secret
		eventually(func(g Gomega) {
			first = secretIn(g, targetNS.Name, src.Name)
		})
		Expect(first.Labels).To(HaveKeyWithValue(appLabel, appValue), "labels of the copy as first seen")
		Expect(first.Labels).To(HaveKeyWithValue(teamLabel, teamValue), "labels of the copy as first seen")
	})

	It("updates every copy when the source's data changes", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		first := s.namespace("a", nil)
		second := s.namespace("b", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(first, second)))
		eventually(func(g Gomega) {
			expectCopy(g, first.Name, src.Name, src, ruleOf(copier))
			expectCopy(g, second.Name, src.Name, src, ruleOf(copier))
		})

		s.update(src, func() { src.Data = data("token", "t1", "extra", "e1") })

		eventually(func(g Gomega) {
			expectCopy(g, first.Name, src.Name, src, ruleOf(copier))
			expectCopy(g, second.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("removes a key the source dropped from every copy (#50)", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0", "old", "o0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.update(src, func() { delete(src.Data, "old") })

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("removes a label the source dropped from the copies", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"),
			withLabels(map[string]string{appLabel: appValue, "stage": "beta"}))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(copied.Labels).To(HaveKeyWithValue("stage", "beta"))
		})

		s.update(src, func() { delete(src.Labels, "stage") })

		eventually(func(g Gomega) {
			copied := secretIn(g, targetNS.Name, src.Name)
			g.Expect(copied.Labels).To(HaveKeyWithValue(appLabel, appValue))
			g.Expect(copied.Labels).NotTo(HaveKey("stage"))
		})
	})

	It("removes a label dropped from targetSecret.labels from the copies", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Labels: map[string]string{teamLabel: teamValue, "stage": "beta"}})))
		eventually(func(g Gomega) {
			copied := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(copied.Labels).To(HaveKeyWithValue("stage", "beta"))
		})

		s.update(copier, func() {
			setRules(copier, copierRule(src, inNamespaces(targetNS),
				shaped(copyShape{Labels: map[string]string{teamLabel: teamValue}})))
		})

		eventually(func(g Gomega) {
			copied := secretIn(g, targetNS.Name, src.Name)
			g.Expect(copied.Labels).To(HaveKeyWithValue(teamLabel, teamValue))
			g.Expect(copied.Labels).NotTo(HaveKey("stage"))
		})
	})

	It("leaves labels and annotations another writer adds to a copy", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		var copied *corev1.Secret
		eventually(func(g Gomega) {
			copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.update(copied, func() {
			setMapEntry(&copied.Labels, "example.com/backup", "daily")
			setMapEntry(&copied.Annotations, "example.com/note", "added by hand")
		})
		s.update(src, func() { src.Data = data("token", "t1") })

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			latest := secretIn(g, targetNS.Name, src.Name)
			g.Expect(latest.Labels).To(HaveKeyWithValue("example.com/backup", "daily"))
			g.Expect(latest.Annotations).To(HaveKeyWithValue("example.com/note", "added by hand"))
		})
	})

	It("removes a key another writer adds to a copy", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		var copied *corev1.Secret
		eventually(func(g Gomega) {
			copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.update(copied, func() { copied.Data["intruder"] = []byte("x") })

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("takes back a label, the owner reference and the source a copy names when another writer changes them",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, s.name("credentials"), data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
				shaped(copyShape{Labels: map[string]string{teamLabel: teamValue}})))
			var copied *corev1.Secret
			eventually(func(g Gomega) {
				copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
				expectOwnedBy(g, copied, copier)
			})

			s.update(copied, func() {
				copied.Labels[teamLabel] = "changed-by-hand"
				copied.OwnerReferences = nil
				copied.Annotations[annotationSecretName] = sourceNS.Name + "/another-secret"
			})

			eventually(func(g Gomega) {
				restored := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
				g.Expect(restored.Labels).To(HaveKeyWithValue(teamLabel, teamValue))
				expectOwnedBy(g, restored, copier)
			})
		})

	It("takes back a key another server-side applier forces on a copy, and leaves that applier's own label",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, s.name("credentials"), data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			})

			forced := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]any{
					"namespace": targetNS.Name,
					"name":      src.Name,
					"labels":    map[string]any{"example.com/backup": "daily"},
				},
				"stringData": map[string]any{"token": "forced"},
			}}
			Expect(target.Client.Apply(context.Background(), client.ApplyConfigurationFromUnstructured(forced),
				client.FieldOwner("another-applier"), client.ForceOwnership)).To(Succeed())

			eventually(func(g Gomega) {
				restored := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
				g.Expect(restored.Labels).To(HaveKeyWithValue("example.com/backup", "daily"))
			})
		})

	It("removes a key another writer added to a copy as it writes a change of the source",
		Label(expectedDifference), func() {
			requireMissedEvents()
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, s.name("credentials"), data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
			var copied *corev1.Secret
			eventually(func(g Gomega) {
				copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			})
			// The secrets-manager does not hear of the added key, so it
			// first meets it when it writes the copy for another reason.
			heard := target.MissedEvents.Miss(func(obj client.Object) bool {
				return obj.GetNamespace() == targetNS.Name
			})
			DeferCleanup(heard)
			s.update(copied, func() { copied.Data["added-by-hand"] = []byte("x") })
			consistently(func(g Gomega) {
				g.Expect(secretIn(g, targetNS.Name, src.Name).Data).To(HaveKey("added-by-hand"))
			})
			heard()

			s.update(src, func() { src.Data = data("token", "t1", "more", "m") })

			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			})
		})

	It("copies a key whose value is empty, and does not write the copy again", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, s.name("ingress"), map[string][]byte{"ca.crt": {}, "tls.crt": []byte("cert")})
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		holdsCopy := func(g Gomega, copied *corev1.Secret) {
			g.Expect(copied.Annotations).To(HaveKeyWithValue(annotationCopierRule, ruleOf(copier)))
			g.Expect(copied.Data).To(HaveLen(2))
			g.Expect(copied.Data).To(HaveKeyWithValue("ca.crt", BeEmpty()))
			g.Expect(copied.Data).To(HaveKeyWithValue("tls.crt", []byte("cert")))
		}
		eventually(func(g Gomega) {
			holdsCopy(g, secretIn(g, targetNS.Name, src.Name))
			expectReady(g, copier, "True", "InSync")
		})

		settled := settledSecret(targetNS.Name, src.Name)
		// An event that has the copier compare the copy again.
		s.update(src, func() { setMapEntry(&src.Annotations, "example.com/touched", "yes") })

		consistently(func(g Gomega) {
			copied := secretIn(g, targetNS.Name, src.Name)
			holdsCopy(g, copied)
			g.Expect(copied.ResourceVersion).To(Equal(settled.ResourceVersion))
		})
	})

	It("still changes the labels of a copy another writer made immutable", Label(expectedDifference), func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, s.name("credentials"), data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS),
			shaped(copyShape{Labels: map[string]string{teamLabel: "before"}})))
		var copied *corev1.Secret
		eventually(func(g Gomega) {
			copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		immutable := true
		s.update(copied, func() { copied.Immutable = &immutable })

		s.update(copier, func() {
			setRules(copier, copierRule(src, inNamespaces(targetNS),
				shaped(copyShape{Labels: map[string]string{teamLabel: "after"}})))
		})

		eventually(func(g Gomega) {
			changed := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(changed.Labels).To(HaveKeyWithValue(teamLabel, "after"))
			g.Expect(changed.UID).To(Equal(copied.UID))
			expectReady(g, copier, "True", "InSync")
		})
	})

	It("never copies the source's annotations", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"), func(secret *corev1.Secret) {
			secret.Annotations = map[string]string{"example.com/owner": "source-tooling"}
		})
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		consistently(func(g Gomega) {
			g.Expect(secretIn(g, targetNS.Name, src.Name).Annotations).NotTo(HaveKey("example.com/owner"))
		})
	})

	It("does not rewrite the copies when only the source's annotations change", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
		// A secrets-manager may still be writing the copy's first version.
		copied := settledSecret(targetNS.Name, src.Name)

		s.update(src, func() { setMapEntry(&src.Annotations, "example.com/touched", "yes") })

		consistently(func(g Gomega) {
			g.Expect(secretIn(g, targetNS.Name, src.Name).ResourceVersion).To(Equal(copied.ResourceVersion))
		})
	})

	It("restores a copy deleted by hand", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		var copied *corev1.Secret
		eventually(func(g Gomega) {
			copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.deleteObject(copied)

		eventually(func(g Gomega) {
			restored := expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			g.Expect(restored.UID).NotTo(Equal(copied.UID))
		})
	})

	It("restores a copy whose data was edited by hand", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		var copied *corev1.Secret
		eventually(func(g Gomega) {
			copied = expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.update(copied, func() { copied.Data["token"] = []byte("tampered") })

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("copies a source created after its copier", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		pending := newSecret(sourceNS.Name, "kubeconfig", data("config", "c0"))
		copier := s.secretCopier(s.name("copier"), copierRule(pending, inNamespaces(targetNS)))
		consistentlyFor(5*target.Timing.Poll, func(g Gomega) {
			expectNoSecret(g, targetNS.Name, pending.Name)
		})

		src := s.secret(sourceNS.Name, pending.Name, pending.Data)

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("copies into a namespace created after its copier", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		later := s.name("later")
		copier := s.secretCopier(s.name("copier"), copierRule(src, byName(later)))

		s.namespaceNamed(later, nil)

		eventually(func(g Gomega) {
			expectCopy(g, later, src.Name, src, ruleOf(copier))
		})
	})

	It("updates the copies when the source is deleted and recreated", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.deleteObject(src)
		recreated := s.secret(sourceNS.Name, src.Name, data("token", "t1"))

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, recreated, ruleOf(copier))
		})
	})

	It("keeps the copies, with the last data, when the source is deleted", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})

		s.deleteObject(src)

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
	})

	It("recreates the copies with the new type when the source is recreated with another type",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
			})

			s.deleteObject(src)
			recreated := s.secret(sourceNS.Name, src.Name, data("token", "t1"), withType("example.com/token"))

			eventually(func(g Gomega) {
				expectCopy(g, targetNS.Name, src.Name, recreated, ruleOf(copier))
			})
		})

	It("keeps a copy as it is, and reports the failure, when the source's new type would have its recreation rejected",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			targetNS := s.namespace("a", nil)
			src := s.secret(sourceNS.Name, s.name("ingress"), data("ca.crt", "ca", "tls.crt", "cert", "tls.key", "key"))
			copier := s.secretCopier(s.name("copier"),
				copierRule(src, inNamespaces(targetNS), copying(src, "ca.crt")))
			var before *corev1.Secret
			eventually(func(g Gomega) {
				before = secretIn(g, targetNS.Name, src.Name)
				g.Expect(before.Data).To(Equal(data("ca.crt", "ca")))
			})

			s.deleteObject(src)
			s.secret(sourceNS.Name, src.Name, data("ca.crt", "ca", "tls.crt", "cert", "tls.key", "key"),
				withType(corev1.SecretTypeTLS))

			eventually(func(g Gomega) {
				expectReady(g, copier, "False", "TargetErrors")
			})
			consistently(func(g Gomega) {
				kept := secretIn(g, targetNS.Name, src.Name)
				g.Expect(kept.UID).To(Equal(before.UID))
				g.Expect(kept.Type).To(Equal(corev1.SecretTypeOpaque))
				g.Expect(kept.Data).To(Equal(data("ca.crt", "ca")))
			})
		})

	It("keeps the copies of a rule removed from its copier", func() {
		s := newScenario()
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		kept := s.secret(sourceNS.Name, "kept", data("token", "k0"))
		removed := s.secret(sourceNS.Name, "removed", data("token", "r0"))
		copier := s.secretCopier(s.name("copier"),
			copierRule(kept, inNamespaces(targetNS)), copierRule(removed, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, kept.Name, kept, ruleOf(copier))
			expectCopy(g, targetNS.Name, removed.Name, removed, ruleOf(copier))
		})

		s.update(copier, func() { setRules(copier, copierRule(kept, inNamespaces(targetNS))) })

		consistently(func(g Gomega) {
			expectCopy(g, targetNS.Name, removed.Name, removed, ruleOf(copier))
		})
	})

	It("copies a rule's new source into its copies when its sourceSecret changes", Label(expectedDifference), func() {
		s := newScenario()
		firstNS := s.namespace("src-a", nil)
		secondNS := s.namespace("src-b", nil)
		targetNS := s.namespace("a", nil)
		first := s.secret(firstNS.Name, "credentials", data("token", "first"))
		second := s.secret(secondNS.Name, "credentials", data("token", "second"))
		copier := s.secretCopier(s.name("copier"), copierRule(first, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, first.Name, first, ruleOf(copier))
		})

		s.update(copier, func() { setRules(copier, copierRule(second, inNamespaces(targetNS))) })

		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, second.Name, second, ruleOf(copier))
		})
	})

	It("keeps copying into the other namespaces when one target cannot be written (#323)",
		Label(expectedDifference), func() {
			s := newScenario()
			sourceNS := s.namespace("src", nil)
			// The namespaces sort with the failing one first.
			failing := s.namespace("a", nil)
			healthy := s.namespace("b", nil)
			src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
			copierName := s.name("copier")
			immutable := true
			s.secret(failing.Name, src.Name, data("token", "stale"), func(secret *corev1.Secret) {
				secret.Annotations = map[string]string{
					annotationCopierRule: "secretcopier/" + copierName,
					annotationSecretName: src.Namespace + "/" + src.Name,
				}
				secret.Immutable = &immutable
			})

			copier := s.secretCopier(copierName, copierRule(src, inNamespaces(failing, healthy)))

			eventually(func(g Gomega) {
				expectCopy(g, healthy.Name, src.Name, src, ruleOf(copier))
			})
		})
})
