{{/*
Expand the name of the chart.
*/}}
{{- define "educates.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "educates.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Resolve image for a component by name. Uses imageVersions override if present, otherwise registry/educates-<name>:<version>.
*/}}
{{- define "educates.image" -}}
{{- $name := .name -}}
{{- $registry := .context.Values.imageRegistry.host | default "registry.default.svc.cluster.local" -}}
{{- if .context.Values.imageRegistry.namespace -}}
{{- $registry = printf "%s/%s" $registry .context.Values.imageRegistry.namespace -}}
{{- end -}}
{{- $version := .context.Values.version | default "latest" -}}
{{- $image := printf "%s/educates-%s:%s" $registry $name $version -}}
{{- range .context.Values.imageVersions -}}
{{- if eq .name $name -}}
{{- $image = .image -}}
{{- end -}}
{{- end -}}
{{- $image -}}
{{- end }}

{{/*
Image pull policy: Always for latest/main/master/develop, else IfNotPresent.
*/}}
{{- define "educates.imagePullPolicy" -}}
{{- $img := . -}}
{{- if or (not (contains ":" $img)) (or (hasSuffix ":latest" $img) (hasSuffix ":main" $img) (hasSuffix ":master" $img) (hasSuffix ":develop" $img)) -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end }}

{{/*
Operator namespace (hardcoded).
*/}}
{{- define "educates.operator.namespace" -}}
educates
{{- end }}

{{/*
Extract list of image pull secret names from clusterSecrets.pullSecretRefs.
Returns list of secret names.
*/}}
{{- define "educates.imagePullSecretNames" -}}
{{- $names := list -}}
{{- range .Values.clusterSecrets.pullSecretRefs -}}
{{- $names = append $names .name -}}
{{- end -}}
{{- $names | toJson -}}
{{- end }}

{{/*
Filter image pull secrets that are in external namespaces (not operator namespace).
Returns list of refs with namespace and name.
*/}}
{{- define "educates.externalImagePullSecrets" -}}
{{- $opNs := include "educates.operator.namespace" . -}}
{{- $external := list -}}
{{- range .Values.clusterSecrets.pullSecretRefs -}}
{{- if and .namespace (ne .namespace $opNs) -}}
{{- $external = append $external . -}}
{{- end -}}
{{- end -}}
{{- $external | toJson -}}
{{- end }}

{{/*
Filter theme data refs that are in external namespaces (not operator namespace).
Returns list of refs with namespace and name.
*/}}
{{- define "educates.externalThemeDataRefs" -}}
{{- $opNs := include "educates.operator.namespace" . -}}
{{- $external := list -}}
{{- range .Values.websiteStyling.themeDataRefs -}}
{{- if and .namespace (ne .namespace $opNs) -}}
{{- $external = append $external . -}}
{{- end -}}
{{- end -}}
{{- $external | toJson -}}
{{- end }}

