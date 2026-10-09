package config

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// conditionPackageImageMountAvailable reports whether extension packages
// declared as an image are mounted into workshop sessions. It sits outside
// the aggregate Ready condition: fetching instead of mounting is a valid
// steady state, not a fault.
const conditionPackageImageMountAvailable = "PackageImageMountAvailable"

// Mounting an extension package image needs the image volume feature, which
// is generally available from Kubernetes 1.36, and a container runtime which
// implements it on every node a session might land on.
var (
	minAPIServerVersion  = version{1, 36}
	minKubeletVersion    = version{1, 35}
	minContainerdVersion = version{2, 1}
	minCRIOVersion       = version{1, 31}
)

// Condition reasons published on PackageImageMountAvailable.
const (
	reasonClusterSupportsImageVolumes = "ClusterSupportsImageVolumes"
	reasonPackageDeliveryModeEnabled  = "ModeEnabled"
	reasonPackageDeliveryModeDisabled = "ModeDisabled"
	reasonAPIServerTooOld             = "APIServerTooOld"
	reasonNodesUnsupported            = "NodesUnsupported"
	reasonProbeFailed                 = "ProbeFailed"
)

// maxReportedNodes caps how many offending nodes a condition message names
// before it summarises the rest, so the message stays readable on a large
// cluster.
const maxReportedNodes = 3

// version is a major.minor pair. Patch releases never gate a feature here, so
// they are parsed and discarded.
type version struct {
	major int
	minor int
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d", v.major, v.minor)
}

// atLeast reports whether v is the same as or newer than other.
func (v version) atLeast(other version) bool {
	if v.major != other.major {
		return v.major > other.major
	}

	return v.minor >= other.minor
}

// parseVersion reads a major.minor prefix, ignoring any leading "v" and
// anything after the minor component. Real clusters report versions such as
// "v1.36.4-eks-abc123" and "containerd://2.1.4-k3s1", so only the leading
// numbers are meaningful.
func parseVersion(value string) (version, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")

	if value == "" {
		return version{}, false
	}

	parts := strings.SplitN(value, ".", 3)

	if len(parts) < 2 {
		return version{}, false
	}

	major, err := strconv.Atoi(parts[0])

	if err != nil {
		return version{}, false
	}

	// The minor component may carry a suffix when there is no patch number,
	// as in "1.36-rc1".
	minorText := parts[1]

	for index, character := range minorText {
		if character < '0' || character > '9' {
			minorText = minorText[:index]
			break
		}
	}

	minor, err := strconv.Atoi(minorText)

	if err != nil {
		return version{}, false
	}

	return version{major: major, minor: minor}, true
}

// runtimeSupportsImageVolumes reports whether a node's container runtime can
// mount an image, given the string a node reports in its status. The scheme
// names the runtime, as in "containerd://2.1.4" or "cri-o://1.31.0".
func runtimeSupportsImageVolumes(runtimeVersion string) (bool, string) {
	scheme, rest, found := strings.Cut(runtimeVersion, "://")

	if !found {
		return false, fmt.Sprintf("container runtime %q is not recognised", runtimeVersion)
	}

	parsed, ok := parseVersion(rest)

	if !ok {
		return false, fmt.Sprintf("container runtime version %q could not be read", runtimeVersion)
	}

	switch scheme {
	case "containerd":
		if !parsed.atLeast(minContainerdVersion) {
			return false, fmt.Sprintf("containerd %s is older than %s", parsed, minContainerdVersion)
		}

		return true, ""

	case "cri-o":
		if !parsed.atLeast(minCRIOVersion) {
			return false, fmt.Sprintf("CRI-O %s is older than %s", parsed, minCRIOVersion)
		}

		return true, ""

	default:
		// Docker and anything unrecognised cannot mount an image.
		return false, fmt.Sprintf("container runtime %q does not support image volumes", scheme)
	}
}

// nodeSupportsImageVolumes reports whether one node can mount an image,
// explaining what fails when it cannot.
func nodeSupportsImageVolumes(node *corev1.Node) (bool, string) {
	kubelet, ok := parseVersion(node.Status.NodeInfo.KubeletVersion)

	if !ok {
		return false, fmt.Sprintf(
			"node %s: kubelet version %q could not be read",
			node.Name, node.Status.NodeInfo.KubeletVersion,
		)
	}

	if !kubelet.atLeast(minKubeletVersion) {
		return false, fmt.Sprintf(
			"node %s: kubelet %s is older than %s",
			node.Name, kubelet, minKubeletVersion,
		)
	}

	supported, why := runtimeSupportsImageVolumes(node.Status.NodeInfo.ContainerRuntimeVersion)

	if !supported {
		return false, fmt.Sprintf("node %s: %s", node.Name, why)
	}

	return true, ""
}

// probeImageMountSupport reports whether every Linux node in the cluster can
// mount an image, given the API server version. Nodes of other operating
// systems never run workshop sessions, so they are not counted.
//
// The second return value explains the outcome either way, for the condition
// message.
func probeImageMountSupport(apiServerVersion string, nodes []corev1.Node) (bool, string, string) {
	serverVersion, ok := parseVersion(apiServerVersion)

	if !ok {
		return false, reasonProbeFailed, fmt.Sprintf(
			"the API server version %q could not be read", apiServerVersion,
		)
	}

	if !serverVersion.atLeast(minAPIServerVersion) {
		return false, reasonAPIServerTooOld, fmt.Sprintf(
			"API server %s is older than %s", serverVersion, minAPIServerVersion,
		)
	}

	var problems []string

	counted := 0

	for index := range nodes {
		node := &nodes[index]

		// Only Linux nodes run workshop sessions. The caller selects them
		// server side; this repeats the rule so the check can be exercised
		// without a cluster, and both agree that a node which does not say
		// it is Linux is not counted.
		if node.Labels["kubernetes.io/os"] != "linux" {
			continue
		}

		counted++

		if supported, why := nodeSupportsImageVolumes(node); !supported {
			problems = append(problems, why)
		}
	}

	if counted == 0 {
		return false, reasonProbeFailed, "no Linux nodes were found to probe"
	}

	if len(problems) != 0 {
		sort.Strings(problems)

		return false, reasonNodesUnsupported, summariseProblems(problems)
	}

	return true, reasonClusterSupportsImageVolumes, fmt.Sprintf(
		"API server %s; all %d nodes meet kubelet %s and containerd %s or CRI-O %s",
		serverVersion, counted, minKubeletVersion, minContainerdVersion, minCRIOVersion,
	)
}

// summariseProblems joins the offending nodes, naming a handful and counting
// the rest.
func summariseProblems(problems []string) string {
	if len(problems) <= maxReportedNodes {
		return strings.Join(problems, "; ")
	}

	remaining := len(problems) - maxReportedNodes

	return fmt.Sprintf(
		"%s; and %d more nodes",
		strings.Join(problems[:maxReportedNodes], "; "), remaining,
	)
}

// resolveImageMount settles the configured mode against what the cluster
// supports, returning the effective value plus the reason and message for the
// PackageImageMountAvailable condition.
//
// Enabled never refuses: the probe still runs so its result can be reported as
// advice, but an administrator who forces the mechanism gets it.
func resolveImageMount(
	mode configv1alpha1.DeliveryMode,
	apiServerVersion string,
	nodes []corev1.Node,
) (configv1alpha1.EffectiveDeliveryMode, string, string) {
	switch mode {
	case configv1alpha1.DeliveryModeDisabled:
		return configv1alpha1.EffectiveDeliveryModeDisabled,
			reasonPackageDeliveryModeDisabled,
			"spec.packageDelivery.imageMount is Disabled"

	case configv1alpha1.DeliveryModeEnabled:
		supported, _, message := probeImageMountSupport(apiServerVersion, nodes)

		if supported {
			return configv1alpha1.EffectiveDeliveryModeEnabled,
				reasonPackageDeliveryModeEnabled,
				"spec.packageDelivery.imageMount is Enabled; " + message
		}

		return configv1alpha1.EffectiveDeliveryModeEnabled,
			reasonPackageDeliveryModeEnabled,
			"spec.packageDelivery.imageMount is Enabled, but the cluster may not support it: " + message

	default:
		supported, reason, message := probeImageMountSupport(apiServerVersion, nodes)

		if supported {
			return configv1alpha1.EffectiveDeliveryModeEnabled, reason, message
		}

		return configv1alpha1.EffectiveDeliveryModeDisabled, reason, message
	}
}

// resolvePackageDelivery settles how extension packages reach a session and
// publishes the result on status, with a condition explaining why.
//
// A failure to read the cluster is not fatal: the resolution falls back to the
// delivery which works anywhere and says so, rather than blocking the whole
// reconcile over an advisory capability.
func (r *EducatesClusterConfigReconciler) resolvePackageDelivery(
	ctx context.Context,
	obj *configv1alpha1.EducatesClusterConfig,
) {
	mode := configv1alpha1.DeliveryModeAuto

	if obj.Spec.PackageDelivery != nil && obj.Spec.PackageDelivery.ImageMount != "" {
		mode = obj.Spec.PackageDelivery.ImageMount
	}

	apiServerVersion := ""

	var nodes []corev1.Node
	var probeErr error

	// Disabled settles without consulting the cluster at all.
	if mode != configv1alpha1.DeliveryModeDisabled {
		apiServerVersion, nodes, probeErr = r.probeClusterCapability(ctx)

		if probeErr != nil {
			log.FromContext(ctx).Error(
				probeErr, "Unable to check whether the cluster can mount package images",
			)
		}
	}

	effective, reason, message := resolveImageMount(mode, apiServerVersion, nodes)

	// A cluster which could not be read reports why, rather than the
	// downstream complaint about an empty version or an empty node list.
	if probeErr != nil {
		reason = reasonProbeFailed
		message = probeErr.Error()

		if mode == configv1alpha1.DeliveryModeEnabled {
			message = "spec.packageDelivery.imageMount is Enabled, but the cluster could not be checked: " + message
		}
	}

	obj.Status.PackageDelivery = &configv1alpha1.StatusPackageDelivery{
		ImageMount: effective,
	}

	status := metav1.ConditionFalse

	if effective == configv1alpha1.EffectiveDeliveryModeEnabled {
		status = metav1.ConditionTrue
	}

	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionPackageImageMountAvailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

// probeClusterCapability reads the facts the probe needs: the API server
// version and every Linux node's reported kubelet and container runtime.
func (r *EducatesClusterConfigReconciler) probeClusterCapability(
	ctx context.Context,
) (string, []corev1.Node, error) {
	if r.Discovery == nil {
		return "", nil, fmt.Errorf("no discovery client is configured")
	}

	// Held between reconciles: the version changes when the control plane is
	// upgraded, not between reconciles, and the cache keeps a momentary
	// failure to reach the API server from reading as an unsupported cluster.
	apiServerVersion, err := r.apiServerVersion.get(func() (string, error) {
		serverVersion, err := r.Discovery.ServerVersion()

		if err != nil {
			return "", err
		}

		return serverVersion.GitVersion, nil
	})

	if err != nil {
		return "", nil, fmt.Errorf("unable to read the API server version: %w", err)
	}

	// Nodes come from the informer cache, so this is an in-memory read.
	nodes := &corev1.NodeList{}

	if err := r.List(ctx, nodes, client.MatchingLabels{"kubernetes.io/os": "linux"}); err != nil {
		return apiServerVersion, nil, fmt.Errorf("unable to list nodes: %w", err)
	}

	return apiServerVersion, nodes.Items, nil
}
