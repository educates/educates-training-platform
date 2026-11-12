---
name: educates-upgrade-vcluster
description: Upgrade the vcluster software version (and the Kubernetes versions the virtual cluster provisions) used by the vcluster workshop application in session-manager. Invoke when asked to "upgrade vcluster", "bump vcluster", "update vcluster to vX.Y.Z", or "align vcluster kubernetes versions".
argument-hint: "[<new-vcluster-version>] (e.g. 0.35.2)"
allowed-tools: "Read, Edit, Glob, Bash, WebFetch"
---

# Educates vcluster Upgrade Protocol

Upgrade the `vcluster` workshop application to the vcluster version in
`$ARGUMENTS` (e.g. `0.35.2`). With no argument, find the latest stable release
at `https://github.com/loft-sh/vcluster/releases` and confirm the target with
the user before proceeding.

This is a **runtime-component** change (`session-manager/`). Confirm the scope
with the user before starting if it was not explicitly requested.

## Why this is not a one-line bump

Educates does **not** `helm install` vcluster at runtime. session-manager
(`session-manager/handlers/application_vcluster.py`) deploys each virtual
cluster by applying **hand-written host-cluster objects** and embeds the
vcluster config as a Secret (`vc-config-my-vcluster`, key `config.yaml`) mounted
into the control-plane pod. The **StatefulSet, Services, and RBAC in that file
are hand-maintained transcriptions of what the upstream vcluster Helm chart
renders**. Nothing regenerates them, so a version bump means re-rendering the
chart and reconciling those objects by hand, preserving Educates' own
customizations. Since vcluster 0.20 the `k8s` distro runs in-process inside the
single `syncer` container (no separate apiserver/etcd Deployment); an init
container stages the Kubernetes binaries from the `loftsh-kubernetes` image.

vcluster's Helm **chart version equals its app/binary version** (chart `0.35.2`
ships vcluster `0.35.2`), unlike the cluster-service charts. There is no
separate appVersion to resolve.

## The version-state surface (all must move together)

1. **`installer/charts/educates-training-platform/charts/session-manager/templates/_helpers.tpl`**
   — the `imageVersions` inventory: `loftsh-vcluster`
   (`ghcr.io/loft-sh/vcluster-oss:<ver>` — Educates uses the **OSS** variant),
   the `loftsh-kubernetes-v1.NN` image set, and `vcluster-internal-contour` /
   `vcluster-internal-envoy` (the Contour/Envoy used only when a workshop
   enables vcluster ingress). The air-gap image list flows from here
   automatically via `hack/generate-image-list.sh` — no separate edit.
2. **`session-manager/handlers/operator_config.py`** — the
   `LOFTSH_KUBERNETES_V1_NN_IMAGE = image_reference(...)` constants and
   `LOFTSH_VCLUSTER_IMAGE` (the latter is version-agnostic; it resolves through
   the inventory).
3. **`session-manager/handlers/application_vcluster.py`** — the `operator_config`
   imports, the `K8S_VERSIONS` map, and `K8S_DEFAULT_VERSION`, plus the embedded
   StatefulSet / Service / RBAC objects (see reconciliation below).
4. **`session-manager/packages/vcluster/vcluster-all-config.yaml`** — the
   upstream chart `values.yaml`, copied verbatim (with comments) for the target
   tag. session-manager loads it, applies a handful of Educates overrides, and
   ships it as the config Secret. Update the version pin in its 3-line header.
5. **`hack/generate-image-list.sh`** — `VCLUSTER_OWN_IMAGES`, the images
   vcluster picks itself and the inventory does not pin: the CoreDNS tag of
   each `K8S_VERSIONS` entry, from `CoreDNSVersionMap` in
   `pkg/constants/coredns.go` of `loft-sh/vcluster` at the target tag, and the
   rewrite-hosts init container image from `vcluster-all-config.yaml`
   (`sync.toHost.pods.rewriteHosts.initContainer.image`). With an image
   registry prefix, session-manager sets vcluster's
   `controlPlane.advanced.defaultImageRegistry` to it, so these are pulled from
   `<prefix>/coredns/coredns` and `<prefix>/library/alpine`, and the mirror
   needs them.

## Release-page artifacts

From `https://github.com/loft-sh/vcluster/releases/tag/v<VER>`:

- **`images-optional.txt`** — the authoritative list of the **OSS** vcluster
  image and every **optional** Kubernetes version the release supports
  (`ghcr.io/loft-sh/kubernetes:v1.NN.p`), plus etcd/coredns variants. This is
  where the supported k8s set and the exact patch versions come from.
- **`images.txt`** — the **default** images: the `vcluster-pro` image, the
  **default** k8s version (the newest, not in `images-optional.txt`), and the
  default coredns.
- **`vcluster-images-v1.NN.txt`** (per-k8s-version) — the full image set for
  **full air-gap** support of that k8s version (etcd, coredns, kine, etc.).
  Out of scope for a version bump, but note it for the air-gap follow-up.

Together, `images.txt` (default k8s) + `images-optional.txt` (optional k8s)
define the full supported Kubernetes range for the release.

```bash
curl -sSfL https://github.com/loft-sh/vcluster/releases/download/v<VER>/images-optional.txt
curl -sSfL https://github.com/loft-sh/vcluster/releases/download/v<VER>/images.txt
```

## Keeping vcluster and platform Kubernetes versions aligned

**The set of Kubernetes versions the vcluster application provisions
(`K8S_VERSIONS`) should match the platform's supported Kubernetes versions**
(the kubectl / kind set managed by the **`educates-upgrade-kubernetes`** skill),
as long as vcluster's `images-optional.txt` (plus the default in `images.txt`)
lists them. `K8S_DEFAULT_VERSION` should match `DefaultKubernetesVersion` in
`client-programs/pkg/constants/kubernetes.go`.

When you run this skill:

1. Read the platform's current set from `pkg/constants/kubernetes.go`
   (`KubernetesVersionToKindImage`, `DefaultKubernetesVersion`).
2. Intersect it with what the target vcluster release supports
   (`images.txt` + `images-optional.txt`). If a platform version is not
   supported by this vcluster release, **stop and ask the user** how to
   reconcile (hold vcluster back, or drop that k8s version platform-wide).
3. If the two sets already match and are supported, align `K8S_VERSIONS` to
   them. If they diverge, **ask the user for permission to run
   `educates-upgrade-kubernetes`** to bring the platform set in line (or to
   accept a deliberate divergence), rather than silently picking one.

The `educates-upgrade-kubernetes` skill carries the reciprocal reminder.

## Step-by-step

1. **Resolve versions.** Read `images-optional.txt` + `images.txt`. Confirm the
   `vcluster-oss:<VER>` tag and the exact `loftsh-kubernetes:v1.NN.p` patch tags
   for the k8s set you will support. Reconcile with the platform set (above).
2. **Update the four version-state files** (inventory, constants, handler map +
   default, values file). Re-copy `vcluster-all-config.yaml` from
   `https://github.com/loft-sh/vcluster/blob/v<VER>/chart/values.yaml` and
   update its header pin. Fold in any new config sub-trees the new values file
   introduces.
3. **Render the chart with Educates' effective config** and reconcile the
   embedded objects:

   ```bash
   helm repo add loft https://charts.loft.sh && helm repo update loft
   # overrides.yaml = the sync toggles application_vcluster.py sets at runtime,
   # because they change the generated RBAC:
   cat > overrides.yaml <<'EOF'
   sync:
     toHost:
       serviceAccounts: { enabled: true }
       ingresses: { enabled: true }
     fromHost:
       storageClasses: { enabled: true }
       ingressClasses: { enabled: true }
   policies:
     resourceQuota: { enabled: false }
     limitRange: { enabled: false }
   EOF
   helm template my-vcluster loft/vcluster --version <VER> -n my-vcluster-vc \
     -f session-manager/packages/vcluster/vcluster-all-config.yaml \
     -f overrides.yaml
   ```

   Diff the rendered `StatefulSet`, `Service`s, `ClusterRole`, and `Role`
   against the embedded dicts. Apply the deltas, but **preserve Educates'
   customizations** (do not blindly copy the chart):
   - the `k8s_image` init container and `LOFTSH_VCLUSTER_IMAGE` syncer image;
   - parametrized resources (`syncer_memory` / `syncer_storage`);
   - `storageClassName: None` on the data volumeClaimTemplate;
   - the pod `securityContext` storage-group (`CLUSTER_STORAGE_GROUP`);
   - the **non-root** syncer `securityContext` (`runAsUser 12345`). vcluster's
     chart may render this container as **root** (`runAsUser 0`); keep non-root
     and validate at runtime. If the control plane misbehaves after the bump,
     revisit whether non-root is still supported upstream.
4. **Reconcile RBAC** (the load-bearing, error-prone step). The embedded
   `ClusterRole`/`Role`s are **Educates-curated**, not a verbatim chart copy
   (they add `ingressclasses`/`storageclasses`/`ingresses`/`serviceaccounts`
   grants for Educates' sync toggles). Add whatever the new render newly
   requires (past bumps added `pods/resize`, `pods/ephemeralcontainers`,
   `events.k8s.io` events, `endpointslices` create/delete). The `-vc` namespace
   Role is the syncer's sync-target Role (it holds `pods/status`); the
   session-namespace Role is intentionally narrower.
   **Mirror every added rule into `educates:session-manager:vcluster` in
   `installer/charts/educates-training-platform/charts/session-manager/templates/clusterroles.yaml`**
   — Kubernetes privilege-escalation prevention means session-manager can only
   create the per-session Roles if it already holds every permission they grant,
   so that ClusterRole must stay a superset. Under-granting shows up at runtime
   as a `403 "attempting to grant RBAC permissions not currently held"` when a
   vcluster session is created.
5. **Release note + docs.** Add a `Features Changed` entry to the current
   `project-docs/release-notes/version-<X.Y.Z>.md` (vcluster version + the k8s
   set change). Review `project-docs` for anything that names the vcluster or
   k8s versions. No emdashes.

## Verify

```bash
python3 -c "import ast; ast.parse(open('session-manager/handlers/application_vcluster.py').read())"
# Re-render and confirm the embedded StatefulSet/Services/RBAC match the chart
# output (modulo the preserved Educates customizations listed above).
```

The authoritative check is a **runtime smoke test**: deploy a workshop whose
`session.applications.vcluster.enabled: true`, confirm the control-plane pod
reaches Ready, the kubeconfig Secret is copied into the session, and
`kubectl` against the virtual cluster works. Exercise a workshop that syncs
services / ingresses so the reconciled RBAC is actually hit.

## Post-upgrade checklist

- [ ] `_helpers.tpl` inventory: `loftsh-vcluster` (oss) + `loftsh-kubernetes-v1.NN` set updated (exact patch tags from `images-optional.txt` / `images.txt`)
- [ ] `operator_config.py` constants match the inventory names
- [ ] `application_vcluster.py`: imports, `K8S_VERSIONS`, `K8S_DEFAULT_VERSION` updated
- [ ] `K8S_VERSIONS` aligned with the platform set (`pkg/constants/kubernetes.go`), or divergence agreed with the user
- [ ] `vcluster-all-config.yaml` re-copied from the tag + header pin updated
- [ ] `VCLUSTER_OWN_IMAGES` in `hack/generate-image-list.sh` matches vcluster's `CoreDNSVersionMap` for every `K8S_VERSIONS` entry and the rewrite-hosts init image
- [ ] Embedded StatefulSet / Services reconciled against the render, Educates customizations preserved
- [ ] Embedded RBAC reconciled AND mirrored into `educates:session-manager:vcluster` (clusterroles.yaml)
- [ ] Non-root syncer securityContext kept + flagged for runtime validation
- [ ] Release note added; `project-docs` reviewed
- [ ] `application_vcluster.py` parses; chart re-render matches; runtime vcluster workshop smoke-tested
- [ ] Air-gap follow-up noted (per-k8s-version image txt files) if full air-gap is in scope
