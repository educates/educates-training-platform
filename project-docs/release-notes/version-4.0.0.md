Version 4.0.0
=============

New Features
------------

* Educates is now installed by a Kubernetes operator. The new
  ``educates-installer`` Helm chart installs the operator together with four
  cluster-scoped custom resource definitions — ``EducatesClusterConfig``
  (``config.educates.dev/v1alpha1``) for cluster-wide infrastructure and
  services, and ``SecretsManager``, ``LookupService`` and ``SessionManager``
  (``platform.educates.dev/v1alpha1``) for the individual Educates platform
  components. The same chart can be installed imperatively using
  ``helm install``, or declaratively by pointing a GitOps tool such as ArgoCD
  or Flux at it. See the installation guides in the documentation for the
  supported installation paths.

* ``EducatesClusterConfig`` supports two modes. In ``Managed`` mode the
  operator installs and manages the cluster services Educates depends on —
  cert-manager for TLS certificates, Contour as the ingress controller,
  Kyverno for security policy enforcement, and optionally external-dns for DNS
  record management — from versions of their Helm charts bundled with the
  operator. In ``Inline`` mode you instead declare the equivalent resources
  which already exist in the cluster (ingress class, wildcard TLS certificate
  secret, policy engine), allowing Educates to be installed onto clusters with
  pre-existing infrastructure, such as OpenShift.

* When a bundled Helm chart fails to install, the operator now reports the
  failure on the owning custom resource — the relevant condition goes to
  ``False`` with reason ``ReleaseFailed`` and the underlying Helm error, and
  the resource phase becomes ``Degraded`` — rather than reporting ``Ready``.
  The operator recovers on its own once the cause is resolved: applying an
  updated operator image or chart triggers a reinstall of a failed first
  install, an in-place upgrade when a previously working release exists, or a
  rollback when a release is stuck mid-operation. A failed release whose inputs
  are unchanged is left untouched rather than reinstalled repeatedly.

* In ``Managed`` mode, wildcard TLS certificates can be issued from a custom
  certificate authority, or obtained automatically from an ACME provider such
  as LetsEncrypt using DNS01 challenges, with support for Amazon Route53
  (using IAM Roles for Service Accounts on EKS) and Google Cloud DNS (using
  Workload Identity on GKE).

* A standalone ``educates-training-platform`` umbrella Helm chart, with
  subcharts for the secrets-manager, lookup-service and session-manager
  components, can be used to install the Educates runtime without the
  operator for users who want to manage cluster services and configuration
  entirely themselves.

* The CLI configuration file format is now kind based, under
  ``cli.educates.dev/v1alpha1``. ``EducatesLocalConfig`` configures the local
  (kind) cluster environment, the ``EducatesGKEConfig``, ``EducatesEKSConfig``
  and ``EducatesInlineConfig`` kinds provide opinionated configuration for
  GKE, EKS and bring-your-own clusters respectively, and ``EducatesConfig``
  is an escape hatch which carries the operator custom resource specs
  verbatim with no CLI defaulting. JSON schemas for all the configuration
  kinds are embedded in the CLI for validation and published at
  ``https://schemas.educates.dev/cli/v1alpha1/`` for editor integration.

* New ``educates admin platform`` commands drive the installation
  end-to-end. ``deploy`` installs the operator chart, applies the platform
  custom resources and waits for each to report ``Ready``. ``render`` emits
  the Helm chart values and custom resources for inspection, or for committing
  to a GitOps repository. ``delete`` uninstalls everything in reverse order
  and supports ``--yes`` and ``--purge`` options. ``educates local cluster
  create`` lays down the full local environment — kind cluster, local image
  registry and platform — in a single command, with preflight checks for
  common problems such as ports already being in use.

* The ``educates local config`` command group now provides ``init``, ``get``,
  ``set``, ``view``, ``edit`` and ``explain`` commands for managing the local
  configuration, with validation against the configuration schemas. The
  ``view`` command prints the effective configuration with all CLI defaults
  filled in, so the values the CLI supplies on your behalf (including the
  ``<host-IP>.nip.io`` domain and the insecure default it implies) are
  explicit rather than hidden behind a sparse file; pass ``--raw`` to print
  the configuration file exactly as written instead. The ``init`` command
  writes a minimal file by default, or with ``--defaults`` writes the
  fully-defaulted configuration with those same values materialised into
  the file.

* A new ``educates local config explain`` command describes configuration
  fields from the embedded JSON schema, in the style of ``kubectl explain``,
  without needing a cluster or a configuration file. It takes a dotted field
  path (for example ``educates local config explain ingress.insecure``) and
  prints the field's type, default and description along with its
  sub-fields. The kind defaults to ``EducatesLocalConfig`` and is chosen
  with ``--kind``, which accepts a kind name or a short alias (``local``,
  ``gke``, ``eks``, ``inline``, ``escape``), so the GKE, EKS, inline and
  escape-hatch kinds can be explored the same way. The descriptions come
  from the schemas, which now carry field documentation for every kind.

* Workshop sessions on local clusters are now served using TLS certificates
  issued in-cluster by cert-manager from a local certificate authority managed
  using ``educates local secrets add ca``. The generated CA is cached locally
  and reused for every future cluster using the same ingress domain. The CA
  certificate can be exported as a PEM file using
  ``educates local secrets export NAME --pem`` for importing into the
  operating system trust store, so browsers trust the workshop URLs; the
  quick start guide includes per-platform import instructions.

* TLS termination by an external load balancer or proxy in front of the
  cluster is now supported by setting ``externalTLSTermination`` in the CLI
  configuration kinds, or ``ingressOverrides.protocol`` in the
  ``SessionManager`` custom resource.

* The Helm charts are published as OCI artifacts to
  ``ghcr.io/educates/charts`` with each release, and a digest-pinned list of
  all images used by a release is attached to the GitHub release to support
  mirroring images into air-gapped registries. The list is intentionally
  complete: in addition to the platform and bundled cluster-service images it
  includes every image in the session-manager image inventory — the
  workshop base, the JDK and conda workshop environments, and the images the
  vcluster workshop application uses (``vcluster`` itself, its loft-sh
  Kubernetes distribution images, the Contour and Envoy images for vcluster
  ingress, ``docker-in-docker`` and the debian base). When mirroring, delete
  the entries you do not use rather than guessing which ones might be missing.

* ``imageVersions`` entries in the CLI configuration now reach every
  platform image: entries named ``secrets-manager`` and ``lookup-service``
  are routed to those components' own custom resources, and the
  ``session-manager``, ``pause-container`` and ``node-ca-injector`` names —
  previously ignored — now override the images they refer to. For
  contributors, a developer-built CLI (one whose embedded version is not a
  release version) automatically targets its compiled-in image registry for
  all platform images, so building Educates from source and deploying the
  locally built system requires no configuration of image references;
  explicit configuration entries always take precedence. See the developer
  documentation for the local build workflow.

* Educates can be installed without in-cluster TLS. The
  ``EducatesClusterConfig`` certificates provider accepts a new ``None``
  value, which installs no cert-manager, issues no certificate, and serves
  workshops over plain HTTP. A companion ``ingress.protocol`` setting
  (``http`` or ``https``) asserts the scheme of the generated workshop and
  portal URLs independently of whether the cluster terminates TLS, so a
  cluster sitting behind an external load balancer that terminates TLS can
  still generate ``https`` URLs while presenting no in-cluster certificate.
  On the local configuration, setting ``ingress.insecure`` selects a plain
  HTTP install and skips the one-time ``educates local secrets add ca``
  step. A local configuration that sets no ingress domain now defaults to
  this insecure mode, since the ``<host-IP>.nip.io`` address it falls back
  to cannot obtain a trusted certificate; set a custom ``ingress.domain``
  to get the certificate-backed HTTPS install. On the
  ``EducatesGKEConfig``, ``EducatesEKSConfig`` and
  ``EducatesInlineConfig`` kinds, ``externalTLSTermination`` now maps to
  this provider, so no unused in-cluster certificate is issued when TLS is
  handled by an external load balancer or proxy.

* The ``educates local mirror list`` command lists the local image
  registry mirrors that have been deployed, complementing the existing
  ``deploy`` and ``delete`` commands in the ``educates local mirror``
  group.

* The ``educates cluster portal export`` command exports a training portal
  and the workshops it references as Kubernetes resource YAML, either to
  standard output as a single document stream or, with ``--as-files``, as
  one file per resource in a target directory. The exported resources are
  sanitised so they can be re-applied to another cluster: cluster-managed
  status and metadata are stripped and the portal password is removed. The
  ``--image-repository`` and ``--workshop-version`` options substitute the
  ``$(image_repository)`` and ``$(workshop_version)`` variables in the
  exported workshops.

* ``educates local cluster create`` accepts a ``--kubernetes-version`` flag
  to choose the Kubernetes version of the local Kind cluster. The supported
  versions are ``1.33``, ``1.34``, ``1.35`` and ``1.36``, defaulting to
  ``1.36``. The version selects a matching Kind node image; an explicit
  ``--kind-cluster-image`` still takes precedence for pinning an arbitrary
  image.

* The local Kind cluster can now be multi-node. The ``EducatesLocalConfig``
  ``cluster.nodes`` list declares the cluster's nodes — each with a
  ``role`` (``control-plane`` or ``worker``), optional Kubernetes node
  ``labels``, and optional ``taints`` — so a laptop cluster can model node
  pools for workshops that schedule across several nodes. Leaving
  ``cluster.nodes`` unset keeps the default single control-plane cluster.

* ``educates local cluster status`` now also reports whether Educates is
  installed on the local cluster and whether its platform components are
  ready. It reads the ``EducatesClusterConfig``, ``SecretsManager``,
  ``LookupService`` and ``SessionManager`` custom resources and reports one
  of three outcomes: a cluster-only cluster (kind running, Educates not
  installed), an installed-but-not-ready cluster (one or more components not
  yet ``Ready``), or a fully installed and ready cluster — making it easy to
  tell a working install from a partial or broken one.

* New user-facing ``ClusterRole`` resources are installed for working with the
  Educates custom resources. For each API group there is an admin and a view
  role: ``educates:rbac:admin:training`` and ``educates:rbac:view:training``
  for the ``training.educates.dev`` resources, ``educates:rbac:admin:secrets``
  and ``educates:rbac:view:secrets`` for the ``secrets.educates.dev``
  resources, ``educates:rbac:admin:lookup`` and ``educates:rbac:view:lookup``
  for the ``lookup.educates.dev`` resources, and
  ``educates:rbac:admin:platform`` and ``educates:rbac:view:platform`` for the
  installer's ``config.educates.dev`` and ``platform.educates.dev`` resources.
  Grant these to users or automation that needs to manage or inspect Educates
  resources. They also carry the standard Kubernetes aggregation labels, so
  their rules automatically merge into the built-in ``admin`` and ``view``
  cluster roles.

Features Changed
----------------

* The bundled Kyverno security policies are now ``ValidatingPolicy`` resources
  (``policies.kyverno.io``), the policy type recommended from Kyverno 1.18,
  replacing the previous CEL ``ClusterPolicy`` form. This covers the cluster-wide
  Pod Security Standards profiles and the per-workshop rules. The per-workshop
  rules are now applied as ``ValidatingPolicy`` objects scoped to each workshop
  environment's session namespaces. There is no action required for workshops
  that rely on the built-in rules.

* The training portal has been made more resilient when allocating and
  managing workshop sessions under load. Previously, when many sessions were
  being requested at the same time, or when the Kubernetes REST API was slow
  to respond or applying throttling, session allocation could fail or requests
  could back up, sometimes surfacing as a ``database is locked`` error or as
  failed or timed out session requests. Updates the training portal makes to
  Kubernetes are now performed in a way that no longer blocks other session
  activity while waiting on the Kubernetes API, reducing failed sessions and
  improving reliability under high traffic.

* The ``educates`` CLI commands now give clearer feedback when invoked
  incorrectly and document themselves more consistently. Commands that take a
  positional argument — such as ``educates local mirror deploy``,
  ``educates local secrets add``, ``educates cluster session status`` and
  ``educates workshop new`` — now report a specific message naming the
  missing, extra or invalid argument along with a pointer to ``--help``,
  instead of a generic ``accepts 1 arg(s), received 0`` error. Every
  ``educates`` command now also includes usage examples in its ``--help``
  output.

* Octant is no longer available as the web console for a workshop session and
  the Octant binaries are no longer included in the workshop base image. Octant
  was archived by its authors, with no release since ``0.25.1`` and no fixes
  available for the vulnerabilities reported against the versions which were
  bundled. Workshops which set ``session.applications.console.vendor`` to
  ``octant`` will now be given the Kubernetes dashboard instead, with a warning
  logged against the workshop session. The
  ``session.applications.console.octant`` property has been removed from the
  ``Workshop`` custom resource definition. Note that Octant was never the
  console used by default, including for workshops which enable a virtual
  cluster, so workshops which did not request it explicitly are unaffected.

* The ``bombardier``, ``dive``, ``kwt`` and ``kctrl`` command line tools are no
  longer included in the workshop base image. None of them were used by
  Educates itself. ``kwt`` was archived by its authors in 2023, and ``kctrl``
  is the ``kapp-controller`` command line tool, which is not needed now that
  Educates is installed by its own operator rather than through Carvel
  packaging. Each of the four was already pinned at the most recent release its
  authors have published, and each still carried vulnerabilities with no fixed
  version available, so there was nothing to upgrade to. Workshops which run
  any of these commands from workshop instructions or setup scripts need to
  install them in a custom workshop image, or use an alternative tool.

* The bundled versions of [reveal.js](https://revealjs.com/) used for workshop
  slides have changed. Version ``6.0.1`` is now bundled alongside ``4.6.1``
  and ``5.2.1``, and version ``3.9.2`` is no longer included. Workshops which
  select the reveal.js version through the
  ``session.applications.slides.reveal.js`` property of the ``Workshop``
  custom resource using a ``3.X`` selector need to move to ``4.X``, ``5.X``
  or ``6.X``, and should review their slide decks against the upstream
  reveal.js upgrade guidance since the markup and plugin APIs changed after
  the 3.x series. If the requested version cannot be matched against a
  bundled version the slides are not served for the workshop session.

* The ``ClusterRole`` and ``ClusterRoleBinding`` resources created by Educates
  have been renamed to follow the Kubernetes convention of an application
  prefix separated by colons. Service roles are now named after their
  component — ``educates:session-manager``, ``educates:secrets-manager``,
  ``educates:lookup-service``, ``educates:training-portal``,
  ``educates:tunnel-manager``, ``educates:remote-access``,
  ``educates:node-ca-injector`` and the operator's
  ``educates:installer:manager`` and ``educates:installer:charts``. The roles
  bound to workshop session service accounts are now
  ``educates:session:admin``, ``educates:session:edit`` and
  ``educates:session:view``, replacing ``educates-admin-session-role``,
  ``educates-edit-session-role`` and ``educates-view-session-role``. The
  OpenShift security context constraints roles are now
  ``educates:scc:privileged``, ``educates:scc:baseline`` and
  ``educates:scc:restricted``, and are installed together with the
  ``educates-privileged``, ``educates-baseline`` and ``educates-restricted``
  ``SecurityContextConstraints`` when the cluster security policy engine is
  ``OpenShiftSCC``. All references within the platform have been updated, but
  any workshop definitions or external tooling that reference the old names
  directly, for example a ``session.objects`` entry binding
  ``educates-admin-session-role``, need to be updated to the new names.

Deprecations
------------

* The ``session.applications.console.vendor`` property of the ``Workshop``
  custom resource is deprecated. With the removal of Octant, the Kubernetes
  dashboard is the only console which is available, so the only value the
  property can be given is ``kubernetes``. The property is still accepted so
  existing workshop definitions remain valid, but it will be removed in a
  future version.

* Following Kyverno's own deprecation of the ``ClusterPolicy`` resource
  (``kyverno.io``) — deprecated in Kyverno 1.18 and scheduled for removal in
  1.20 — workshops that supply their own Kyverno policies as ``ClusterPolicy``
  objects via ``workshopSecurity.additionalKyvernoPolicies`` are correspondingly
  deprecated. Such policies are still honoured for now (Educates continues to
  scope them per workshop session, and logs a warning when it does), but support
  will be removed on the same timeline as Kyverno's removal of ``ClusterPolicy``.
  Migrate workshop-provided policies to ``ValidatingPolicy``.

* The Carvel-based installer from version 3 has been removed. Educates is no
  longer packaged or installed as a ``kapp-controller`` package: the
  ``educates-installer`` package bundle and package repository are no longer
  published, and the CLI no longer requires or interacts with
  ``kapp-controller`` for installation. There is no in-place upgrade from
  version 3 — delete the existing version 3 installation and install version
  4 following the new installation guides. A migration guide mapping version
  3 configuration settings onto their version 4 equivalents is included in
  the documentation.

* The local CLI configuration file is now ``config.yaml`` in the CLI data
  home, using the new ``EducatesLocalConfig`` format, replacing the version 3
  ``values.yaml``. On first use the CLI migrates an existing version 3
  ``values.yaml`` automatically when the configured provider was ``kind`` (or
  unset); configurations for other providers must be re-declared using the
  new configuration kinds. As part of the new format some top-level keys have
  been renamed, including ``localKindCluster`` to ``cluster``,
  ``localDNSResolver`` to ``resolver``, and the kind cluster networking
  fields ``podCIDR`` and ``serviceCIDR`` to ``podSubnet`` and
  ``serviceSubnet``.

* Workshop sessions are served over HTTPS when the cluster configuration
  provides certificate settings, which is a wildcard certificate, a
  certificate authority, or ACME issuer details. On local clusters with a
  custom ingress domain, ``educates local cluster create`` stops and prints
  the exact ``educates local secrets add ca`` command to run when no CA
  exists yet for that domain. A local configuration with no ingress domain
  set instead defaults to a plain HTTP install on a ``<host-IP>.nip.io``
  address and needs no CA, since that address cannot obtain a trusted
  certificate. Set ``ingress.insecure`` to request plain HTTP explicitly
  for a custom domain. See the New Features entry on installing without
  in-cluster TLS for the full picture.

* The ``imageCache`` configuration setting has been renamed to
  ``imagePrePuller``. It is now enabled by default in the CLI configuration
  kinds (``EducatesLocalConfig``, ``EducatesGKEConfig``,
  ``EducatesEKSConfig`` and ``EducatesInlineConfig``), so installs created
  with the CLI pre-pull workshop images onto every node ahead of time
  unless ``imagePrePuller`` is set to ``false``. Installs applied directly
  from custom resources or the Helm chart keep the chart default of
  disabled.

* The CLI binaries are no longer published as the ``educates-client-programs``
  imgpkg bundle. Download the binaries from the GitHub release for the
  version, or use the ``educates-cli`` container image when embedding the CLI
  in a container image.

* The browser JavaScript bundles for the workshop renderer and gateway
  applications in the workshop base environment image are now generated using
  esbuild instead of browserify. This eliminates security alerts arising from
  the deprecated elliptic package which was an indirect dependency of
  browserify, but for which no fixed version is available.

* The default Hugo theme used when rendering workshop instructions has been
  replaced with a new self contained implementation which no longer depends on
  JavaScript code generated from the classic renderer. The original theme is
  still included under the name ``educates-classic`` and can be selected by
  setting the ``WORKSHOP_RENDERER_THEME`` environment variable for a workshop
  to ``educates-classic`` if any issues are encountered with the new theme. The
  recommended way of setting this environment variable is via the ``env``
  override for a workshop in the ``TrainingPortal`` resource definition. The
  ``educates-classic`` theme will be removed in a future version at the same
  time as the classic renderer is removed.

* The ``baseurl`` short code for the Hugo renderer no longer includes a
  trailing slash when expanded, so the documented usage of
  ``{{<baseurl>}}/path`` now produces a URL with a single slash. Previously the
  expansion always ended in a slash, resulting in a repeated slash. Although
  web servers will generally collapse the repeated slash when workshop
  instructions are served from a workshop session, in the case of static HTML
  files generated using ``educates render-workshop``, where URLs are relative
  to the root, the result was a URL starting with a double slash, which a
  browser would incorrectly interpret as a scheme relative URL. Note that if
  workshop instructions relied on the undocumented form ``{{<baseurl>}}path``,
  without a slash after the short code, they will need to be updated to use the
  documented form.

* If a user ends a workshop session, or attempts to return to one, after their
  training portal login session has already expired, for example after leaving
  the browser window open for a long period after the workshop session had
  finished, they are now shown the workshop session finished page rather than
  being redirected to the portal login page. Previously an expired login in
  this situation could result in the portal login page being displayed, which
  was undesirable when workshop sessions are coordinated by a custom front end
  through the training portal REST API.

* The ``helm`` CLI included in the workshop base environment image has been
  updated from the 3.x series to 4.x. Helm 4 is a new major release which
  introduces breaking changes relative to Helm 3, so any workshops which use
  the ``helm`` CLI should be verified against the newer Helm version to ensure
  they still behave as expected.

* A number of the command line tools bundled in the workshop base environment
  image have been updated to their latest upstream releases, being ``helm``
  4.2.3, ``hugo`` 0.165.0, ``skaffold`` 2.23.0 and ``kctrl`` 0.60.4. These
  updates pick up releases built with a patched Go toolchain and updated
  dependencies which resolve critical vulnerabilities reported against the
  previously bundled versions.

* The ``code-server`` release which provides the workshop editor has been
  updated from 4.125.0 to 4.131.0, bringing the embedded VS Code version from
  1.125.0 to 1.131.0. The update resolves a critical vulnerability and a number
  of high severity vulnerabilities reported against packages bundled with the
  editor.

* The Kubernetes dashboard backend used for the workshop console is now
  rebuilt from the upstream 2.7.0 sources with a current Go toolchain and
  patched dependency versions, rather than being copied as a prebuilt binary
  from the upstream container image. This resolves all critical and the
  majority of high severity vulnerabilities reported against the previously
  bundled binary. The dashboard version and its behavior within workshop
  sessions are unchanged.

* The ``vendir``, ``k9s`` and ``kustomize`` command line tools bundled in the
  workshop base environment image are now rebuilt from their upstream release
  sources with a current Go toolchain and patched dependency versions, rather
  than using the prebuilt release binaries. Each of these tools is already at
  the newest release its authors have published, and those releases still
  carry critical vulnerabilities in their bundled dependencies, so rebuilding
  is the only way to resolve them. The tool versions and their behavior are
  unchanged.

* The ``kubectl-convert`` plugin is no longer included in the workshop base
  environment image. It existed to migrate manifests away from long removed
  Kubernetes API versions, nothing in Educates itself uses it, and each of
  the four bundled copies carried vulnerabilities which only a Kubernetes
  patch release can resolve. Workshops which run ``kubectl-convert`` from
  workshop instructions or setup scripts need to install it in a custom
  workshop image, or perform the conversion outside the workshop session.

* The Zot Registry used by the OCI image cache for workshop environments has
  been updated from version 1.4.3 to 2.1.18, resolving a large number of
  critical and high severity vulnerabilities reported against the old
  version. The format of the synchronization rules able to be supplied under
  ``environment.images.registries`` in the workshop definition is unchanged.
  Any image cache contents from an existing deployment are re-indexed or
  re-synchronized on demand, so no action is required when upgrading.

* The image registry deployed for workshop sessions and for workshop
  environment image mirrors has been updated from the CNCF Distribution
  registry version 2.8.3 to 3.1.1, with the registry binary additionally
  rebuilt against a patched Go toolchain and updated dependencies so that no
  known critical or high severity vulnerabilities remain. The registry
  continues to be configured through the same environment variables, so
  workshop definitions which enable a session image registry or an image
  mirror require no changes. Note that registry 3.x no longer serves
  manifests in the legacy Docker schema 1 format, which has been deprecated
  since Docker 1.10 and does not affect images pushed by current tooling.

* The set of ``kubectl`` versions bundled in the workshop base environment
  image has changed. The 1.31 and 1.32 versions have been dropped and 1.35 and
  1.36 have been added, so the supported range is now 1.33 to 1.36. The
  ``kubectl`` version is still selected automatically to match the Kubernetes
  cluster the workshop session is connected to. For clusters older than the
  supported range the oldest bundled version, 1.33, is used, and for clusters
  newer than the supported range the most recent bundled version, 1.36, is
  used. Workshops which target clusters running Kubernetes 1.32 or older may
  therefore see a larger client/server version skew and should be verified
  against a supported cluster version.

* The JDK versions included in the ``jdk8-environment``, ``jdk11-environment``,
  ``jdk17-environment`` and ``jdk21-environment`` workshop base images have
  been updated to the latest Eclipse Temurin patch releases, being 8u502-b07,
  11.0.32+9, 17.0.20+8 and 21.0.12+8 respectively. The bundled Maven and
  Gradle versions have also been updated. All four images now include Maven
  3.9.16. The ``jdk8-environment`` and ``jdk11-environment`` images include
  Gradle 8.14.5, the latest version able to run on those JDK versions, while
  the ``jdk17-environment`` and ``jdk21-environment`` images include Gradle
  9.6.1. Gradle 9 is a new major release which introduces breaking changes
  relative to Gradle 8, so any workshops which use Gradle in the JDK 17 or
  JDK 21 images should be verified against the newer Gradle version to ensure
  they still behave as expected. One behaviour change affects all four
  images: ``gradle init`` now aborts if the target directory already
  contains any files, including hidden files, where previously it would
  generate the new project alongside them. Since the home directory of a
  workshop session is never empty, any workshop which has users run
  ``gradle init`` directly in the home directory, or any other non-empty
  directory, will need its instructions updated to either pass the
  ``--overwrite`` option to ``gradle init`` or create the project in a new
  empty subdirectory.

* The ``conda-environment`` workshop base image has been updated to use
  Miniconda 26.5.3-2, with the conda environment it provides now based on
  Python 3.14 rather than Python 3.13. The bundled ``notebook`` and
  ``jupyterlab`` packages have also been updated, to versions 7.6.1 and 4.6.2
  respectively. Workshops which install additional conda or pip packages into
  the default conda environment should be verified against the newer Python
  version in case any of the packages they depend on do not yet support
  Python 3.14.

* The ``vcluster`` software used by the ``vcluster`` workshop application has
  been updated from 0.30.2 to 0.35.2, and the set of Kubernetes versions the
  virtual cluster can provision has been aligned with the versions supported
  elsewhere in the platform. The 1.31 and 1.32 versions have been dropped and
  1.35 and 1.36 have been added, so the supported range is now 1.33 to 1.36
  with 1.36 the default. Workshops which pin the virtual cluster to Kubernetes
  1.31 or 1.32 through the ``vcluster`` application ``version`` property must be
  updated to a supported version.

* The version of ``kind`` embedded in the ``educates`` CLI has been updated
  from 0.29 to 0.32. As a result the default Kubernetes version used when
  creating a local cluster with ``educates local cluster create`` has changed
  from 1.33 to 1.36. A specific node image can still be selected using the
  ``--kind-cluster-image`` option if a different Kubernetes version is required.

* ``educates version`` now also reports the git commit the binary was built
  from, with a ``-dirty`` suffix when it was built from a modified working
  tree. This makes otherwise-identical development builds — which all report
  the floating ``latest`` or ``develop`` version — distinguishable. Release
  binaries continue to report their release version and additionally show the
  commit they were built from.

Bugs Fixed
----------

* The Contour ingress controller deployed inside a vcluster workshop session
  (when the ``vcluster`` application enables ingress) used hardcoded upstream
  Contour and Envoy image references that were not subject to image relocation
  and were absent from the published image list, so vcluster ingress could not
  work in air-gapped or registry-mirrored environments. These images are now
  first-class entries in the ``imageVersions`` inventory, so they are
  relocatable through the same per-name override mechanism as the other
  platform images and are included in the air-gap image list.

* When reserved workshop sessions were enabled for a workshop environment
  along with a timeout for deleting orphaned workshop sessions, a workshop
  session which had sat in reserve for longer than the orphaned timeout could
  be deleted moments after being allocated to a user. This was because the
  idle time reported by a workshop session is measured from when the workshop
  session was created, so the whole period a workshop session spent in
  reserve was counted as idle time. The reported idle time is now capped at
  the time which has elapsed since the workshop session was allocated to the
  user when checking whether a workshop session should be deleted due to
  being orphaned or inactive.

* The custom resource definition for the ``SecretImporter`` resource declared
  a ``sourceSecret`` property in its schema, and the documentation described
  it as a way of qualifying by name which source secret could be copied. This
  property was never consulted by the secrets manager, as the name of the
  secret a ``SecretImporter`` accepts is always determined from the name of
  the ``SecretImporter`` resource itself. The unused property has been
  removed from the custom resource definition and the documentation. If you
  had set ``sourceSecret`` in a ``SecretImporter`` resource it had no effect,
  and the field should be removed from your manifests.
