kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
{{- if or .LocalKindCluster.ApiServer .LocalKindCluster.Networking }}
networking:
  {{- if .LocalKindCluster.ApiServer.Address }}
  # WARNING: It is _strongly_ recommended that you keep this the default
  # (127.0.0.1) for security reasons. However it is possible to change this.
  apiServerAddress: "{{ .LocalKindCluster.ApiServer.Address }}"
  {{- end }}
  {{- if .LocalKindCluster.ApiServer.Port }}
  # By default the API server listens on a random open port.
  # You may choose a specific port but probably don't need to in most cases.
  # Using a random port makes it easier to spin up multiple clusters.
  apiServerPort: {{ .LocalKindCluster.ApiServer.Port }}
  {{- end }}
  {{- if .LocalKindCluster.Networking.ServiceSubnet }}
  serviceSubnet: "{{ .LocalKindCluster.Networking.ServiceSubnet }}"
  {{- end }}
  {{- if .LocalKindCluster.Networking.PodSubnet }}
  podSubnet: "{{ .LocalKindCluster.Networking.PodSubnet }}"
  {{- end }}
{{- end }}
nodes:
{{- if .LocalKindCluster.Nodes }}
{{- range .LocalKindCluster.Nodes }}
- role: {{ .Role }}
{{- if or .Labels .Taints (eq .Role "control-plane") }}
  kubeadmConfigPatches:
{{- if eq .Role "control-plane" }}
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
{{- if .Labels }}
        node-labels: "{{- range $key, $value := .Labels }}{{ $key }}={{ $value }},{{- end }}ingress-ready=true"
{{- else }}
        node-labels: "ingress-ready=true"
{{- end }}
{{- else }}
  - |
    kind: JoinConfiguration
    nodeRegistration:
      kubeletExtraArgs:
{{- if .Labels }}
        node-labels: "{{- $first := true }}{{- range $key, $value := .Labels }}{{- if not $first }},{{- end }}{{ $key }}={{ $value }}{{- $first = false }}{{- end }}"
{{- end }}
{{- if .Taints }}
        register-with-taints: "{{- range $i, $taint := .Taints }}{{- if $i }},{{- end }}{{ $taint.Key }}={{- if $taint.Value }}{{ $taint.Value }}{{- end }}:{{ $taint.Effect }}{{- end }}"
{{- end }}
{{- end }}
{{- end }}
{{- if eq .Role "control-plane" }}
  extraPortMappings:
  - containerPort: 80
{{- if $.LocalKindCluster.ListenAddress }}
    listenAddress: {{ $.LocalKindCluster.ListenAddress }}
{{- end }}
    hostPort: 80
    protocol: TCP
  - containerPort: 443
{{- if $.LocalKindCluster.ListenAddress }}
    listenAddress: {{ $.LocalKindCluster.ListenAddress }}
{{- end }}
    hostPort: 443
    protocol: TCP
{{- if $.LocalKindCluster.VolumeMounts }}
  extraMounts:
{{- range $.LocalKindCluster.VolumeMounts }}
  - hostPath: {{ .HostPath }}
    containerPath: {{ .ContainerPath }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- else }}
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  {{- if eq .ClusterSecurity.PolicyEngine "pod-security-policies" }}
  - |
    kind: ClusterConfiguration
    metadata:
      name: config
    apiServer:
      extraArgs:
        enable-admission-plugins: PodSecurityPolicy
  {{- end }}
  extraPortMappings:
  - containerPort: 80
{{- if .LocalKindCluster.ListenAddress }}
    listenAddress: {{ .LocalKindCluster.ListenAddress }}
{{- end }}
    hostPort: 80
    protocol: TCP
  - containerPort: 443
{{- if .LocalKindCluster.ListenAddress }}
    listenAddress: {{ .LocalKindCluster.ListenAddress }}
{{- end }}
    hostPort: 443
    protocol: TCP
{{- if .LocalKindCluster.VolumeMounts }}
  extraMounts:
{{- range .LocalKindCluster.VolumeMounts }}
  - hostPath: {{ .HostPath }}
    containerPath: {{ .ContainerPath }}
{{- end }}
{{- end }}
{{- end }}
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
{{- if eq .ClusterSecurity.PolicyEngine "pod-security-standards" }}
featureGates:
  PodSecurity: true
{{ end }}
