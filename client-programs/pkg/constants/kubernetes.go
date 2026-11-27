package constants

import "sort"

// OperatorNamespace is the namespace of the educates-installer chart's
// release, the operator namespace. It is also where the CLI keeps the
// Secrets of its local cache in a cluster, since the operator pod pulls
// its own image with Secrets that must be there before the chart is
// installed, and the EducatesClusterConfig names its Secrets there.
const OperatorNamespace = "educates-installer"

// KubernetesVersionToKindImage maps a supported Kubernetes minor version to
// the kind node image (with digest) published for the kind release this CLI
// vendors (sigs.k8s.io/kind v0.33.0). The supported set is kept in sync with
// the kubectl versions shipped in the workshop base environment and with
// go.mod's k8s.io/* — see the educates-upgrade-kubernetes protocol.
var KubernetesVersionToKindImage = map[string]string{
	"1.37": "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5",
	"1.36": "kindest/node:v1.36.4@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed",
	"1.35": "kindest/node:v1.35.8@sha256:07b2536e30b803ed61d1677a79df6115f798ce64c80f9e22f6ed45afd09323c0",
	"1.34": "kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d",
}

// DefaultKubernetesVersion is the version 'local cluster create' uses when
// --kubernetes-version is not given. It deliberately tracks the second-newest
// supported version, trading recency for stability.
const DefaultKubernetesVersion = "1.36"

// SupportedKubernetesVersions returns the supported Kubernetes minor
// versions, sorted, for use in flag help and validation errors.
func SupportedKubernetesVersions() []string {
	versions := make([]string, 0, len(KubernetesVersionToKindImage))
	for v := range KubernetesVersionToKindImage {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions
}
