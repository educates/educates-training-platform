package config

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// TestResolveImageMount_RealClusterVersions pins the thresholds against the
// versions a current kind cluster reports, so a threshold change which would
// stop a supported cluster from mounting fails here.
func TestResolveImageMount_RealClusterVersions(t *testing.T) {
	nodes := []corev1.Node{
		node("educates-control-plane", "v1.36.4", "containerd://2.3.4"),
	}

	effective, reason, message := resolveImageMount(configv1alpha1.DeliveryModeAuto, "v1.36.4", nodes)

	if effective != configv1alpha1.EffectiveDeliveryModeEnabled {
		t.Fatalf("this cluster should support mounting, got %q: %s", effective, message)
	}

	if reason != reasonClusterSupportsImageVolumes {
		t.Errorf("reason = %q", reason)
	}

	t.Logf("resolved %s: %s", effective, message)
}
