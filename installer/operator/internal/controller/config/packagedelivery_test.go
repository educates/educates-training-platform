package config

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// node builds a Linux node reporting the given kubelet and runtime versions.
func node(name string, kubelet string, runtime string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"kubernetes.io/os": "linux"},
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				KubeletVersion:          kubelet,
				ContainerRuntimeVersion: runtime,
			},
		},
	}
}

func supportedNode(name string) corev1.Node {
	return node(name, "v1.36.4", "containerd://2.3.4")
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		value string
		major int
		minor int
		ok    bool
	}{
		{value: "1.36", major: 1, minor: 36, ok: true},
		{value: "v1.36.4", major: 1, minor: 36, ok: true},
		// Real clusters carry distribution suffixes.
		{value: "v1.35.0-eks-abc123", major: 1, minor: 35, ok: true},
		{value: "2.1.4-k3s1", major: 2, minor: 1, ok: true},
		{value: "v1.36-rc1", major: 1, minor: 36, ok: true},
		{value: "1.36.4+gke.100", major: 1, minor: 36, ok: true},
		{value: "", ok: false},
		{value: "nonsense", ok: false},
		{value: "1", ok: false},
		{value: "v.x", ok: false},
	}

	for _, test := range cases {
		t.Run(test.value, func(t *testing.T) {
			parsed, ok := parseVersion(test.value)

			if ok != test.ok {
				t.Fatalf("parseVersion(%q) ok = %v, want %v", test.value, ok, test.ok)
			}

			if !test.ok {
				return
			}

			if parsed.major != test.major || parsed.minor != test.minor {
				t.Errorf("parseVersion(%q) = %d.%d, want %d.%d",
					test.value, parsed.major, parsed.minor, test.major, test.minor)
			}
		})
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		have string
		want string
		ok   bool
	}{
		{have: "1.36", want: "1.36", ok: true},
		{have: "1.37", want: "1.36", ok: true},
		{have: "2.0", want: "1.36", ok: true},
		{have: "1.35", want: "1.36", ok: false},
		{have: "0.9", want: "1.0", ok: false},
		// A major bump outranks a smaller minor.
		{have: "2.1", want: "1.99", ok: true},
	}

	for _, test := range cases {
		have, _ := parseVersion(test.have)
		want, _ := parseVersion(test.want)

		if got := have.atLeast(want); got != test.ok {
			t.Errorf("%s atLeast %s = %v, want %v", test.have, test.want, got, test.ok)
		}
	}
}

func TestRuntimeSupportsImageVolumes(t *testing.T) {
	cases := []struct {
		runtime string
		ok      bool
		mention string
	}{
		{runtime: "containerd://2.1.0", ok: true},
		{runtime: "containerd://2.3.4", ok: true},
		{runtime: "containerd://2.1.4-k3s1", ok: true},
		{runtime: "cri-o://1.31.0", ok: true},
		{runtime: "cri-o://1.34.2", ok: true},
		{runtime: "containerd://1.7.27", mention: "older than 2.1"},
		{runtime: "cri-o://1.30.0", mention: "older than 1.31"},
		{runtime: "docker://24.0.7", mention: "does not support image volumes"},
		{runtime: "containerd://nonsense", mention: "could not be read"},
		{runtime: "bare-string", mention: "not recognised"},
	}

	for _, test := range cases {
		t.Run(test.runtime, func(t *testing.T) {
			ok, why := runtimeSupportsImageVolumes(test.runtime)

			if ok != test.ok {
				t.Fatalf("runtimeSupportsImageVolumes(%q) = %v (%s), want %v", test.runtime, ok, why, test.ok)
			}

			if test.mention != "" && !strings.Contains(why, test.mention) {
				t.Errorf("expected the explanation to mention %q, got %q", test.mention, why)
			}
		})
	}
}

func TestProbeImageMountSupport(t *testing.T) {
	t.Run("a supported cluster", func(t *testing.T) {
		ok, reason, message := probeImageMountSupport("v1.36.4", []corev1.Node{
			supportedNode("worker-1"), supportedNode("worker-2"),
		})

		if !ok {
			t.Fatalf("expected support, got %s: %s", reason, message)
		}

		if reason != reasonClusterSupportsImageVolumes {
			t.Errorf("reason = %q", reason)
		}

		for _, want := range []string{"1.36", "all 2 nodes"} {
			if !strings.Contains(message, want) {
				t.Errorf("expected the message to mention %q, got %q", want, message)
			}
		}
	})

	t.Run("an old API server", func(t *testing.T) {
		ok, reason, message := probeImageMountSupport("v1.34.2", []corev1.Node{supportedNode("worker-1")})

		if ok {
			t.Fatalf("expected no support")
		}

		if reason != reasonAPIServerTooOld {
			t.Errorf("reason = %q, want %q", reason, reasonAPIServerTooOld)
		}

		if !strings.Contains(message, "1.34") || !strings.Contains(message, "1.36") {
			t.Errorf("expected the message to name both versions, got %q", message)
		}
	})

	t.Run("one offending node is named", func(t *testing.T) {
		ok, reason, message := probeImageMountSupport("v1.36.4", []corev1.Node{
			supportedNode("worker-1"),
			node("worker-2", "v1.36.4", "containerd://1.7.27"),
		})

		if ok {
			t.Fatalf("expected no support")
		}

		if reason != reasonNodesUnsupported {
			t.Errorf("reason = %q, want %q", reason, reasonNodesUnsupported)
		}

		if !strings.Contains(message, "worker-2") {
			t.Errorf("expected the offending node to be named, got %q", message)
		}

		if strings.Contains(message, "worker-1") {
			t.Errorf("a supported node should not be named, got %q", message)
		}
	})

	t.Run("an old kubelet is named", func(t *testing.T) {
		_, reason, message := probeImageMountSupport("v1.36.4", []corev1.Node{
			node("worker-1", "v1.34.1", "containerd://2.3.4"),
		})

		if reason != reasonNodesUnsupported {
			t.Errorf("reason = %q", reason)
		}

		if !strings.Contains(message, "kubelet") {
			t.Errorf("expected the message to name the kubelet, got %q", message)
		}
	})

	t.Run("many offending nodes are summarised", func(t *testing.T) {
		nodes := make([]corev1.Node, 0, 8)

		for index := range 8 {
			nodes = append(nodes, node(fmt.Sprintf("worker-%d", index), "v1.36.4", "containerd://1.7.27"))
		}

		_, _, message := probeImageMountSupport("v1.36.4", nodes)

		if !strings.Contains(message, "and 5 more nodes") {
			t.Errorf("expected the message to summarise the rest, got %q", message)
		}
	})

	t.Run("nodes of other operating systems are not counted", func(t *testing.T) {
		windows := node("windows-1", "v1.30.0", "docker://24.0.7")
		windows.Labels["kubernetes.io/os"] = "windows"

		ok, _, message := probeImageMountSupport("v1.36.4", []corev1.Node{
			supportedNode("worker-1"), windows,
		})

		if !ok {
			t.Fatalf("a Windows node should not block support, got %q", message)
		}

		if !strings.Contains(message, "all 1 nodes") {
			t.Errorf("expected only Linux nodes to be counted, got %q", message)
		}
	})

	t.Run("a node which does not say it is Linux is not counted", func(t *testing.T) {
		unlabelled := node("mystery-1", "v1.30.0", "docker://24.0.7")
		unlabelled.Labels = nil

		ok, _, message := probeImageMountSupport("v1.36.4", []corev1.Node{
			supportedNode("worker-1"), unlabelled,
		})

		if !ok {
			t.Fatalf("an unlabelled node should not block support, got %q", message)
		}

		if !strings.Contains(message, "all 1 nodes") {
			t.Errorf("expected only labelled Linux nodes to be counted, got %q", message)
		}
	})

	t.Run("an unreadable API server version", func(t *testing.T) {
		_, reason, _ := probeImageMountSupport("nonsense", []corev1.Node{supportedNode("worker-1")})

		if reason != reasonProbeFailed {
			t.Errorf("reason = %q, want %q", reason, reasonProbeFailed)
		}
	})

	t.Run("no nodes at all", func(t *testing.T) {
		ok, reason, _ := probeImageMountSupport("v1.36.4", nil)

		if ok || reason != reasonProbeFailed {
			t.Errorf("expected a probe failure with no nodes, got ok=%v reason=%q", ok, reason)
		}
	})
}

func TestResolveImageMount(t *testing.T) {
	supported := []corev1.Node{supportedNode("worker-1")}
	unsupported := []corev1.Node{node("worker-1", "v1.36.4", "containerd://1.7.27")}

	t.Run("Auto enables on a supported cluster", func(t *testing.T) {
		effective, reason, _ := resolveImageMount(configv1alpha1.DeliveryModeAuto, "v1.36.4", supported)

		if effective != configv1alpha1.EffectiveDeliveryModeEnabled {
			t.Errorf("effective = %q, want Enabled", effective)
		}

		if reason != reasonClusterSupportsImageVolumes {
			t.Errorf("reason = %q", reason)
		}
	})

	t.Run("Auto falls back on an unsupported cluster", func(t *testing.T) {
		effective, reason, message := resolveImageMount(configv1alpha1.DeliveryModeAuto, "v1.36.4", unsupported)

		if effective != configv1alpha1.EffectiveDeliveryModeDisabled {
			t.Errorf("effective = %q, want Disabled", effective)
		}

		if reason != reasonNodesUnsupported {
			t.Errorf("reason = %q", reason)
		}

		if !strings.Contains(message, "worker-1") {
			t.Errorf("expected the reason to name the node, got %q", message)
		}
	})

	t.Run("an empty mode behaves as Auto", func(t *testing.T) {
		effective, _, _ := resolveImageMount("", "v1.36.4", supported)

		if effective != configv1alpha1.EffectiveDeliveryModeEnabled {
			t.Errorf("effective = %q, want Enabled", effective)
		}
	})

	t.Run("Enabled never refuses", func(t *testing.T) {
		effective, reason, message := resolveImageMount(configv1alpha1.DeliveryModeEnabled, "v1.30.0", unsupported)

		if effective != configv1alpha1.EffectiveDeliveryModeEnabled {
			t.Errorf("effective = %q, want Enabled even on an unsupported cluster", effective)
		}

		if reason != reasonPackageDeliveryModeEnabled {
			t.Errorf("reason = %q", reason)
		}

		// The probe result still reaches the operator as advice.
		if !strings.Contains(message, "may not support it") {
			t.Errorf("expected the message to carry the warning, got %q", message)
		}
	})

	t.Run("Disabled always disables", func(t *testing.T) {
		effective, reason, _ := resolveImageMount(configv1alpha1.DeliveryModeDisabled, "v1.36.4", supported)

		if effective != configv1alpha1.EffectiveDeliveryModeDisabled {
			t.Errorf("effective = %q, want Disabled", effective)
		}

		if reason != reasonPackageDeliveryModeDisabled {
			t.Errorf("reason = %q", reason)
		}
	})

	t.Run("Disabled does not probe", func(t *testing.T) {
		// A cluster which would fail the probe still resolves cleanly.
		effective, _, message := resolveImageMount(configv1alpha1.DeliveryModeDisabled, "nonsense", nil)

		if effective != configv1alpha1.EffectiveDeliveryModeDisabled {
			t.Errorf("effective = %q", effective)
		}

		if strings.Contains(message, "could not be read") {
			t.Errorf("Disabled should not report a probe result, got %q", message)
		}
	})
}
