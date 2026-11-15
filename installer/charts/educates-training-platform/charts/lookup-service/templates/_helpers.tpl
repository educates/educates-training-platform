{{- define "lookup-service.labels" -}}
app.kubernetes.io/name: lookup-service
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: lookup-service
app.kubernetes.io/part-of: educates
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "lookup-service.selectorLabels" -}}
app: lookup-service
{{- end -}}

{{/*
The namespace lookup-service reads its configuration from: its
ClusterConfig, ClientConfig and TenantConfig resources and the kubeconfig
Secrets they reference. lookup-service watches `educates-config` unless its
OPERATOR_NAMESPACE environment variable names another, which the chart does
not set.
*/}}
{{- define "lookup-service.configNamespace" -}}
educates-config
{{- end -}}

{{/*
Resolve the effective imageRegistry for chart-rendered refs. Reads the
development override (subchart-local + umbrella global, deep-merged) and
falls back per-leaf to Chart.yaml annotations
(`educates.dev/image-registry-host` / `-namespace`) when empty. The
annotations are publish-time defaults updated by the release workflow.
*/}}
{{- define "lookup-service.resolvedImageRegistry" -}}
{{- $local := default dict (default dict .Values.development).imageRegistry -}}
{{- $global := default dict (default dict (default dict .Values.global).development).imageRegistry -}}
{{- $merged := mergeOverwrite (deepCopy $local) $global -}}
{{- if not $merged.host -}}
  {{- $_ := set $merged "host" (index .Chart.Annotations "educates.dev/image-registry-host" | default "") -}}
{{- end -}}
{{- if not $merged.namespace -}}
  {{- $_ := set $merged "namespace" (index .Chart.Annotations "educates.dev/image-registry-namespace" | default "") -}}
{{- end -}}
{{- toYaml $merged -}}
{{- end -}}

{{- define "lookup-service.imageRegistryPrefix" -}}
{{- $ir := include "lookup-service.resolvedImageRegistry" . | fromYaml -}}
{{- $host := default "" $ir.host -}}
{{- $ns := default "" $ir.namespace -}}
{{- if and $host $ns -}}
{{ $host }}/{{ $ns }}
{{- else if $host -}}
{{ $host }}
{{- else -}}
{{- fail "imageRegistry.host could not be resolved. Either set Chart.yaml annotation `educates.dev/image-registry-host` (publish-time default) or override locally via .development.imageRegistry / .global.development.imageRegistry." -}}
{{- end -}}
{{- end -}}

{{- define "lookup-service.image.repository" -}}
{{- if .Values.image.repository -}}
{{ .Values.image.repository }}
{{- else -}}
{{ include "lookup-service.imageRegistryPrefix" . }}/educates-lookup-service
{{- end -}}
{{- end -}}

{{- define "lookup-service.image.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{- define "lookup-service.image.pullPolicy" -}}
{{- if .Values.image.pullPolicy -}}
{{ .Values.image.pullPolicy }}
{{- else -}}
{{- $tag := include "lookup-service.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Resolve the clusterIngress block by deep-merging the umbrella's
`global.clusterIngress` over the subchart's local `clusterIngress`. Globals
win where set; subchart-local defaults pass through otherwise.
*/}}
{{- define "lookup-service.resolvedClusterIngress" -}}
{{- $local := default dict .Values.clusterIngress -}}
{{- $global := default dict (default dict .Values.global).clusterIngress -}}
{{- toYaml (mergeOverwrite (deepCopy $local) $global) -}}
{{- end -}}

{{/*
True when the resolved clusterIngress.caCertificateRef.name is set — drives
mounting of the CA Secret at /opt/app-root/certs/ingress-ca in the
lookup-service Deployment. The image's start script appends that CA to the
system bundle at /opt/app-root/ca-bundle.crt, which SSL_CERT_FILE,
REQUESTS_CA_BUNDLE and related variables in the image point at, so no init
container and no root access are needed.

Required when lookup-service must verify TLS against a private/self-signed
CA when calling other clusters' training portals.
*/}}
{{- define "lookup-service.caTrustEnabled" -}}
{{- $ci := include "lookup-service.resolvedClusterIngress" . | fromYaml -}}
{{- $caRef := default dict $ci.caCertificateRef -}}
{{- if $caRef.name }}true{{- end -}}
{{- end -}}
