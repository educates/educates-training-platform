/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkshopPolicyEngine names the engine enforcing per-workshop isolation
// rules. Mirrors the same-named enum in the config API group;
// duplicated to avoid cross-group Go coupling.
// +kubebuilder:validation:Enum=Kyverno;None
type WorkshopPolicyEngine string

const (
	WorkshopPolicyEngineKyverno WorkshopPolicyEngine = "Kyverno"
	WorkshopPolicyEngineNone    WorkshopPolicyEngine = "None"
)

// ThemeSourceType selects how a theme's content is sourced.
// Additional types may be added by the session-manager owner.
// +kubebuilder:validation:Enum=ConfigMap;Secret;URL
type ThemeSourceType string

const (
	ThemeSourceTypeConfigMap ThemeSourceType = "ConfigMap"
	ThemeSourceTypeSecret    ThemeSourceType = "Secret"
	ThemeSourceTypeURL       ThemeSourceType = "URL"
)

// IngressOverrides allows SessionManager to override the cluster-wide
// ingress secrets for the bare-domain hostnames it serves directly
// (TrainingPortal CRs prefix the domain for individual portals).
type IngressOverrides struct {
	// tlsSecretRef optionally names a TLS Secret, holding tls.crt and
	// tls.key, that replaces the cluster's wildcard certificate for
	// session-manager and the training portals and workshop sessions it
	// creates. It takes a name only because session-manager receives
	// this Secret as it is: the operator neither reads nor copies it, so
	// it is looked up in the namespace the cluster's wildcard
	// certificate is published in
	// (EducatesClusterConfig.status.ingress.wildcardCertificateSecretRef),
	// or in session-manager's own namespace when the cluster publishes
	// none. caCertificateSecretRef takes a namespace because the
	// operator reads that Secret to derive a key-free copy.
	// +optional
	TLSSecretRef *LocalObjectReference `json:"tlsSecretRef,omitempty"`

	// caCertificateSecretRef optionally references a CA trust Secret
	// holding, under ca.crt, the CA certificate that replaces the
	// cluster's ingress CA for session-manager and the training portals
	// and workshop sessions it creates. Namespace defaults to the
	// operator namespace when empty. session-manager never receives this
	// Secret: the operator derives the operator-owned
	// educates-session-manager-ingress-ca in the operator namespace from
	// its ca.crt alone, so any other key it holds, a private key
	// included, is ignored.
	// +optional
	CACertificateSecretRef *SecretReference `json:"caCertificateSecretRef,omitempty"`

	// protocol asserts the scheme of the public-facing URLs the
	// session manager and workshops generate. Set to https when TLS
	// is terminated outside the cluster (external load balancer or
	// proxy forwarding plain HTTP inward) so links are generated
	// correctly despite no in-cluster certificate being presented.
	// Empty derives from the TLS configuration: https when a
	// wildcard certificate is configured, http otherwise.
	// +kubebuilder:validation:Enum=http;https
	// +optional
	Protocol string `json:"protocol,omitempty"`
}

// WorkshopPolicyOverride locally overrides
// EducatesClusterConfig.status.policyEnforcement.workshopPolicyEngine
// for this SessionManager. While an effective engine is Kyverno, the
// operator installs session-manager only once Kyverno serves the policy
// kinds Educates creates, reported by the KyvernoAvailable condition.
type WorkshopPolicyOverride struct {
	// +required
	Engine WorkshopPolicyEngine `json:"engine"`
}

// ImageOverride entries replace one chart-default image by short name.
// Mirrors the v3 imageVersions shape: any image the chart's default
// inventory exposes by name can be overridden here (e.g.,
// "training-portal", "base-environment", "jdk17-environment"). Three
// names are special-cased by the reconciler because they live outside
// that inventory: "session-manager" (the chart-pod image),
// "pause-container" (the pre-puller pause image) and
// "node-ca-injector" (its own subchart) — they route to the dedicated
// chart values that control those images.
type ImageOverride struct {
	// name is the image's short name, e.g. "session-manager",
	// "training-portal", "jdk17-environment".
	// +required
	Name string `json:"name"`

	// image is the full reference including tag. Digest-pinned
	// references are not supported for the special-cased names
	// ("session-manager", "pause-container", "node-ca-injector"),
	// whose chart values are repository+tag shaped.
	// +required
	Image string `json:"image"`
}

// Images groups image-related overrides. Registry prefix and pull
// secrets are inherited from
// EducatesClusterConfig.status.imageRegistry; only per-image overrides
// belong here.
type Images struct {
	// +optional
	Overrides []ImageOverride `json:"overrides,omitempty"`
}

// NamespacedObjectReference references an object by name and namespace.
type NamespacedObjectReference struct {
	// +required
	Name string `json:"name"`

	// +required
	Namespace string `json:"namespace"`
}

// ThemeSource sources theme content. Exactly one of the per-type fields
// (secretRef, configMapRef) should be populated for the selected type.
// Secret is the only source the reconciler supports in v1alpha1;
// ConfigMap and URL are reserved and rejected as "not yet supported".
type ThemeSource struct {
	// +required
	Type ThemeSourceType `json:"type"`

	// secretRef applies when type is Secret. It names a Secret holding
	// the theme assets; when its namespace differs from the release
	// namespace the runtime chart auto-creates a SecretCopier for it.
	// +optional
	SecretRef *NamespacedObjectReference `json:"secretRef,omitempty"`

	// configMapRef applies when type is ConfigMap.
	// +optional
	ConfigMapRef *NamespacedObjectReference `json:"configMapRef,omitempty"`
}

// Theme is one named entry in the spec.themes list.
type Theme struct {
	// +required
	Name string `json:"name"`

	// +required
	Source ThemeSource `json:"source"`
}

// TrackingProvider holds a single analytics provider's tracking ID.
type TrackingProvider struct {
	// +required
	TrackingID string `json:"trackingId"`
}

// TrackingWebhook configures an HTTP webhook receiver for analytics
// events.
type TrackingWebhook struct {
	// +required
	URL string `json:"url"`
}

// Tracking groups analytics provider configuration.
type Tracking struct {
	// +optional
	GoogleAnalytics *TrackingProvider `json:"googleAnalytics,omitempty"`

	// +optional
	Amplitude *TrackingProvider `json:"amplitude,omitempty"`

	// +optional
	Clarity *TrackingProvider `json:"clarity,omitempty"`

	// +optional
	Webhook *TrackingWebhook `json:"webhook,omitempty"`
}

// DefaultAccessCredentials configures the default
// username/password used for workshop access when a TrainingPortal
// doesn't override them.
type DefaultAccessCredentials struct {
	// +optional
	Username string `json:"username,omitempty"`

	// passwordSecretRef references a Secret holding the password value.
	// +optional
	PasswordSecretRef *LocalObjectReference `json:"passwordSecretRef,omitempty"`
}

// SessionStorage configures persistent storage characteristics for
// workshop sessions.
type SessionStorage struct {
	// +optional
	StorageClass string `json:"storageClass,omitempty"`

	// storageGroup sets the supplemental GID for mounted volumes.
	// +optional
	StorageGroup *int64 `json:"storageGroup,omitempty"`

	// storageUser sets the UID for mounted volumes.
	// +optional
	StorageUser *int64 `json:"storageUser,omitempty"`
}

// SessionNetwork configures network characteristics applied to workshop
// sessions.
type SessionNetwork struct {
	// packetSize sets the MTU for workshop session networking. Useful
	// on overlay networks where the default MTU is too large.
	// +kubebuilder:validation:Minimum=576
	// +optional
	PacketSize *int32 `json:"packetSize,omitempty"`

	// blockedCidrs lists CIDR ranges workshop sessions are denied
	// network access to (e.g., cloud metadata endpoints).
	// +optional
	BlockedCIDRs []string `json:"blockedCidrs,omitempty"`
}

// ImagePrePuller configures the optional DaemonSet that pre-pulls workshop
// images onto every node ahead of time, so session startup isn't blocked on
// image pulls.
type ImagePrePuller struct {
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

// ComponentMode is a tri-state toggle for optional SessionManager
// extras. Auto derives intent from cluster state (presence of a CA
// Secret, presence of a sibling CR, etc.); Enabled and Disabled
// override that derivation explicitly.
// +kubebuilder:validation:Enum=Auto;Enabled;Disabled
type ComponentMode string

const (
	// ComponentModeAuto evaluates the component's install intent
	// against cluster state at reconcile time. See each component's
	// docs for the exact signal Auto uses.
	ComponentModeAuto ComponentMode = "Auto"

	// ComponentModeEnabled forces installation. If the component has
	// a prerequisite (e.g., nodeCATrust requires a CA Secret in the
	// cluster config status) and that prerequisite is missing, the
	// reconciler refuses with Ready=False and a clear message rather
	// than silently skipping.
	ComponentModeEnabled ComponentMode = "Enabled"

	// ComponentModeDisabled keeps the component uninstalled. If a
	// release exists from a previous Enabled/Auto reconcile, the
	// reconciler drains it.
	ComponentModeDisabled ComponentMode = "Disabled"
)

// NodeCATrust controls installation of the node-ca-injector
// subchart, which writes per-host containerd registry-CA configuration
// on every node so workshop image pulls trust a private/self-signed
// CA. Nodes trust the CA the session manager uses: the key-free copy of
// spec.ingressOverrides.caCertificateSecretRef when it is set, which
// replaces the cluster's CA, otherwise
// `EducatesClusterConfig.status.ingress.caCertificateSecretRef`.
type NodeCATrust struct {
	// mode defaults to Auto. Auto installs the subchart only when a CA
	// is configured, either published by the cluster config or set as
	// spec.ingressOverrides.caCertificateSecretRef; with no CA configured,
	// Auto skips the install silently. Enabled forces the install
	// (refuses if no CA is configured). Disabled keeps it uninstalled.
	// +kubebuilder:default=Auto
	// +optional
	Mode ComponentMode `json:"mode,omitempty"`
}

// RemoteAccess controls installation of the remote-access subchart,
// which renders a read-only ServiceAccount + long-lived token Secret
// + ClusterRole used by external CLIs (e.g., the `educates` CLI
// pointed at a remote cluster) to enumerate training.educates.dev
// resources.
type RemoteAccess struct {
	// mode defaults to Auto. Auto installs the subchart only when a
	// `LookupService` CR exists in the cluster (the signal that
	// cross-cluster federation is being used). Enabled forces the
	// install regardless of LookupService presence. Disabled keeps
	// it uninstalled.
	// +kubebuilder:default=Auto
	// +optional
	Mode ComponentMode `json:"mode,omitempty"`
}

// RegistryMirror declares a registry mirror used by workshop containers.
type RegistryMirror struct {
	// mirror is the upstream registry being mirrored
	// (e.g., "docker.io").
	// +required
	Mirror string `json:"mirror"`

	// url is the mirror endpoint.
	// +required
	URL string `json:"url"`
}

// SessionManagerSpec defines the desired state of SessionManager.
//
// Requires SecretsManager.Ready and EducatesClusterConfig.Ready; both
// dependencies are singletons so no explicit refs are carried.
//
// Image registry prefix and pull secrets are inherited from
// EducatesClusterConfig.status.imageRegistry; only per-image overrides
// land in spec.images.overrides.
type SessionManagerSpec struct {
	// +optional
	IngressOverrides *IngressOverrides `json:"ingressOverrides,omitempty"`

	// +optional
	WorkshopPolicyOverride *WorkshopPolicyOverride `json:"workshopPolicyOverride,omitempty"`

	// +optional
	Images *Images `json:"images,omitempty"`

	// themes is a list of named themes available to TrainingPortals.
	// +optional
	Themes []Theme `json:"themes,omitempty"`

	// defaultTheme names the entry from themes used as the install-wide
	// default. Must match a Theme.name.
	// +optional
	DefaultTheme string `json:"defaultTheme,omitempty"`

	// +optional
	Tracking *Tracking `json:"tracking,omitempty"`

	// +optional
	DefaultAccessCredentials *DefaultAccessCredentials `json:"defaultAccessCredentials,omitempty"`

	// sessionCookieDomain sets the cookie domain used by workshop
	// sessions for cross-subdomain authentication.
	// +optional
	SessionCookieDomain string `json:"sessionCookieDomain,omitempty"`

	// allowedEmbeddingHosts lists hosts allowed to embed Educates
	// workshop frames (CSP frame-ancestors).
	// +optional
	AllowedEmbeddingHosts []string `json:"allowedEmbeddingHosts,omitempty"`

	// +optional
	Storage *SessionStorage `json:"storage,omitempty"`

	// +optional
	Network *SessionNetwork `json:"network,omitempty"`

	// +optional
	ImagePrePuller *ImagePrePuller `json:"imagePrePuller,omitempty"`

	// registryMirrors configures per-registry mirrors for workshop
	// container pulls.
	// +optional
	RegistryMirrors []RegistryMirror `json:"registryMirrors,omitempty"`

	// logLevel defaults to info.
	// +kubebuilder:default=info
	// +optional
	LogLevel LogLevel `json:"logLevel,omitempty"`

	// nodeCATrust controls the optional node-ca-injector install.
	// +optional
	NodeCATrust *NodeCATrust `json:"nodeCATrust,omitempty"`

	// remoteAccess controls the optional remote-access install.
	// +optional
	RemoteAccess *RemoteAccess `json:"remoteAccess,omitempty"`
}

// SessionManagerStatus defines the observed state of SessionManager.
// Publishes the full status contract: phase + conditions +
// installedVersion + deploymentRef.
type SessionManagerStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Phase ComponentPhase `json:"phase,omitempty"`

	// conditions report the resource's state:
	//   - Ready                    (aggregate)
	//   - ClusterConfigAvailable   (EducatesClusterConfig.Ready gate)
	//   - SecretsManagerAvailable  (SecretsManager.Ready gate)
	//   - Deployed                 (session-manager helm release + Deployment Available)
	//   - NodeCATrustDeployed      (optional extra; reflects mode evaluation outcome)
	//   - RemoteAccessDeployed     (optional extra; reflects mode evaluation outcome)
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// installedVersion records the session-manager chart version most
	// recently applied.
	// +optional
	InstalledVersion string `json:"installedVersion,omitempty"`

	// deploymentRef names the upstream session-manager Deployment the
	// operator is gating Ready on.
	// +optional
	DeploymentRef *NamespacedRef `json:"deploymentRef,omitempty"`
}

// SessionManager is the singleton resource that drives installation of
// the session-manager component (with training-portal,
// assets-server, image-cache, and supporting services).
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="SessionManager must be named 'cluster' (singleton per cluster)"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type SessionManager struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SessionManagerSpec `json:"spec"`

	// +optional
	Status SessionManagerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SessionManagerList contains a list of SessionManager.
type SessionManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SessionManager `json:"items"`
}

func init() {
	register(&SessionManager{}, &SessionManagerList{})
}
