Air-gapped Installation
=======================

Educates can be installed on clusters without internet access, or against an internal registry mirror, using the digest-pinned image list published with every release.

What a release provides
-----------------------

Each GitHub release attaches ``educates-images-<version>.txt``: one fully
qualified, digest-pinned image reference per line, covering everything an
installation can need. It holds the Educates platform images at the released
version, including the operator and the image pre-puller's helper
``educates-image-puller``; every image of the session manager's image
inventory, which includes the workshop base environment, the JDK and conda
workshop environments and the images the vcluster workshop application uses;
and the upstream cluster-service images (cert-manager, Contour, external-dns,
Kyverno) the operator installs in Managed mode.

The list is complete on purpose, although most installations use only some of
these images and the JDK and conda environments add several gigabytes each.
When mirroring, delete the entries you do not use rather than guessing which
ones might be missing.

The Helm charts are attached to the release as tarballs (`educates-installer-<version>.tgz`, `educates-training-platform-<version>.tgz`) for transport, alongside the CLI binaries.

Mirroring the images
--------------------

Mirror the listed images into your internal registry **preserving their repository paths** — Educates resolves images by composing a registry prefix with the original path, so the layout must survive the copy. Any name-preserving tool works; with `skopeo` on a connected machine:

```shell
mkdir mirror
while read -r ref; do
  skopeo copy --all "docker://${ref}" "dir:mirror/$(echo "${ref%@*}" | tr '/:' '__')"
done < educates-images-X.Y.Z.txt
```

Carry the directory (and the chart tarballs and CLI binary) across the air gap, then push into the internal registry, keeping each image's original repository path under your chosen prefix — for example `ghcr.io/educates/educates-session-manager:X.Y.Z` becomes `registry.internal/educates/educates/educates-session-manager:X.Y.Z` and `quay.io/jetstack/cert-manager-controller:vA.B.C` becomes `registry.internal/educates/jetstack/cert-manager-controller:vA.B.C`.

For online-but-mirrored environments (the cluster can reach an internal registry that proxies or mirrors the upstream ones), `skopeo sync` or a registry pull-through cache achieves the same layout without the tarball hop.

Pointing the installation at the mirror
---------------------------------------

Two settings re-root image resolution onto the mirror:

* **`EducatesClusterConfig.spec.imageRegistry.prefix`** — rewrites the images of everything the operator installs (cluster services in Managed mode and the Educates runtime components). Reachable via the `educatesClusterConfig` block of an `EducatesConfig` configuration file, or `imageRegistry.prefix` in `EducatesInlineConfig`.
* **The operator chart's `development.imageRegistry`** value (`{host, namespace}`) — rewrites the operator pod's own image when installing the chart directly with Helm. When deploying via the CLI, set `operator.image.repository` in the configuration file instead.

For a Helm-driven install from the transported chart tarball:

```shell
helm install educates-installer educates-installer-X.Y.Z.tgz \
  --namespace educates-installer --create-namespace \
  --set development.imageRegistry.host=registry.internal \
  --set development.imageRegistry.namespace=educates/educates
kubectl apply -f educates-cluster-config.yaml   # spec.imageRegistry.prefix: registry.internal/educates
```

If the mirror requires authentication, create the pull secret in the operator namespace and reference it via `imagePullSecrets` in the operator chart, so that the operator pod can pull its own image. The operator does not distribute registry credentials to the cluster services and Educates runtime components it installs, so the cluster's nodes must be able to pull those images from the mirror themselves, for example through node-level registry credentials or a pull-through cache that needs no authentication.

Workshop content images are a separate concern from the platform: workshops published with `educates publish-workshop` are self-contained OCI artifacts that can be relocated with `imgpkg copy`, as covered in the workshop content documentation.
