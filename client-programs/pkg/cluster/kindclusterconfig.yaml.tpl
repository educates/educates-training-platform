kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
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
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
{{- if eq .ClusterSecurity.PolicyEngine "pod-security-standards" }}
featureGates:
  PodSecurity: true
{{ end }}
