{{- define "session-manager.labels" -}}
app.kubernetes.io/name: session-manager
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: session-manager
app.kubernetes.io/part-of: educates
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "session-manager.selectorLabels" -}}
deployment: session-manager
{{- end -}}

{{/*
Resolve a cross-cutting values block (clusterIngress, clusterSecurity) by
deep-merging the umbrella's `global:` over the subchart's local block.
Globals win where set; subchart-local defaults pass through otherwise.
Returned as a YAML string — consume via `fromYaml`.
*/}}

{{/*
Resolve the development imageRegistry override. Reads BOTH `.development.
imageRegistry` (subchart-local) and `.global.development.imageRegistry`
(umbrella global), with global winning per-leaf. Returns the user's intent
verbatim — no Chart.yaml annotation fallback. Consumers that need the
"effective" registry for chart-rendered refs should use
`session-manager.resolvedImageRegistry` instead, which falls back to
annotations.

This raw form is what gets emitted into the operator-config blob's
`imageRegistry` field — empty in normal use, so the runtime falls back to
`registry.default.svc.cluster.local` for `$(image_repository)` workshop
content placeholder resolution.
*/}}
{{- define "session-manager.resolvedDevelopmentImageRegistry" -}}
{{- $local := default dict (default dict .Values.development).imageRegistry -}}
{{- $global := default dict (default dict (default dict .Values.global).development).imageRegistry -}}
{{- toYaml (mergeOverwrite (deepCopy $local) $global) -}}
{{- end -}}

{{/*
Resolve the effective imageRegistry for chart-rendered refs (chart pod
image, pause image, Educates-published entries in the imageVersions
helper). Layered resolution:
  1. `.development.imageRegistry` (subchart-local) and
     `.global.development.imageRegistry` (umbrella global), deep-merged
     with global winning.
  2. Per-leaf fallback to Chart.yaml annotations
     (`educates.dev/image-registry-host` / `-namespace`) when the merged
     value is empty. Annotations are the publish-time default — the
     release workflow updates them per fork.
*/}}
{{- define "session-manager.resolvedImageRegistry" -}}
{{- $merged := include "session-manager.resolvedDevelopmentImageRegistry" . | fromYaml -}}
{{- if not $merged.host -}}
  {{- $_ := set $merged "host" (index .Chart.Annotations "educates.dev/image-registry-host" | default "") -}}
{{- end -}}
{{- if not $merged.namespace -}}
  {{- $_ := set $merged "namespace" (index .Chart.Annotations "educates.dev/image-registry-namespace" | default "") -}}
{{- end -}}
{{- toYaml $merged -}}
{{- end -}}

{{- define "session-manager.resolvedClusterIngress" -}}
{{- $local := default dict .Values.clusterIngress -}}
{{- $global := default dict (default dict .Values.global).clusterIngress -}}
{{- toYaml (mergeOverwrite (deepCopy $local) $global) -}}
{{- end -}}

{{- define "session-manager.resolvedClusterSecurity" -}}
{{- $local := default dict .Values.clusterSecurity -}}
{{- $global := default dict (default dict .Values.global).clusterSecurity -}}
{{- toYaml (mergeOverwrite (deepCopy $local) $global) -}}
{{- end -}}

{{/*
Compose the registry+namespace prefix from the resolved imageRegistry.
Two forms:
  host + namespace -> "{host}/{namespace}"
  host alone       -> "{host}"
Used as the default prefix for the chart-pod image, the pause image, and the
Educates-published entries in the `imageVersions` helper. A user that points
imageRegistry at a fork or a local registry redirects all three at once.
*/}}
{{- define "session-manager.imageRegistryPrefix" -}}
{{- $ir := include "session-manager.resolvedImageRegistry" . | fromYaml -}}
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

{{- define "session-manager.image.repository" -}}
{{- if .Values.image.repository -}}
{{ .Values.image.repository }}
{{- else -}}
{{ include "session-manager.imageRegistryPrefix" . }}/educates-session-manager
{{- end -}}
{{- end -}}

{{- define "session-manager.image.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{- define "session-manager.image.pullPolicy" -}}
{{- if .Values.image.pullPolicy -}}
{{ .Values.image.pullPolicy }}
{{- else -}}
{{- $tag := include "session-manager.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
True when the resolved clusterIngress.caCertificateRef.name is set — drives
rendering of the chart-side ca-trust-store init container in the
session-manager Deployment. The init container reuses the main session-manager
image (Fedora-based: has `update-ca-trust` and `tar`), so no extra image pull
on the node. Mirrors the runtime-side overlay session-manager already applies
to spawned pods (workshopsession.py) and v3's overlay-ca-injector.yaml.
*/}}
{{- define "session-manager.caTrustEnabled" -}}
{{- $ci := include "session-manager.resolvedClusterIngress" . | fromYaml -}}
{{- $caRef := default dict $ci.caCertificateRef -}}
{{- if $caRef.name }}true{{- end -}}
{{- end -}}

{{/*
Image references the image-puller DaemonSet pre-pulls, as a YAML array.
An explicit imagePrePuller.images wins verbatim. When empty, default to
the v3-equivalent set — training-portal + base-environment — resolved
through the imageVersions inventory so registry relocation
(development.imageRegistry) and per-name imageVersions overrides are
honoured. Mirrors v3's image-puller DaemonSet, which always pre-pulled
training-portal plus a prePullImages list defaulting to
["base-environment"].
*/}}
{{- define "session-manager.prePullImages" -}}
{{- if .Values.imagePrePuller.images -}}
{{ toYaml .Values.imagePrePuller.images }}
{{- else -}}
{{- $inventory := include "session-manager.imageVersions" . | fromYamlArray -}}
{{- $defaults := list -}}
{{- range $inventory -}}
{{- if or (eq .name "training-portal") (eq .name "base-environment") -}}
{{- $defaults = append $defaults .image -}}
{{- end -}}
{{- end -}}
{{ toYaml $defaults }}
{{- end -}}
{{- end -}}

{{- define "session-manager.pause.image.repository" -}}
{{- if .Values.imagePrePuller.pauseImage.repository -}}
{{ .Values.imagePrePuller.pauseImage.repository }}
{{- else -}}
{{ include "session-manager.imageRegistryPrefix" . }}/educates-pause-container
{{- end -}}
{{- end -}}

{{- define "session-manager.pause.image.tag" -}}
{{- default .Chart.AppVersion .Values.imagePrePuller.pauseImage.tag -}}
{{- end -}}

{{- define "session-manager.pause.image.pullPolicy" -}}
{{- if .Values.imagePrePuller.pauseImage.pullPolicy -}}
{{ .Values.imagePrePuller.pauseImage.pullPolicy }}
{{- else -}}
{{- $tag := include "session-manager.pause.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Auto-derive imagePullPolicy for an arbitrary fully-qualified image ref
passed in as a string.
*/}}
{{- define "session-manager.derivedPullPolicy" -}}
{{- $parts := splitList ":" . -}}
{{- $tag := "" -}}
{{- if eq (len $parts) 1 -}}
  {{- $tag = "" -}}
{{- else -}}
  {{- $tag = last $parts -}}
{{- end -}}
{{- if or (eq $tag "") (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}

{{/*
Derive ingress protocol from the resolved clusterIngress. `protocol` wins
when set; otherwise "https" if a tlsCertificateRef.name is provided, "http"
otherwise. Mirrors operator_config.py's own derivation.
*/}}
{{- define "session-manager.derivedProtocol" -}}
{{- $ci := include "session-manager.resolvedClusterIngress" . | fromYaml -}}
{{- $tlsRef := default dict $ci.tlsCertificateRef -}}
{{- if $ci.protocol -}}
{{- $ci.protocol -}}
{{- else if $tlsRef.name -}}
https
{{- else -}}
http
{{- end -}}
{{- end -}}

{{/*
Build the YAML stream for the `kyverno-policies.yaml` key of the
`educates-config` Secret. session-manager reads the result via
`yaml.load_all` and creates a per-workshop-environment copy of each policy,
scoped to the environment's session namespaces (see handlers/kyverno_rules.py).
Concatenates:
  1. Bundled workshop policies under files/kyverno-policies/workshop-policies/
     (when workshopSecurity.rulesEngine == "Kyverno"). These are now
     `ValidatingPolicy` resources (policies.kyverno.io).
  2. User-supplied policies from workshopSecurity.additionalKyvernoPolicies
     (also gated on Kyverno). A new Policy type (ValidatingPolicy, ...) is
     scoped natively; a legacy ClusterPolicy is still scoped but deprecated
     (the runtime logs a warning), tracking Kyverno's own ClusterPolicy
     removal in 1.20.
Each document is separated by `---\n`.
*/}}
{{- define "session-manager.kyvernoPoliciesContent" -}}
{{- $ws := .Values.workshopSecurity -}}
{{- $output := "" -}}
{{- if eq $ws.rulesEngine "Kyverno" -}}
  {{- range $path, $_ := .Files.Glob "files/kyverno-policies/workshop-policies/*.yaml" -}}
    {{- $content := $.Files.Get $path | trim -}}
    {{- $output = printf "%s---\n%s\n" $output $content -}}
  {{- end -}}
  {{- range default list $ws.additionalKyvernoPolicies -}}
    {{- $content := toYaml . | trim -}}
    {{- $output = printf "%s---\n%s\n" $output $content -}}
  {{- end -}}
{{- end -}}
{{- $output -}}
{{- end -}}

{{/*
Default `imageVersions` set the chart ships, mirroring v3's
carvel-installer `images.yaml`. Two kinds of entries:

  - Educates-published images. Repository prefix derived from
    `imageRegistry.{host,namespace}` so a fork or local registry
    redirects them in one knob; tag derived from `.Chart.AppVersion`
    so a chart release that bumps the runtime moves these in lock-
    step.
  - Upstream pins (docker-in-docker, loftsh-*, debian-base) that
    aren't Educates-published. Hard-coded to specific upstream refs;
    `imageRegistry` does NOT relocate them — override the matching
    `imageVersions` entry directly when mirroring.

User overrides in `.Values.imageVersions` are merged in BY NAME — each
user entry replaces the default with the same `name`, but other defaults
pass through untouched. Names not in this default list are appended,
which preserves forward-compat with new runtime images.

Returns the merged list as a YAML array string (consume via fromYamlArray).
*/}}
{{- define "session-manager.imageVersions" -}}
{{- $repo := include "session-manager.imageRegistryPrefix" . -}}
{{- $v := .Chart.AppVersion -}}
{{- $defaults := list
    (dict "name" "training-portal"         "image" (printf "%s/educates-training-portal:%s" $repo $v))
    (dict "name" "docker-registry"         "image" (printf "%s/educates-docker-registry:%s" $repo $v))
    (dict "name" "tunnel-manager"          "image" (printf "%s/educates-tunnel-manager:%s" $repo $v))
    (dict "name" "image-cache"             "image" (printf "%s/educates-image-cache:%s" $repo $v))
    (dict "name" "assets-server"           "image" (printf "%s/educates-assets-server:%s" $repo $v))
    (dict "name" "base-environment"        "image" (printf "%s/educates-base-environment:%s" $repo $v))
    (dict "name" "jdk8-environment"        "image" (printf "%s/educates-jdk8-environment:%s" $repo $v))
    (dict "name" "jdk11-environment"       "image" (printf "%s/educates-jdk11-environment:%s" $repo $v))
    (dict "name" "jdk17-environment"       "image" (printf "%s/educates-jdk17-environment:%s" $repo $v))
    (dict "name" "jdk21-environment"       "image" (printf "%s/educates-jdk21-environment:%s" $repo $v))
    (dict "name" "conda-environment"       "image" (printf "%s/educates-conda-environment:%s" $repo $v))
    (dict "name" "debian-base-image"       "image" "debian:sid-20230502-slim")
    (dict "name" "docker-in-docker"        "image" "docker:27.5.1-dind")
    (dict "name" "loftsh-kubernetes-v1.33" "image" "ghcr.io/loft-sh/kubernetes:v1.33.4")
    (dict "name" "loftsh-kubernetes-v1.34" "image" "ghcr.io/loft-sh/kubernetes:v1.34.0")
    (dict "name" "loftsh-kubernetes-v1.35" "image" "ghcr.io/loft-sh/kubernetes:v1.35.0")
    (dict "name" "loftsh-kubernetes-v1.36" "image" "ghcr.io/loft-sh/kubernetes:v1.36.0")
    (dict "name" "loftsh-vcluster"         "image" "ghcr.io/loft-sh/vcluster-oss:0.35.2")
    (dict "name" "vcluster-internal-contour" "image" "ghcr.io/projectcontour/contour:v1.30.2")
    (dict "name" "vcluster-internal-envoy"   "image" "docker.io/envoyproxy/envoy:v1.31.5")
-}}
{{- $overrides := dict -}}
{{- range default list .Values.imageVersions -}}
  {{- $_ := set $overrides .name .image -}}
{{- end -}}
{{- $merged := list -}}
{{- $defaultNames := dict -}}
{{- range $defaults -}}
  {{- $name := .name -}}
  {{- $image := .image -}}
  {{- if hasKey $overrides $name -}}
    {{- $image = index $overrides $name -}}
  {{- end -}}
  {{- $merged = append $merged (dict "name" $name "image" $image) -}}
  {{- $_ := set $defaultNames $name true -}}
{{- end -}}
{{- range default list .Values.imageVersions -}}
  {{- if not (hasKey $defaultNames .name) -}}
    {{- $merged = append $merged (dict "name" .name "image" .image) -}}
  {{- end -}}
{{- end -}}
{{- toYaml $merged -}}
{{- end -}}

{{/*
Resolve training-portal credentials with stability across `helm upgrade` and
session-manager pod restarts. Priority for each field:

  1. User-supplied non-empty value in .Values.trainingPortal.{credentials,
     clients}. Explicit operator intent always wins.
  2. Previously-persisted value, read back from the live `educates-config`
     Secret if present. This is what keeps generated credentials stable
     across upgrades and restarts.
  3. Generated defaults: fixed usernames ("educates", "robot@educates");
     32-char randAlphaNum for passwords and OAuth client id/secret.

The runtime's xget() helper falls back to its own defaults only when keys are
*absent* — not when they're set to empty strings. The chart previously
emitted `username: ""` / `password: ""` keys unconditionally, which made the
runtime use "" verbatim and broke training-portal initialisation. This helper
ensures non-empty values land in the rendered config.

The lookup-based reuse mirrors the v3 carvel installer's "ytt-generated-once
at install time" semantics. `helm lookup` returns nil during `helm template`
(no cluster connection); in that case the generated branch produces fresh
values, which is the expected behaviour for offline rendering.
*/}}
{{- define "session-manager.resolvedTrainingPortal" -}}
{{- $tp := default dict .Values.trainingPortal -}}
{{- $cur := dict -}}
{{- $existing := lookup "v1" "Secret" .Release.Namespace "educates-config" -}}
{{- if $existing -}}
  {{- $raw := index (default dict $existing.data) "educates-operator-config.yaml" | default "" -}}
  {{- if $raw -}}
    {{- $cfg := $raw | b64dec | fromYaml -}}
    {{- $cur = default dict (dig "trainingPortal" dict $cfg) -}}
  {{- end -}}
{{- end -}}
{{- $adminUsername := dig "credentials" "admin" "username" "" $tp -}}
{{- if eq $adminUsername "" -}}{{- $adminUsername = dig "credentials" "admin" "username" "" $cur -}}{{- end -}}
{{- if eq $adminUsername "" -}}{{- $adminUsername = "educates" -}}{{- end -}}
{{- $adminPassword := dig "credentials" "admin" "password" "" $tp -}}
{{- if eq $adminPassword "" -}}{{- $adminPassword = dig "credentials" "admin" "password" "" $cur -}}{{- end -}}
{{- if eq $adminPassword "" -}}{{- $adminPassword = randAlphaNum 32 -}}{{- end -}}
{{- $robotUsername := dig "credentials" "robot" "username" "" $tp -}}
{{- if eq $robotUsername "" -}}{{- $robotUsername = dig "credentials" "robot" "username" "" $cur -}}{{- end -}}
{{- if eq $robotUsername "" -}}{{- $robotUsername = "robot@educates" -}}{{- end -}}
{{- $robotPassword := dig "credentials" "robot" "password" "" $tp -}}
{{- if eq $robotPassword "" -}}{{- $robotPassword = dig "credentials" "robot" "password" "" $cur -}}{{- end -}}
{{- if eq $robotPassword "" -}}{{- $robotPassword = randAlphaNum 32 -}}{{- end -}}
{{- $robotClientId := dig "clients" "robot" "id" "" $tp -}}
{{- if eq $robotClientId "" -}}{{- $robotClientId = dig "clients" "robot" "id" "" $cur -}}{{- end -}}
{{- if eq $robotClientId "" -}}{{- $robotClientId = randAlphaNum 32 -}}{{- end -}}
{{- $robotClientSecret := dig "clients" "robot" "secret" "" $tp -}}
{{- if eq $robotClientSecret "" -}}{{- $robotClientSecret = dig "clients" "robot" "secret" "" $cur -}}{{- end -}}
{{- if eq $robotClientSecret "" -}}{{- $robotClientSecret = randAlphaNum 32 -}}{{- end -}}
{{- $out := dict
  "credentials" (dict
    "admin" (dict "username" $adminUsername "password" $adminPassword)
    "robot" (dict "username" $robotUsername "password" $robotPassword)
  )
  "clients" (dict
    "robot" (dict "id" $robotClientId "secret" $robotClientSecret)
  )
-}}
{{- toYaml $out -}}
{{- end -}}

{{/*
Compose the `educates-operator-config.yaml` Secret content from typed values.
Auto-injects `operator.namespace` (release ns) and `version` (.Chart.AppVersion
— the bundled Educates runtime version, distinct from the chart-package
`Chart.Version`). Lowercases policy/rules engine names to match the runtime's
expected casing. Materialises empty-string TLS/CA refs explicitly — the runtime
reads these via xget() with no default, so absent keys become Python None and
crash later when encoded as strings (see project_runtime_config_quirks memory).
Deep-merges .Values.config on top so the escape hatch wins on conflict.
*/}}
{{- define "session-manager.operatorConfigYAML" -}}
{{- $ci := include "session-manager.resolvedClusterIngress" . | fromYaml -}}
{{- if not $ci.domain -}}
{{- fail "clusterIngress.domain is required (set globally under .global.clusterIngress.domain or locally under session-manager.clusterIngress.domain)" -}}
{{- end -}}
{{- $tlsRef := default dict $ci.tlsCertificateRef -}}
{{- $caRef := default dict $ci.caCertificateRef -}}
{{- $cs := include "session-manager.resolvedClusterSecurity" . | fromYaml -}}
{{/*
The runtime expects the policy-engine identifiers the handlers compare
against, which are not a plain lowercasing of the chart enum:
OpenShiftSCC selects the `security-context-constraints` code path and
PodSecurityStandards the `pod-security-standards` one. Map explicitly
so those branches engage.
*/}}
{{- $policyEngineNames := dict
  "Kyverno" "kyverno"
  "PodSecurityStandards" "pod-security-standards"
  "OpenShiftSCC" "security-context-constraints"
  "None" "none"
-}}
{{- $policyEngine := get $policyEngineNames $cs.policyEngine | default (lower $cs.policyEngine) -}}
{{- $ws := .Values.workshopSecurity -}}
{{/*
Emit the user-supplied development.imageRegistry verbatim into the runtime
config — NOT the annotation-resolved one. Empty in normal use means the
runtime falls back to `registry.default.svc.cluster.local` for the
`$(image_repository)` workshop-content placeholder. The chart's own
imageVersions helper handles all Educates runtime images explicitly with
fully-qualified refs, so emitting empty here doesn't break runtime image
resolution.
*/}}
{{- $ir := include "session-manager.resolvedDevelopmentImageRegistry" . | fromYaml -}}
{{- $tp := default dict .Values.trainingPortal -}}
{{- $sc := default dict .Values.sessionCookies -}}
{{- $cstg := default dict .Values.clusterStorage -}}
{{- $crt := default dict .Values.clusterRuntime -}}
{{- $cnet := default dict .Values.clusterNetwork -}}
{{- $dd := default dict .Values.dockerDaemon -}}
{{- $proxy := default dict $dd.proxyCache -}}
{{- $wa := default dict .Values.workshopAnalytics -}}
{{- $wstyle := default dict .Values.websiteStyling -}}
{{- $typed := dict
  "operator" (dict "namespace" .Release.Namespace)
  "version" .Chart.AppVersion
  "clusterIngress" (dict
    "domain" $ci.domain
    "class" (default "" $ci.class)
    "protocol" (include "session-manager.derivedProtocol" .)
    "tlsCertificateRef" (dict
      "name" (default "" $tlsRef.name)
      "namespace" (default "" $tlsRef.namespace)
    )
    "caCertificateRef" (dict
      "name" (default "" $caRef.name)
      "namespace" (default "" $caRef.namespace)
    )
  )
  "clusterSecurity" (dict "policyEngine" $policyEngine)
  "workshopSecurity" (dict "rulesEngine" (lower $ws.rulesEngine))
  "imageRegistry" (dict
    "host" (default "" $ir.host)
    "namespace" (default "" $ir.namespace)
  )
  "imageVersions" (include "session-manager.imageVersions" . | fromYamlArray)
  "trainingPortal" (include "session-manager.resolvedTrainingPortal" . | fromYaml)
  "sessionCookies" (dict "domain" (default "" $sc.domain))
  "clusterStorage" (dict
    "class" (default "" $cstg.class)
    "user" $cstg.user
    "group" (default 1 $cstg.group)
  )
  "clusterRuntime" (dict "class" (default "" $crt.class))
  "clusterNetwork" (dict "blockCIDRs" (default list $cnet.blockCIDRs))
  "dockerDaemon" (dict
    "networkMTU" (default 1400 $dd.networkMTU)
    "proxyCache" (dict
      "remoteURL" (default "" $proxy.remoteURL)
      "username" (default "" $proxy.username)
      "password" (default "" $proxy.password)
    )
  )
  "workshopAnalytics" (dict
    "google" (dict "trackingId" (default "" (dig "google" "trackingId" "" $wa)))
    "clarity" (dict "trackingId" (default "" (dig "clarity" "trackingId" "" $wa)))
    "amplitude" (dict "trackingId" (default "" (dig "amplitude" "trackingId" "" $wa)))
    "webhook" (dict "url" (default "" (dig "webhook" "url" "" $wa)))
  )
  "websiteStyling" (dict
    "defaultTheme" (default "" $wstyle.defaultTheme)
    "frameAncestors" (default list $wstyle.frameAncestors)
  )
-}}
{{- $merged := mergeOverwrite $typed (deepCopy (default dict .Values.config)) -}}
{{- toYaml $merged -}}
{{- end -}}

{{/*
Map the structured `websiteStyling.inline` values to the flat secret-key shape
the runtime expects in the `default-website-theme` Secret. Returns a YAML map
of stringData entries (or empty if no inline assets are populated).

  workshopDashboard.{html,script,style}    -> workshop-dashboard.{html,js,css}
  workshopInstructions.{html,script,style} -> workshop-instructions.{html,js,css}
  workshopStarted.html                     -> workshop-started.html
  workshopFinished.html                    -> workshop-finished.html
  trainingPortal.{html,script,style}       -> training-portal.{html,js,css}
*/}}
{{- define "session-manager.inlineThemeStringData" -}}
{{- $inline := default dict (default dict .Values.websiteStyling).inline -}}
{{- $entries := dict -}}
{{- $tripleSources := list
    (list "workshopDashboard" "workshop-dashboard")
    (list "workshopInstructions" "workshop-instructions")
    (list "trainingPortal" "training-portal")
-}}
{{- range $tripleSources -}}
  {{- $key := index . 0 -}}
  {{- $prefix := index . 1 -}}
  {{- $block := default dict (index $inline $key) -}}
  {{- if $block.html }}{{- $_ := set $entries (printf "%s.html" $prefix) $block.html -}}{{- end -}}
  {{- if $block.script }}{{- $_ := set $entries (printf "%s.js" $prefix) $block.script -}}{{- end -}}
  {{- if $block.style }}{{- $_ := set $entries (printf "%s.css" $prefix) $block.style -}}{{- end -}}
{{- end -}}
{{- $started := default dict $inline.workshopStarted -}}
{{- if $started.html }}{{- $_ := set $entries "workshop-started.html" $started.html -}}{{- end -}}
{{- $finished := default dict $inline.workshopFinished -}}
{{- if $finished.html }}{{- $_ := set $entries "workshop-finished.html" $finished.html -}}{{- end -}}
{{- toYaml $entries -}}
{{- end -}}
