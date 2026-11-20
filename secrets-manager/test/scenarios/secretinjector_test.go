package scenarios

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// pullSecretName is the name of a scenario's image pull Secret.
	pullSecretName = "registry-credentials"

	// builder and runner are the names of a scenario's service accounts.
	builder = "builder"
	runner  = "runner"

	// injectLabel marks the Secrets a scenario's injector selects by label.
	injectLabel = "inject"
)

var _ = Describe("SecretInjector", Label(areaInjection), func() {
	It("adds a dockerconfigjson Secret to the imagePullSecrets of the service accounts it selects", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		pull := s.pullSecret(ns.Name, pullSecretName)
		s.serviceAccount(ns.Name, builder, nil)
		s.serviceAccount(ns.Name, runner, nil)

		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byName(pull.Name), accounts(builder)))

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(pull.Name))
		})
		consistently(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, runner))).NotTo(ContainElement(pull.Name))
		})
	})

	It("adds a Secret of another type to the service account's secrets", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		opaque := s.secret(ns.Name, "api-token", data("token", "t0"))
		s.serviceAccount(ns.Name, builder, nil)

		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byName(opaque.Name), accounts(builder)))

		eventually(func(g Gomega) {
			account := serviceAccountIn(g, ns.Name, builder)
			g.Expect(secretNames(account)).To(ContainElement(opaque.Name))
			g.Expect(pullSecretNames(account)).NotTo(ContainElement(opaque.Name))
		})
	})

	It("selects Secrets, service accounts and namespaces by labels", func() {
		s := newScenario()
		selected := s.namespace("a", s.scope())
		unselected := s.namespace("b", nil)
		for _, ns := range []*corev1.Namespace{selected, unselected} {
			s.pullSecret(ns.Name, "labelled", withLabels(map[string]string{injectLabel: "yes"}))
			s.pullSecret(ns.Name, "unlabelled")
			s.serviceAccount(ns.Name, builder, map[string]string{"role": "build"})
			s.serviceAccount(ns.Name, runner, nil)
		}

		s.secretInjector(s.name("injector"), injectorRule(
			byLabels(s.scope()),
			byLabels(map[string]string{injectLabel: "yes"}),
			accountsBy(byExpressions(expression("role", "In", "build")))))

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, selected.Name, builder))).To(ContainElement("labelled"))
		})
		consistently(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, selected.Name, builder))).NotTo(ContainElement("unlabelled"))
			g.Expect(pullSecretNames(serviceAccountIn(g, selected.Name, runner))).To(BeEmpty())
			g.Expect(pullSecretNames(serviceAccountIn(g, unselected.Name, builder))).To(BeEmpty())
		})
	})

	It("injects into every service account of the namespace when the rule names none", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		pull := s.pullSecret(ns.Name, pullSecretName)
		s.serviceAccount(ns.Name, builder, nil)
		s.serviceAccount(ns.Name, runner, nil)

		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byName(pull.Name)))

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(pull.Name))
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, runner))).To(ContainElement(pull.Name))
		})
	})

	It("injects into a service account created after the injector", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		pull := s.pullSecret(ns.Name, pullSecretName)
		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byName(pull.Name), accounts(builder)))

		s.serviceAccount(ns.Name, builder, nil)

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(pull.Name))
		})
	})

	It("injects a Secret created after the injector", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		s.serviceAccount(ns.Name, builder, nil)
		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byName(pullSecretName), accounts(builder)))

		s.pullSecret(ns.Name, pullSecretName)

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(pullSecretName))
		})
	})

	It("injects in a namespace relabelled into the rule's scope", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		pull := s.pullSecret(ns.Name, pullSecretName)
		s.serviceAccount(ns.Name, builder, nil)
		s.secretInjector(s.name("injector"), injectorRule(byLabels(s.scope()), byName(pull.Name), accounts(builder)))
		consistentlyFor(5*target.Timing.Poll, func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(BeEmpty())
		})

		s.update(ns, func() { setMapEntry(&ns.Labels, scenarioLabel, s.id) })

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(pull.Name))
		})
	})

	It("never removes a name it injected", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		pull := s.pullSecret(ns.Name, pullSecretName)
		opaque := s.secret(ns.Name, "api-token", data("token", "t0"))
		s.serviceAccount(ns.Name, builder, nil)
		injector := s.secretInjector(s.name("injector"),
			injectorRule(inNamespaces(ns), byName(pull.Name, opaque.Name), accounts(builder)))
		eventually(func(g Gomega) {
			account := serviceAccountIn(g, ns.Name, builder)
			g.Expect(pullSecretNames(account)).To(ContainElement(pull.Name))
			g.Expect(secretNames(account)).To(ContainElement(opaque.Name))
		})

		s.deleteObject(pull)
		s.deleteObject(injector)

		consistently(func(g Gomega) {
			account := serviceAccountIn(g, ns.Name, builder)
			g.Expect(pullSecretNames(account)).To(ContainElement(pull.Name))
			g.Expect(secretNames(account)).To(ContainElement(opaque.Name))
		})
	})

	It("matches service account names exactly", func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		pull := s.pullSecret(ns.Name, pullSecretName)
		s.serviceAccount(ns.Name, builder, nil)

		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byName(pull.Name), accounts("build*")))

		consistently(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(BeEmpty())
		})
	})

	It("adds a kubernetes.io/dockercfg Secret to imagePullSecrets", Label(expectedDifference), func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		legacy := s.secret(ns.Name, "legacy-credentials",
			data(corev1.DockerConfigKey, `{"registry.example.com":{"auth":"dXNlcjpwYXNz"}}`), withType(typeDockerCfg))
		s.serviceAccount(ns.Name, builder, nil)

		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byName(legacy.Name), accounts(builder)))

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(legacy.Name))
		})
	})

	It("does not add a name twice when the existing entry carries more fields than its name",
		Label(expectedDifference), func() {
			s := newScenario()
			ns := s.namespace("a", nil)
			listed := s.secret(ns.Name, "listed", data("token", "l0"))
			fresh := s.secret(ns.Name, "fresh", data("token", "f0"))
			account := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: builder, Namespace: ns.Name},
				Secrets:    []corev1.ObjectReference{{Name: listed.Name, Namespace: ns.Name}},
			}
			s.create(account)
			// An API server that keeps only the name of each entry leaves
			// nothing to tell the entry from the name the injector adds.
			if account.Secrets[0].Namespace == "" {
				Skip("the API server keeps only the name of a service account's secrets entry")
			}

			s.secretInjector(s.name("injector"),
				injectorRule(inNamespaces(ns), byName(listed.Name, fresh.Name), accounts(builder)))

			eventually(func(g Gomega) {
				g.Expect(secretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(fresh.Name))
			})
			consistently(func(g Gomega) {
				names := secretNames(serviceAccountIn(g, ns.Name, builder))
				g.Expect(names).To(HaveExactElements(listed.Name, fresh.Name))
			})
		})

	It("selects source Secrets by name globs, excluding names under !", Label(expectedDifference), func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		s.pullSecret(ns.Name, "first-pull-secret")
		s.pullSecret(ns.Name, "second-pull-secret")
		s.pullSecret(ns.Name, "skip-pull-secret")
		s.serviceAccount(ns.Name, builder, nil)

		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns),
			byName("*-pull-secret", "!skip-pull-secret"), accounts(builder)))

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).
				To(ContainElements("first-pull-secret", "second-pull-secret"))
		})
		consistently(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).NotTo(ContainElement("skip-pull-secret"))
		})
	})

	It("selects source Secrets by owner", Label(expectedDifference), func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: ns.Name}}
		s.create(owner)
		owned := s.pullSecret(ns.Name, "owned", func(secret *corev1.Secret) {
			secret.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "v1", Kind: "ConfigMap", Name: owner.Name, UID: owner.UID,
			}}
		})
		s.pullSecret(ns.Name, "unowned")
		s.serviceAccount(ns.Name, builder, nil)

		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns),
			byOwners(ownerOf("v1", "ConfigMap", owner.Name, owner.UID)), accounts(builder)))

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(owned.Name))
		})
		consistently(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).NotTo(ContainElement("unowned"))
		})
	})

	It("selects source Secrets by UID", Label(expectedDifference), func() {
		s := newScenario()
		ns := s.namespace("a", nil)
		chosen := s.pullSecret(ns.Name, "chosen")
		s.pullSecret(ns.Name, "other")
		s.serviceAccount(ns.Name, builder, nil)

		s.secretInjector(s.name("injector"), injectorRule(inNamespaces(ns), byUIDs(chosen.UID), accounts(builder)))

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).To(ContainElement(chosen.Name))
		})
		consistently(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, ns.Name, builder))).NotTo(ContainElement("other"))
		})
	})

	It("selects target namespaces by owner", Label(expectedDifference), func() {
		s := newScenario()
		owner := s.namespace("owner", nil)
		owned := s.ownedNamespace("a", owner)
		other := s.namespace("b", nil)
		for _, ns := range []*corev1.Namespace{owned, other} {
			s.pullSecret(ns.Name, pullSecretName)
			s.serviceAccount(ns.Name, builder, nil)
		}

		s.secretInjector(s.name("injector"), injectorRule(
			merged(inNamespaces(owned, other), byOwners(namespaceOwner(owner))),
			byName(pullSecretName), accounts(builder)))

		eventually(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, owned.Name, builder))).To(ContainElement(pullSecretName))
		})
		consistently(func(g Gomega) {
			g.Expect(pullSecretNames(serviceAccountIn(g, other.Name, builder))).To(BeEmpty())
		})
	})
})
