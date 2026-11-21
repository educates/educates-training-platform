package scenarios

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// secretsManagerName names the Deployment the secrets-manager runs as, and
// the Lease its replicas elect their leader with, in its namespace.
const secretsManagerName = "secrets-manager"

// requireDeployedSecretsManager skips the scenario on a target whose
// secrets-manager does not run as a Deployment on the cluster.
func requireDeployedSecretsManager() {
	if target.SecretsManagerNamespace == "" {
		Skip(target.Name + " does not run its secrets-manager as a Deployment")
	}
}

// secretsManagerKey names the Deployment and the Lease of the target's
// secrets-manager.
func secretsManagerKey() types.NamespacedName {
	return types.NamespacedName{Namespace: target.SecretsManagerNamespace, Name: secretsManagerName}
}

// secretsManagerPods lists the pods of the target's secrets-manager, those
// being deleted among them.
func secretsManagerPods(g Gomega) []corev1.Pod {
	deployment := &appsv1.Deployment{}
	g.Expect(target.Client.Get(context.Background(), secretsManagerKey(), deployment)).To(Succeed())
	pods := &corev1.PodList{}
	g.Expect(target.Client.List(context.Background(), pods, client.InNamespace(target.SecretsManagerNamespace),
		client.MatchingLabels(deployment.Spec.Selector.MatchLabels))).To(Succeed())
	return pods.Items
}

// isReady reports whether pod runs, is ready and is not being deleted.
func isReady(pod *corev1.Pod) bool {
	if !pod.DeletionTimestamp.IsZero() {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// leader returns the name of the pod holding the Lease of the target's
// secrets-manager, from the holder identity controller-runtime gives it,
// <pod name>_<id>, or "" when no pod holds it.
func leader(g Gomega) string {
	lease := &coordinationv1.Lease{}
	g.Expect(target.Client.Get(context.Background(), secretsManagerKey(), lease)).To(Succeed())
	if lease.Spec.HolderIdentity == nil {
		return ""
	}
	pod, _, _ := strings.Cut(*lease.Spec.HolderIdentity, "_")
	return pod
}

// scaleSecretsManager scales the Deployment of the target's secrets-manager
// to replicas, through its scale subresource as kubectl scale does, and
// waits until it runs that many ready pods and no other, one of them
// holding the Lease.
func (s *scenario) scaleSecretsManager(replicas int32) {
	GinkgoHelper()
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Namespace: target.SecretsManagerNamespace, Name: secretsManagerName,
	}}
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		scale := &autoscalingv1.Scale{}
		if err := target.Client.SubResource("scale").Get(s.ctx, deployment, scale); err != nil {
			return err
		}
		scale.Spec.Replicas = replicas
		return target.Client.SubResource("scale").Update(s.ctx, deployment, client.WithSubResourceBody(scale))
	})).To(Succeed())

	eventually(func(g Gomega) {
		pods := secretsManagerPods(g)
		g.Expect(pods).To(HaveLen(int(replicas)), "pods of the secrets-manager")
		names := make([]string, 0, len(pods))
		for i := range pods {
			g.Expect(isReady(&pods[i])).To(BeTrue(), "pod %s of the secrets-manager is ready", pods[i].Name)
			names = append(names, pods[i].Name)
		}
		if replicas > 0 {
			g.Expect(names).To(ContainElement(leader(g)), "the pod leading the secrets-manager")
		}
	})
}

// scaleSecretsManagerFor scales the Deployment of the target's
// secrets-manager to replicas for the rest of the scenario, and back to
// the replicas it has now when the scenario ends.
func (s *scenario) scaleSecretsManagerFor(replicas int32) {
	GinkgoHelper()
	deployment := &appsv1.Deployment{}
	Expect(target.Client.Get(s.ctx, secretsManagerKey(), deployment)).To(Succeed())
	original := int32(1)
	if deployment.Spec.Replicas != nil {
		original = *deployment.Spec.Replicas
	}
	DeferCleanup(s.scaleSecretsManager, original)
	s.scaleSecretsManager(replicas)
}

// cordonNodes marks every schedulable node of the target unschedulable, as
// kubectl cordon does, so that no new pod starts, and marks them
// schedulable again when the scenario ends.
func (s *scenario) cordonNodes() {
	GinkgoHelper()
	nodes := &corev1.NodeList{}
	Expect(target.Client.List(s.ctx, nodes)).To(Succeed())
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if node.Spec.Unschedulable {
			continue
		}
		DeferCleanup(s.setUnschedulable, node.Name, false)
		s.setUnschedulable(node.Name, true)
	}
}

// setUnschedulable marks the node name unschedulable, or schedulable again.
func (s *scenario) setUnschedulable(name string, unschedulable bool) {
	GinkgoHelper()
	patch := `{"spec":{"unschedulable":null}}`
	if unschedulable {
		patch = `{"spec":{"unschedulable":true}}`
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
	Expect(target.Client.Patch(s.ctx, node, client.RawPatch(types.MergePatchType, []byte(patch)))).To(Succeed())
}

var _ = Describe("The deployed secrets-manager", Label(liveOnly, expectedDifference), Serial, func() {
	It("lets the namespaces of a SecretExporter and its SecretImporter be deleted while it is not running (#291)",
		Label(areaExportImport), func() {
			requireDeployedSecretsManager()
			if !target.NamespaceController {
				Skip(target.Name + " has no namespace controller")
			}
			s := newScenario()
			exporterNS := s.namespace("src", nil)
			importerNS := s.namespace("a", nil)
			src := s.secret(exporterNS.Name, "exported", data("token", "t0"))
			exporter := s.secretExporter(exporterNS.Name, src.Name, exporterRule(inNamespaces(importerNS)))
			s.secretImporter(importerNS.Name, src.Name, sharedSecret(uidOf(exporter)))
			eventually(func(g Gomega) {
				expectCopy(g, importerNS.Name, src.Name, src, ruleOf(exporter))
			})

			// No replica runs, as when the secrets-manager is uninstalled.
			s.scaleSecretsManagerFor(0)
			s.deleteObject(exporterNS)
			s.deleteObject(importerNS)

			eventually(func(g Gomega) {
				for _, ns := range []*corev1.Namespace{exporterNS, importerNS} {
					err := target.Client.Get(context.Background(), client.ObjectKeyFromObject(ns), &corev1.Namespace{})
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "namespace %s should be gone, got %v", ns.Name, err)
				}
			})
		})

	It("hands over to a standby replica when the leading pod is deleted", Label(areaConvergence), func() {
		requireDeployedSecretsManager()
		s := newScenario()
		s.scaleSecretsManagerFor(2)
		var leading, standby string
		eventually(func(g Gomega) {
			leading = leader(g)
			for _, pod := range secretsManagerPods(g) {
				if pod.Name != leading {
					standby = pod.Name
				}
			}
			g.Expect(standby).NotTo(BeEmpty(), "a standby pod")
		})

		// The pod replacing the deleted one cannot start, and so cannot win
		// the Lease before the standby does.
		s.cordonNodes()
		Expect(target.Client.Delete(s.ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Namespace: target.SecretsManagerNamespace, Name: leading,
		}})).To(Succeed())

		eventually(func(g Gomega) {
			g.Expect(leader(g)).To(Equal(standby), "the standby %s leads", standby)
		})
		sourceNS := s.namespace("src", nil)
		targetNS := s.namespace("a", nil)
		src := s.secret(sourceNS.Name, "credentials", data("token", "t0"))
		copier := s.secretCopier(s.name("copier"), copierRule(src, inNamespaces(targetNS)))
		eventually(func(g Gomega) {
			expectCopy(g, targetNS.Name, src.Name, src, ruleOf(copier))
		})
	})
})
