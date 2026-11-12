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

Mirror the listed images into your internal registry under a prefix of your
choosing, keeping each image's upstream repository path, without its
registry host, under the prefix. Educates composes every image reference
from the prefix and that path, so the layout must survive the copy. Under
the prefix `registry.internal/educates`,
`ghcr.io/educates/educates-session-manager:X.Y.Z` becomes
`registry.internal/educates/educates/educates-session-manager:X.Y.Z` and
`quay.io/jetstack/cert-manager-controller:vA.B.C` becomes
`registry.internal/educates/jetstack/cert-manager-controller:vA.B.C`.

Each line of the list names an image by both tag and digest,
`<repository>:<tag>@sha256:<digest>`, a form `skopeo` does not accept, so the
loops below copy each image by its digest and push it under its tag. On a
connected machine, copy the images into a directory:

```shell
mkdir mirror
while read -r ref; do
  name="${ref%@*}"
  skopeo copy --all --preserve-digests "docker://${name%:*}@${ref#*@}" \
    "dir:mirror/$(echo "${name}" | tr '/:' '__')"
done < educates-images-X.Y.Z.txt
```

Carry the directory, the image list, the chart tarballs and the CLI binary
across the air gap, then push each image to its path under the prefix:

```shell
PREFIX=registry.internal/educates
while read -r ref; do
  name="${ref%@*}"
  skopeo copy --all --preserve-digests "dir:mirror/$(echo "${name}" | tr '/:' '__')" \
    "docker://${PREFIX}/${name#*/}"
done < educates-images-X.Y.Z.txt
```

A machine that reaches both the upstream registries and the internal one can
skip the directory and copy each image straight to its destination:

```shell
PREFIX=registry.internal/educates
while read -r ref; do
  name="${ref%@*}"
  skopeo copy --all --preserve-digests "docker://${name%:*}@${ref#*@}" \
    "docker://${PREFIX}/${name#*/}"
done < educates-images-X.Y.Z.txt
```

`skopeo sync` does not produce the layout on its own: it keeps only the last
path element of each image, or with `--scoped` its whole reference, registry
host included, so it takes one run for each upstream namespace with that
namespace added to the destination.

The internal registry must accept repository names several levels deep, such
as `educates/educates/educates-session-manager` under the prefix. CNCF
Distribution, Zot, Harbor (inside a project), Red Hat Quay (inside an
organization), Amazon ECR, Azure Container Registry, Google Artifact Registry,
Sonatype Nexus and JFrog Artifactory accept them. Docker Hub does not, since
its repository names cannot contain a slash. GitLab allows two levels below a
project, exactly what the layout needs, so the project itself must be the
prefix. Some registries need the destination to exist before the first push:
a Harbor project, a Quay organization, an Artifact Registry, Nexus or
Artifactory repository, and in Amazon ECR each repository, unless a
repository creation template creates them on push.

A pull-through cache can stand in for the copy only when it serves the
images of every upstream registry the list names, `ghcr.io`, `quay.io`,
`docker.io`, `reg.kyverno.io` and `registry.k8s.io`, under the one prefix and
at their upstream paths. Most caches front one upstream registry under a path
of their own, so they cannot: the proxy mode of CNCF Distribution mirrors a
single upstream, a Harbor proxy cache project fronts one registry endpoint
and keeps the upstream path under the project, and each Amazon ECR pull
through cache rule maps one upstream registry to its own repository prefix.
Zot's sync extension can map each upstream to its destination under one
prefix, and so can the cache rules of Azure Container Registry's artifact
cache, one for each upstream namespace, except for `reg.kyverno.io`, which
it does not offer as an upstream. Repositories that group several proxies
behind one path, such as a Nexus group, an Artifactory virtual repository or
an Artifact Registry virtual repository, look an image up in each upstream in
turn, so a path that exists on more than one upstream resolves to whichever
is tried first.

Node-level registry mirrors, such as containerd's `hosts.toml` or CRI-O's
`registries.conf`, are an alternative to the prefix: they let the nodes pull
the images by their original references from the internal registry, one
upstream registry at a time, and no prefix is set.

Pointing the installation at the mirror
---------------------------------------

Two settings re-root image resolution onto the mirror:

* **The image registry prefix of `EducatesClusterConfig`**,
  `spec.imageRegistry.prefix` in Managed mode and
  `spec.inline.imageRegistry.prefix` in Inline mode, moves the images of
  everything the operator installs: the cluster services in Managed mode, and
  the Educates runtime components in both modes, including the images the
  session manager creates training portals and workshop sessions from. Each
  image keeps its upstream repository path, without the upstream registry
  host, under the prefix, which is the layout the mirroring steps above
  produce. With the prefix `registry.internal/educates`,
  `ghcr.io/educates/educates-session-manager:X.Y.Z` is pulled from
  `registry.internal/educates/educates/educates-session-manager:X.Y.Z`. From
  the CLI, set it in the `educatesClusterConfig` block of an `EducatesConfig`
  configuration file, or as `imageRegistry.prefix` in `EducatesInlineConfig`.
* **The operator chart's `development.imageRegistry`** value
  (`{host, namespace}`) moves the operator pod's own image when installing the
  chart directly with Helm. When deploying via the CLI, set
  `operator.image.repository` in the configuration file instead.

The prefix does not move the upstream images the session manager's image
inventory pins for the vcluster workshop application and for Docker support in
workshop sessions, such as `ghcr.io/loft-sh/vcluster-oss` and `docker`. Point
those at the mirror with `imageVersions` overrides of the matching names. The
images a session's vcluster picks itself, which the inventory does not pin, do
move under the prefix, since the session manager makes the prefix vcluster's
default image registry: the CoreDNS of the virtual cluster, whose version
follows its Kubernetes version, such as `docker.io/coredns/coredns:1.14.2`
pulled from `registry.internal/educates/coredns/coredns:1.14.2`, and the init
container that rewrites the hosts of stateful set pods,
`mirror.gcr.io/library/alpine:3.20`. The release image list includes them. With
a prefix set, the `$(image_repository)` data variable in workshop definitions
resolves by default to where the Educates images are,
`registry.internal/educates/educates` in the example above.

Changing the prefix on a running installation moves the cluster services and
the Educates runtime components to the new location. Training portals and
workshop sessions created before the change keep running the images they were
created with.

For a Helm-driven install from the transported chart tarball:

```shell
helm install educates-installer educates-installer-X.Y.Z.tgz \
  --namespace educates-installer --create-namespace \
  --set development.imageRegistry.host=registry.internal \
  --set development.imageRegistry.namespace=educates/educates
kubectl apply -f educates-cluster-config.yaml   # spec.imageRegistry.prefix: registry.internal/educates
```

If the mirror requires authentication, create a `kubernetes.io/dockerconfigjson`
Secret holding its credentials in the operator namespace, `educates-installer`
by default, and use it in two places:

* **The operator chart's `imagePullSecrets`**, so that the operator pod can
  pull its own image. From the CLI, set `operator.imagePullSecrets` in the
  configuration file.
* **The image pull secrets of `EducatesClusterConfig`**,
  `spec.imageRegistry.pullSecrets` in Managed mode and
  `spec.inline.imageRegistry.pullSecrets` in Inline mode, so that everything
  the operator installs can pull from the mirror. From the CLI, set them in
  the `educatesClusterConfig` block of an `EducatesConfig` configuration file,
  or as `imageRegistry.pullSecrets` in `EducatesInlineConfig`.

A pod can use a pull secret only from its own namespace, so the operator
copies each Secret listed in `pullSecrets`, under the same name, into every
namespace it installs a chart into: the `educates` namespace of the Educates
runtime components in both modes, which it creates first if needed, and in
Managed mode the `cert-manager`, `contour`, `external-dns` and `kyverno`
namespaces of the cluster services it installs. Each of those charts is given
the Secrets for all of its pods, Helm hooks included. In Inline mode the
cluster services are your own and keep their own pull secrets, so only
`educates` gets copies. Rules the session manager installs then have the
secrets manager copy the Secrets from `educates` into the training portal,
workshop environment and workshop session namespaces the session manager
creates, and add them to the service accounts there. Every service account of a
workshop session namespace gets them, so a workshop's own pods, such as the
vcluster of a session whose images you point at the mirror with
`imageVersions`, can pull from the mirror too. A pod takes its service
account's pull secrets only when it is created, so the session manager also
names the Secrets itself on every service account it creates, and on the
`default` service account of every namespace it creates, before anything runs
there. Workshop users can read the
Secrets in their session namespaces, so give the Secrets credentials that only
allow pulling from the mirror.

To keep the credentials from workshop users altogether, set
`spec.imagePullSecretPropagation.sessionNamespaces` of the `SessionManager` to
`false`. Workshop session namespaces then get no copies, except the namespace
of a session's vcluster, which the session user cannot reach and which the
vcluster needs to start. The setting applies to sessions created after it is
changed; existing session namespaces keep their copies until their sessions
end. Everything else keeps pulling from the mirror as
before, but pods that a workshop or its users run in their session namespaces,
such as those of a workshop's session objects, can then only pull images that
need no credentials from them. One way to serve those is a pull-through proxy,
or one for each registry, running in a namespace workshop users cannot reach,
holding the credentials itself and serving pulls without them. The nodes pull
the images, not the pods, so a proxy must be at an address the nodes can
resolve and trust, such as an ingress host name or a node port on `localhost`,
rather than a cluster service name.

The copies follow the Secrets in the operator namespace, so rotating the
credentials there updates every copy, down to the workshop namespaces. Removing
a name from `pullSecrets` deletes its copies. The copies belong to the
`EducatesClusterConfig`: deleting Educates leaves them in place while the
cluster services are uninstalled, so that Helm hooks such as Kyverno's can
still pull, and removes them once the `EducatesClusterConfig` is gone. Until
every Secret listed exists and has the type `kubernetes.io/dockerconfigjson`,
the `EducatesClusterConfig` reports `Ready` as `False` with a message naming
the entry.

Alternatively, nodes that hold credentials for the mirror themselves, through a
kubelet image credential provider, the container runtime's registry
configuration or a cloud node identity, can pull every image without pull
secrets.

Workshop content images are a separate concern from the platform: workshops published with `educates publish-workshop` are self-contained OCI artifacts that can be relocated with `imgpkg copy`, as covered in the workshop content documentation.
