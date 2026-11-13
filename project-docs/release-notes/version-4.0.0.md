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

* In ``Managed`` mode, a wildcard TLS certificate issued outside the cluster
  can be served with the ``StaticCertificate`` certificates provider, which
  installs no cert-manager and creates no ``ClusterIssuer`` or
  ``Certificate``. ``ingress.certificates.staticCertificate.tlsSecretRef``
  references a secret holding the certificate under ``tls.crt`` and its
  private key under ``tls.key``, and the optional
  ``staticCertificate.caCertificateSecretRef``, which replaces
  ``caCertificateRef``, references a CA trust Secret holding, under
  ``ca.crt``, the certificate of the CA that issued it. Both take an optional
  ``namespace``, which defaults to the operator namespace. The operator
  copies only ``tls.crt`` and ``tls.key`` into a secret named
  ``educates-wildcard-tls`` in its own namespace, keeps the copy updated when
  the referenced secret changes, publishes it as
  ``status.ingress.wildcardCertificateSecretRef`` with no
  ``clusterIssuerRef``, and derives ``educates-ingress-ca`` from the CA trust
  Secret. Before serving the certificate or deriving ``educates-ingress-ca``
  the operator checks the certificate, so a CA it does not verify against
  never reaches Educates components, and reports each problem on the
  ``CertificatesReady`` condition with its own reason:
  ``WildcardCertificateNotFound``, ``WildcardCertificateIncomplete``,
  ``WildcardCertificateUnparseable``, ``WildcardCertificateKeyMismatch``,
  ``WildcardCertificateDomainMismatch`` when its SANs do not include
  ``*.<domain>``, ``WildcardCertificateExpired``, or
  ``WildcardCertificateUntrusted`` when its chain does not verify against the
  CA trust Secret. A certificate that expires while the cluster runs is
  reported without any change to the configuration. A local configuration
  selects the provider when a cached wildcard certificate covers its ingress
  domain, as described in the entry on local wildcard certificates; other
  installs select it through the ``EducatesConfig`` configuration kind or by
  applying the ``EducatesClusterConfig`` directly. The
  ``ExternalCertManager`` provider remains reserved and is rejected as not
  yet supported.

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
  issued in-cluster by cert-manager from a certificate authority limited to
  the cluster's ingress domain, minted from a root certificate authority
  whose private key never leaves the machine running the CLI.
  ``educates local secrets add ca NAME`` generates the root CA, or with
  ``--cert`` and ``--key`` registers an existing one, such as the root CA of
  ``mkcert``, after checking that the certificate is a CA and that the key
  matches it. The root certificate can be exported as a PEM file using
  ``educates local secrets export NAME --pem`` for importing into the
  operating system trust store, so browsers trust the workshop URLs; with
  ``--pem`` the command prints certificates only and never a private key,
  and the quick start guide includes per-platform import instructions. The
  import is needed once: the first deploy for an ingress domain mints an
  intermediate CA from the root, with a name constraint permitting only that
  domain and excluding every IP address, and copies only the intermediate's
  CA key pair into the cluster, so switching to a new local domain needs no
  new import and a key taken from a cluster cannot sign for any other site.
  The deploy prints a line whenever it mints. Later deploys for the same domain
  reuse the intermediate, and one that has expired or was minted from a root
  since replaced is minted again. A root that carries its own name
  constraints must permit the ingress domain. The local secrets cache holds
  one root CA: ``add ca`` and ``educates local secrets import`` refuse a
  second one, naming the existing root, and
  ``educates local secrets remove`` on the root removes its private key and
  every CA minted from it, so a root is replaced by removing it and
  registering the new one. With ``--cert`` alone, ``add ca`` caches a CA
  certificate for trust only.

* Local clusters can serve a wildcard certificate you already have, such as
  one created with ``mkcert``, and then install no cert-manager.
  ``educates local secrets add tls NAME --cert FILE --key FILE`` caches the
  certificate, with any intermediate certificates that follow it, and its
  private key, after checking that the certificate is not a CA, that the key
  matches it, and that it has at least one ``*.<domain>`` DNS name. A cached
  TLS certificate serves every domain it has a wildcard DNS name for, so one
  certificate created for several wildcard domains serves whichever of them
  the local configuration selects. When exactly one cached TLS certificate
  that has not expired covers ``ingress.domain``, a deploy uses the
  ``StaticCertificate`` certificates provider, even when a root CA is also
  cached. It references the certificate as
  ``staticCertificate.tlsSecretRef`` and, as
  ``staticCertificate.caCertificateSecretRef``, the cached CA certificate the
  certificate's chain verifies against, such as the root CA certificate of
  ``mkcert`` cached with ``educates local secrets add ca NAME --cert``. The
  deploy says that cert-manager will not be installed, names the certificate
  to remove with ``educates local secrets remove`` to have cert-manager
  instead, and lists the other domains the certificate covers;
  ``educates admin platform render`` shows the same as comments. It warns
  when no cached CA certificate verifies the certificate, since the training
  portal and workshop sessions would not trust the ingress. Expired TLS
  certificates are skipped with a notice, and more than one valid
  certificate for the domain is an error naming them. A version 3 local
  secrets cache holding a ``mkcert`` wildcard certificate and the CA
  certificate that issued it deploys this way as it is.

* The new ``educates local secrets view NAME`` command shows the details of
  one cached secret that do not fit the table of
  ``educates local secrets list``, so you can inspect it without decoding
  its YAML. It shows the type and domains of the secret as the list shows
  them, the file it is cached in and its Kubernetes secret type, and for a
  CA key pair the ingress domain it was minted for and its root CA. For
  each certificate the secret holds, those of ``tls.crt`` and then those of
  ``ca.crt``, it shows the common name, subject, issuer, subject
  alternative names, validity, whether it is a CA and its path length, the
  name constraints of a CA, the serial number and the SHA-256 fingerprint.
  It never prints key material: it shows only whether the secret holds a
  private key, the registries of registry credentials and the names of the
  keys of a generic secret. For an ``invalid`` secret it shows why it is
  invalid and the ``educates local secrets remove`` command that removes
  it. For a CA cached with its private key by a 4.0.0 alpha release, it
  first says to register the CA as the root CA under another name with
  ``educates local secrets add ca ROOT --cert FILE --key FILE``, since
  removing the secret removes the CA's private key.

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
  are routed to those components' own custom resources, and entries named
  ``session-manager``, ``image-puller`` and ``node-ca-injector`` override
  the images they refer to. For
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
  versions are ``1.34``, ``1.35``, ``1.36`` and ``1.37``, defaulting to
  ``1.36``. The default deliberately tracks the second newest supported
  version, trading recency for stability. The version selects a matching Kind
  node image; an explicit ``--kind-cluster-image`` still takes precedence for
  pinning an arbitrary image.

* The local Kind cluster can now be multi-node. The ``EducatesLocalConfig``
  ``cluster.nodes`` list declares the cluster's nodes, each with a ``role``
  (``control-plane`` or ``worker``), optional Kubernetes node ``labels``, and
  optional ``taints``, so a laptop cluster can model node pools for workshops
  that schedule across several nodes. Leaving ``cluster.nodes`` unset keeps
  the default single control-plane cluster. Every node pulls from the local
  image registry and the local registry mirrors: ``educates local cluster
  create``, ``educates local registry deploy`` and ``educates local mirror
  deploy`` configure each node, and ``educates local mirror delete`` removes
  the mirror's configuration from each node. ``educates local cluster stop``
  and ``educates local cluster start`` stop and start every node, and
  ``educates local cluster status`` reports a cluster with only some of its
  nodes running as partly running, naming the nodes which are not.

* ``educates local cluster status`` now also reports whether Educates is
  installed on the local cluster and whether its platform components are
  ready. It reads the ``EducatesClusterConfig``, ``SecretsManager``,
  ``LookupService`` and ``SessionManager`` custom resources and reports one
  of three outcomes: a cluster-only cluster (kind running, Educates not
  installed), an installed-but-not-ready cluster (one or more components not
  yet ``Ready``), or a fully installed and ready cluster — making it easy to
  tell a working install from a partial or broken one.

* Several local clusters can now run at the same time. Next to the default
  local cluster, ``educates local cluster create --local-cluster test2
  --listen-address 10.10.10.2`` creates the named local cluster ``test2``:
  the Kind cluster ``educates-test2``, with the kube context
  ``kind-educates-test2``, publishing ports 80 and 443 on the host IP
  address ``10.10.10.2``, with Educates installed and reachable at
  ``10-10-10-2.nip.io``. A named cluster keeps its configuration in
  ``clusters/<name>/config.yaml`` in the data home, which ``create`` writes
  from ``--listen-address`` and the optional ``--domain`` when it is
  missing. Its ``cluster.listenAddress`` must be a specific IP address of
  the host, neither ``0.0.0.0`` nor a loopback address, and without an
  ``ingress.domain`` the cluster uses the ``nip.io`` name of that address
  and is served over plain HTTP. The ``create``, ``delete``, ``start``,
  ``stop`` and ``status`` commands of ``educates local cluster`` select a
  cluster with ``--local-cluster``, or for a whole terminal with the
  ``EDUCATES_LOCAL_CLUSTER`` environment variable, and act on the default
  cluster without either. Deleting a named cluster keeps its
  configuration, and every cluster pulls from the one local image registry
  on ``localhost:5001``. The platform install of ``educates local cluster
  create`` now always targets the cluster it has just created, whatever
  ``KUBECONFIG`` or the current kube context say.

  ``educates local cluster list`` shows every local cluster, the default
  one marked, with its state, listen address, ingress domain and kube
  context. A Kind cluster named ``educates-<name>`` counts as a local
  cluster only when its folder ``clusters/<name>/`` exists in the data
  home, so Kind clusters made by hand are left out of the list and of every
  check. Before ``create`` creates a cluster, and before ``start`` starts
  one, its listen address is checked against every other local cluster,
  running or stopped: an address another cluster also uses, a cluster on
  ``0.0.0.0`` beside any other, and an address on no network interface of
  the host are refused with an error naming the other cluster. ``educates
  local cluster delete --all`` now deletes the local image registry, the DNS
  resolver and the registry mirrors only when no other local cluster
  remains, and otherwise keeps them and names the clusters still using
  them.

  Every local cluster shares the local image registry and the registry
  mirrors, so the commands which manage them take no ``--local-cluster``
  and act on every local cluster. ``educates local registry deploy``
  configures every node of every cluster to pull from the registry, and
  points each cluster's ``registry`` Service and ``local-registry-hosting``
  ConfigMap at it. ``educates local mirror deploy`` links a mirror into
  every node of every cluster, and ``educates local mirror delete`` unlinks
  it from all of them. A cluster whose configuration declares a registry
  mirror reuses the mirror's container when another cluster created it,
  and ``educates local cluster create`` fails before creating the Kind
  cluster when that container pulls from another URL or as another
  username, naming the mirror, the container's values and the declared
  ones. A different password can't be seen on the container, so it is not
  detected.

* An extension package listed under ``spec.workshop.packages`` of a workshop
  definition can now be declared as a published image, by setting ``image`` to
  a tagged image reference instead of listing ``files``. The two are mutually
  exclusive. An optional ``imagePullPolicy`` and an optional
  ``pullSecretRef``, naming a secret also listed in
  ``spec.environment.secrets``, can accompany it. The image reference may use
  the ``$(image_repository)``, ``$(workshop_name)`` and
  ``$(workshop_version)`` variables, must carry a tag, and cannot be a digest.
  A package declared this way needs a name which is a valid DNS label, as the
  name is used when the package is delivered to a workshop session. Where the
  package image is mounted and declares no ``imagePullPolicy``, it is given
  the same pull policy as the workshop image, which is ``Always`` for an image
  with the ``:main``, ``:master``, ``:develop`` or ``:latest`` tag, and
  ``IfNotPresent`` otherwise.

* A new ``educates package publish`` command, also available as ``educates
  publish-package``, builds and publishes an extension package as an image. It
  takes a package source directory holding a ``package.yaml`` manifest beside
  the reserved ``common``, ``linux-amd64`` and ``linux-arm64`` directories, and
  publishes an image index with one child image per platform. No Docker daemon
  is required. Two builds of the same source with the same version of the CLI
  produce the same digests, and ``SOURCE_DATE_EPOCH`` is honoured where a
  specific build timestamp is wanted.
  Use ``--dry-run`` to validate a package source and see the digests which
  would be published without contacting a registry, and ``--digest-file`` to
  record the published digest for a later step in a pipeline. A package whose
  files have to be built for each architecture, such as a program compiled
  from source, can instead be built with a ``Dockerfile`` and ``docker
  buildx``, and the documentation describes when to use each.

* On a cluster which supports it, an extension package declared as an image is
  now mounted into a workshop session read only, rather than having its
  contents downloaded. The workshop definition is the same either way, and the
  platform chooses: the delivery is settled when the workshop environment is
  created and every session of that environment uses it, so changing the
  platform setting affects new environments only. A package held in a registry
  requiring authentication is pulled using the secret named by
  ``pullSecretRef``, which must also be listed under
  ``spec.environment.secrets``.

* ``EducatesClusterConfig`` gains ``packageDelivery.imageMount``, controlling
  whether an extension package declared as an image is mounted into a workshop
  session or has its contents fetched. The default, ``Auto``, mounts only when
  the cluster supports it: the API server must be 1.36 or newer, and every
  Linux node must report a kubelet of 1.35 or newer together with
  ``containerd`` 2.1 or newer or ``CRI-O`` 1.31 or newer. Where any of those is
  not met the package contents are fetched instead, which works on any
  cluster. ``Enabled`` mounts regardless, for a cluster whose support the
  operator cannot detect, and ``Disabled`` always fetches. The resolved value
  is published in ``status.packageDelivery.imageMount``, and a
  ``PackageImageMountAvailable`` condition explains the outcome, naming the
  API server version or the nodes which fall short. That condition is
  reported separately from readiness, as fetching is a valid steady state
  rather than a fault. The resolution is re-evaluated when a node joins,
  leaves, or is upgraded.

* A ``bin`` directory contained in an installed extension package is now added
  to the application search path defined by the ``PATH`` environment variable,
  so a program the package ships can be run by name without the package having
  to set this up itself. This applies to a package declared with ``files`` as
  well as one declared as an image. Packages are set up in the alphabetical
  order of their names, and a package's ``bin`` directory is added just before
  its own setup scripts run and then stays on the search path, so a setup
  script can run programs from its own package and from packages set up before
  it, but never from a package not yet set up. Each ``bin`` directory is added
  at the front of the search path, so where two packages ship a program of the
  same name, the one from the package whose name sorts last is found. Running
  ``rebuild-workshop`` again does not add a directory to the search path a
  second time. An existing package which ships a ``bin`` directory and puts it
  on the search path from a ``profile.d`` script, as the packages in the
  ``educates-extension-packages`` repository do, now has it added as well, so
  that line is no longer needed. A package which only creates its ``bin``
  directory from one of its setup scripts has nothing added for it, and still
  puts the directory on the search path itself.

* Each extension package declared as an image is now checked when a workshop
  session starts, and one line per package is written to
  ``download-workshop.log`` naming the package and whether it was mounted or
  fetched. A package which did not arrive, most commonly because the image
  reference names an ordinary application image rather than an image built as
  an extension package, is reported there and an error dialog is shown on the
  workshop session dashboard, rather than the session starting without the
  package. Every package is checked before the session reports a failure, so
  one package which did not arrive does not hide the state of the others or
  stop the remaining packages' setup scripts from running.

* An extension package declared as an image is now delivered when deploying a
  workshop with ``educates docker workshop deploy``, where previously such a
  package was not delivered at all. It is mounted into the session read only
  where the local Docker daemon supports it, which requires Docker Engine
  28.0 or newer, Docker Compose 2.35 or newer and the containerd image store,
  and its contents are fetched where it does not. A current Docker Desktop
  meets all three. The chosen delivery is printed, along with the reason when
  it falls back, and one line names each package. A new
  ``--package-delivery`` option takes ``auto``, ``image-mount`` or ``fetch``,
  so an author can force either delivery. Asking for a mount on a daemon
  which cannot do it fails before the workshop is deployed, naming what is
  missing.

* The ``imagePullPolicy`` of an extension package declared as an image is now
  honoured by ``educates docker workshop deploy`` where the package is
  mounted. ``Always`` pulls the image before deploying, which matters when
  republishing the same tag while working on a package locally, and ``Never``
  reports an error before deploying when the image is not already held
  locally. A package which declares no ``imagePullPolicy`` is given the same
  default as on a cluster, which is ``Always`` for an image with the
  ``:main``, ``:master``, ``:develop`` or ``:latest`` tag, and
  ``IfNotPresent`` otherwise. If a pull fails but a copy of the image is
  already held locally, a warning is printed and that copy is used. Where the package contents are fetched instead, the
  policy continues to be ignored. Note that ``pullSecretRef`` is ignored when
  deploying with Docker, so an image held in a registry requiring
  authentication needs ``docker login`` for that registry first. A pull which
  fails for want of credentials now names the image, the registry and the
  command to run.

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

* The ``educates workshop render`` command accepts a ``--pathway`` option
  naming the pathway in ``workshop/config.yaml`` to render, in place of the
  one named by the ``PATHWAY_NAME`` environment variable in ``session.env`` of
  the workshop definition, or else ``pathways.default``. The rendered
  instructions use that pathway as a workshop session started with
  ``PATHWAY_NAME`` set to it would: its ``steps`` order the pages, its
  ``params`` apply on top of the top-level ones, its ``title`` and
  ``description`` replace those of the workshop definition, and the
  ``pathway_name`` data variable is set to its name. This lets an author
  render each pathway of a workshop without editing the workshop definition.
  A name which ``pathways.paths`` does not define, or any name when
  ``workshop/config.yaml`` defines no pathways, fails with an error listing
  the pathways that are defined.

* The image pre-puller can now pull images beyond its default images, the
  training portal and the workshop base environment. The ``imagePrePuller``
  block of the ``SessionManager`` custom resource gains
  ``includeDefaultImages``, which defaults to ``true`` and drops the default
  images when set to ``false``, and ``images``, a list of further images to
  pull. Each entry gives either a ``name``, the short name of an image in the
  Educates image inventory such as ``jdk21-environment``, or an ``image``, a
  full image reference. A ``name`` resolves to the reference the installation
  runs, with ``spec.images.overrides`` and the image registry prefix applied,
  so an Educates image follows the installed version on upgrade. An ``image``
  is pulled exactly as written. Images are pulled in order, the default images
  first and then each entry as listed, and an image already in the list is
  pulled once. An entry giving both or neither of ``name`` and ``image``, and
  a pre-puller with ``enabled`` set to ``true``, ``includeDefaultImages`` set
  to ``false`` and no ``images``, are rejected when the resource is applied. A
  ``name`` the inventory does not hold leaves the session-manager release as
  it was, and the ``SessionManager`` reports ``Ready`` as ``False`` with
  reason ``InstallFailed`` and a message naming the entry. ``enabled`` remains
  the only switch: while it is ``false`` nothing is pulled, and the list can
  stay in place. See [pre-pulling workshop
  images](pre-pulling-workshop-images).

Features Changed
----------------

* When deploying a workshop with ``educates docker workshop deploy``, a
  workshop image using the ``:main``, ``:master``, ``:develop`` or ``:latest``
  tag, or no tag, is now pulled each time the workshop is deployed, as it is
  when the workshop is deployed to a cluster. Previously a copy of the image
  already held by the local Docker daemon was always used, so a newer version
  pushed under the same tag was not picked up. If the pull fails but a copy of
  the image is already held locally, a warning is printed and that copy is
  used, so a workshop can still be deployed without access to the registry.

* An extension package declared as an image is now delivered to a workshop
  session by a purpose built package fetcher rather than by ``vendir``. The
  fetcher resolves a multi architecture image to the architecture the session
  runs on, so an author writes one image reference whichever architecture the
  node uses, and it preserves the file permissions the package was published
  with, which ``vendir`` does not. Packages declared with ``files`` continue to
  be downloaded by ``vendir`` exactly as before, and a workshop can mix the
  two.

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

* ``educates local secrets list`` now shows what each cached secret is and
  what a deploy uses it for, where it previously printed only names. It
  prints a table with the columns ``NAME``, ``TYPE``, ``DOMAINS`` and
  ``EXPIRES``. ``TYPE`` is the role of the secret, decided the same way a
  deploy decides it: ``CA certificate``, ``CA root key pair``,
  ``CA intermediate key pair``, ``TLS certificate``,
  ``Registry credentials`` or ``Generic``. A secret to remove from the cache
  is shown as ``invalid`` while the rest of the list still prints: a file
  that cannot be read or decoded, or a CA certificate with its private key
  that is neither a CA root key pair nor a CA key pair, such as a CA cached
  by a 4.0.0 alpha release, which a deploy does not use. ``DOMAINS`` is
  what the secret is valid for: every wildcard domain of a TLS certificate,
  the ingress domain of a CA key pair, and the permitted DNS name
  constraints of a CA certificate or CA root key pair, or ``any`` when it
  has none. ``EXPIRES`` is the date the certificate expires, or
  ``expired``. The new ``educates local secrets view NAME`` command shows
  the details of one secret, such as the subject and issuer of its
  certificates or why it is ``invalid``.

* The bundled ``external-dns`` cluster service has been updated to version
  0.22.0. This version only recognizes annotations carrying the
  ``external-dns.kubernetes.io/`` prefix and no longer reads the older
  ``external-dns.alpha.kubernetes.io/`` prefix. The operator now annotates
  the Envoy service of the ingress controller with both
  ``external-dns.kubernetes.io/hostname`` and the older
  ``external-dns.alpha.kubernetes.io/hostname``, so the wildcard DNS record
  for the ingress domain is published whether it is picked up by the bundled
  ``external-dns`` or by an ``external-dns`` you run yourself, including
  versions before 0.22. If you annotate your own ``Service`` or ``Ingress``
  resources for the bundled ``external-dns`` to act on, for example when its
  ``sources`` include ``ingress``, those annotations must be changed to the
  new prefix. Otherwise ``external-dns`` no longer sees those resources and
  removes the DNS records it previously created for them.

* The ``dnsmasq`` image run by ``educates local resolver deploy`` has been
  updated from version 2.90 to 2.93. The configuration the CLI generates for
  the local DNS resolver is unchanged. A resolver container created by an
  earlier release keeps the image it was created with, since neither
  ``educates local resolver deploy`` nor ``educates local resolver update``
  replaces an existing resolver container. To move it to the new image, run
  ``educates local resolver delete`` followed by ``educates local resolver
  deploy``.

* The table of contents for workshop instructions rendered with the default
  ``educates`` Hugo theme is now a panel which slides up from behind the
  navigation bar at the bottom of the instructions, instead of a pop-up
  dialog. The table of contents button in the navigation bar opens and closes
  the panel, which lists every page with its step number and highlights the
  page currently being viewed. Clicking anywhere on the entry for a page
  navigates to that page. When viewing the workshop instructions in the
  workshop dashboard, pages which have not yet been visited remain dimmed and
  cannot be selected, as before. When a workshop has more pages than fit in
  the panel, the list scrolls, with a scroll bar that is always shown so it is
  apparent there are further pages. The panel can also be closed using the
  ``Escape`` key or by clicking outside of it. A custom
  ``workshop-instructions.css`` file which styled the table of contents
  through the Bootstrap modal classes needs to be updated. The panel keeps
  the ``table-of-contents`` element ID, and the ``modules``, ``page`` and
  ``toc-step`` classes for its list of pages. The colors of the panel, and of
  the navigation bar at the bottom of the instructions, are taken from the
  Bootstrap theme variables, so a custom theme which sets the Bootstrap
  primary color using ``--bs-primary`` and ``--bs-primary-rgb`` also changes
  the highlight for the current page, the step progress indicator, and the
  step number shown with the page title.

* The ``ClusterRole`` and ``ClusterRoleBinding`` resources created by Educates
  have been renamed to follow the Kubernetes convention of an application
  prefix separated by colons. Service roles are now named after their
  component: ``educates:session-manager``, ``educates:secrets-manager``,
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

* The ``session-manager``, ``lookup-service`` and ``training-portal``
  containers no longer rely on an init container running as ``root`` to
  install the cluster ingress CA into the operating system trust store when a
  private certificate authority is in use. The CA secret is instead mounted
  into the container and, on start-up, appended to a copy of the system CA
  bundle at ``/opt/app-root/ca-bundle.crt``. The ``SSL_CERT_FILE``,
  ``REQUESTS_CA_BUNDLE``, ``CURL_CA_BUNDLE``, ``GIT_SSL_CAINFO`` and
  ``NODE_EXTRA_CA_CERTS`` environment variables are set in the container
  images to point at that bundle, so the CA is trusted by the application and
  also by tools such as ``curl`` when using ``kubectl exec`` to debug from
  inside the container. The ``secrets-manager`` and ``tunnel-manager`` images
  carry the same environment variables for consistency. The same mechanism
  applies regardless of whether a CA is configured, in which case the bundle
  holds only the system roots. Workshop session containers are unaffected and
  continue to use an init container, as the many command line tools they
  provide need the CA in the operating system trust store.

* Every pod that mounts the CA trust Secret of the cluster ingress, being the
  ``session-manager``, ``lookup-service`` and ``node-ca-injector``
  deployments, the training portal and workshop sessions, now mounts only its
  ``ca.crt`` key, the only key these components read. Previously the
  ``session-manager``, ``lookup-service`` and ``node-ca-injector``
  deployments and the training portal mounted every key the secret held, so
  a CA secret which also carried a private key exposed that key inside their
  containers. The CA trust Secret is still mounted as a directory rather than
  as a single file, so a replaced CA certificate is picked up when the pod
  next restarts.

* The container images for the ``secrets-manager``, ``session-manager``,
  ``lookup-service``, ``tunnel-manager`` and ``training-portal`` components
  are now based on Debian (``debian:trixie-slim``) instead of Fedora, and the
  ``image-cache`` image is now based on a distroless base image. The Python
  components run on a Python interpreter installed by ``uv`` rather than the
  distribution's Python, and are built in two stages so that the published
  images contain no compiler, package build tooling or ``uv`` binary. The
  ``training-portal`` image uses the Debian Apache HTTPD packages for
  ``mod_wsgi``. Debian stable receives security updates for a given release
  for far longer than a Fedora release, so the base image no longer needs to
  be moved forward twice a year. The images are smaller as a result of these
  changes. The workshop base environment image and the images derived from it
  remain based on Fedora for now.

* How each cluster service of an ``EducatesClusterConfig`` is provided can no
  longer be changed once the resource exists, in the same way as
  ``spec.mode``. The Kubernetes API server rejects an update that changes the
  certificates provider, ``spec.ingress.certificates.provider``, the ingress
  controller provider, ``spec.ingress.controller.provider``, the DNS provider,
  ``spec.dns.provider``, or the policy enforcement settings, which are the
  ``clusterPolicy`` and ``workshopPolicy`` engines and ``kyverno.provider``
  under ``spec.policyEnforcement``. It also rejects removing ``spec.ingress``
  once it has been set, adding or removing a ``spec.dns`` whose provider is not
  ``None``, and adding or removing ``spec.policyEnforcement`` or its
  ``kyverno`` block. Switching how a service is provided in place could leave
  a bundled cluster service installed beside its replacement, or certificates
  half switched. Settings within a bundled service, such as the Route53
  options of the bundled external-dns, can still be changed. To change how a
  service is provided, delete the ``EducatesClusterConfig`` and create it
  again, for example with ``educates admin platform delete`` followed by a new
  deploy, or install into a new cluster. When ``educates admin platform
  deploy`` would make such a change to an existing installation, for example
  because ``ingress.insecure`` was set or cleared in the local configuration,
  it now fails with a message naming the setting that cannot change and
  pointing at ``educates admin platform delete`` or recreating the cluster.

* The custom CA of the ``BundledCertManager`` certificates provider in
  ``Managed`` mode is now named by
  ``ingress.certificates.bundledCertManager.customCA.caKeyPairSecretRef``,
  which replaces ``caCertificateRef``. It references the CA key pair
  cert-manager signs the wildcard certificate with: a secret holding the CA
  certificate under ``tls.crt`` and its private key under ``tls.key``, and
  optionally the root certificate under ``ca.crt``. The operator now checks
  that the secret holds both keys and that its certificate is a CA before
  cert-manager is involved, and reports a problem on the
  ``CertificatesReady`` condition with a reason naming it, being
  ``CAKeyPairNotFound``, ``CAKeyPairIncomplete``, ``CAKeyPairUnparseable``
  or ``CAKeyPairNotCA``. A new optional field,
  ``customCA.caCertificateSecretRef``, references a CA trust Secret holding,
  under ``ca.crt``, the certificate Educates components should trust, for
  example the root of an intermediate CA used as the key pair. Both
  references take an optional ``namespace``, which defaults to the operator
  namespace.

* Whenever a CA is configured, being a custom CA or a CA trust Secret named
  by ``staticCertificate.caCertificateSecretRef`` in ``Managed`` mode, or a
  CA trust Secret named by ``inline.ingress.caCertificateSecretRef`` in
  ``Inline`` mode, Educates components now receive the CA certificate
  through a secret named ``educates-ingress-ca`` that the operator creates
  and owns in its own namespace, and publishes as
  ``status.ingress.caCertificateSecretRef`` of the
  ``EducatesClusterConfig``. The secret holds a single key, ``ca.crt``, and
  never a private key, whatever the source secret holds. The certificate
  comes from the ``ca.crt`` key of the CA trust Secret when one is
  referenced. A custom CA without one uses the ``ca.crt`` key of the CA key
  pair, otherwise the root-most certificate of the key pair's ``tls.crt``
  chain. Any other key a CA trust Secret holds is ignored, so an ``Inline``
  mode CA secret that also holds a private key keeps working without the key
  reaching any component. The secret is updated when its source secret
  changes. A referenced CA trust Secret that is missing, has no ``ca.crt``
  key, or whose ``ca.crt`` does not parse sets the ``CertificatesReady``
  condition to ``False`` with the reason ``CATrustSecretNotFound``,
  ``CATrustSecretIncomplete`` or ``CATrustSecretUnparseable``. Previously no
  CA was published in ``Managed`` mode, so the session manager, lookup
  service, training portals, workshop sessions and cluster nodes did not
  trust certificates issued from a custom CA, and workshops calling their
  own ingress over HTTPS failed, while ``Inline`` mode published the
  referenced CA secret itself, with whatever keys it held. With a custom CA
  the CA's private key now stays in the namespace of the CA key pair and in
  the ``cert-manager`` namespace.

* The ``inline.ingress.wildcardCertificateSecretRef`` field of the
  ``EducatesClusterConfig`` now takes an optional ``namespace``, which
  defaults to the operator namespace, as
  ``inline.ingress.caCertificateSecretRef`` already did, so the wildcard
  certificate can be kept outside the operator namespace.
  ``status.ingress.wildcardCertificateSecretRef`` names the referenced secret
  with its namespace, and changes to either referenced secret are reflected
  in the status. The ``EducatesInlineConfig`` configuration kind still names
  secrets in the operator namespace; use the ``EducatesConfig`` escape hatch
  to name another namespace.

* In ``Inline`` mode the wildcard certificate named by
  ``inline.ingress.wildcardCertificateSecretRef`` is now checked in the same
  way as one served with the ``StaticCertificate`` provider, against the CA
  trust Secret named by ``inline.ingress.caCertificateSecretRef`` when there
  is one. Previously only the presence of its ``tls.crt`` and ``tls.key``
  keys was checked. ``Inline`` mode now also sets the ``CertificatesReady``
  condition: ``True`` once the wildcard certificate and CA trust Secret pass
  validation, and ``False`` with the same reasons as ``StaticCertificate``
  when the certificate is not usable, or with a reason such as
  ``CATrustSecretIncomplete`` when the CA trust Secret is not. A wildcard
  certificate that does not cover ``*.<domain>``, has expired, or was not
  issued by the named CA now leaves the ``EducatesClusterConfig`` not ready.

* The ``ingressOverrides.caCertificateSecretRef`` field of the
  ``SessionManager`` resource, which names a CA trust Secret, now takes an
  optional ``namespace``, which defaults to the operator namespace.
  Previously the secret was looked up in the namespace of the cluster's
  wildcard certificate. The session manager no longer receives the
  referenced secret itself: the operator copies its ``ca.crt`` key, and
  nothing else, into a secret named ``educates-session-manager-ingress-ca``
  that it creates and owns in its own namespace, keeps it updated when the
  referenced secret changes, and passes that secret to the session manager
  in place of ``educates-ingress-ca``. When the referenced secret is missing
  or holds no valid certificate under ``ca.crt``, the ``SessionManager``
  reports ``Ready`` as ``False`` with the same reasons as a CA trust Secret
  of the ``EducatesClusterConfig``, being ``CATrustSecretNotFound``,
  ``CATrustSecretIncomplete`` or ``CATrustSecretUnparseable``.

* With ``nodeCATrust.mode`` left at ``Auto`` in the ``SessionManager``
  resource, the node CA injector is now installed on every cluster that
  publishes a CA: a ``Managed`` mode cluster with a custom CA, a
  ``StaticCertificate`` cluster that names a CA trust Secret, and an
  ``Inline`` mode cluster that names a CA trust Secret. This includes every
  local cluster whose certificates are issued from a cached root CA, and
  every local cluster serving a cached wildcard certificate that a cached CA
  certificate verifies, since such a cluster now publishes its CA. Cluster
  nodes then trust registries served through the cluster ingress, such as
  the image registry of a workshop session. A ``SessionManager`` that sets
  ``ingressOverrides.caCertificateSecretRef`` also gets the node CA
  injector, even on a cluster that publishes no CA, and nodes then trust the
  override's ``educates-session-manager-ingress-ca`` in place of the
  cluster's CA, the same CA the session manager and the workshop sessions it
  creates trust. With ``nodeCATrust.mode`` set to
  ``Enabled``, the ``SessionManager`` reports ``Ready`` as ``False`` only
  when neither the cluster nor an override provides a CA. Set
  ``nodeCATrust.mode`` to ``Disabled`` to keep the privileged node CA
  injector daemon set off.

* Workshop images published by ``educates workshop publish`` are now OCI
  images: a single OCI image manifest, with an OCI image config and one OCI
  layer holding the workshop files at its root, in place of the Docker media
  types used before. In the layer, files are ``0755`` when they have any
  execute bit set and ``0644`` otherwise, directories are ``0755``,
  everything is owned by ``root``, and every entry carries the time given by
  ``SOURCE_DATE_EPOCH``, or 1 January 2000 when it is not set. A directory
  holding no files is left out. Publishing unchanged content again with the
  same version of the CLI gives the same digest, but the first publish with
  this version gives a different digest from a 3.x publish of the same
  content.

* ``educates workshop publish`` now leaves ``.DS_Store`` files and ``._*``
  AppleDouble files out of the workshop image, wherever they are in the
  workshop files. A symbolic link or a hard-linked file in the workshop files
  fails the publish, and the error names every one found, so they can all be
  fixed at once. Hard-linked files were previously published as separate
  copies.

* ``educates workshop publish`` now pushes the workshop image under the
  requested tag only. The extra ``sha256-<hex>.imgpkg`` tag that was also
  written to the image repository is no longer created.

* In the ``includePaths``, ``excludePaths`` and ``legalPaths`` patterns of
  ``spec.publish.files``, a class written as ``[!x]`` now matches any
  character other than ``x``, the same as ``[^x]``, where it previously
  matched ``!`` or ``x``. A pattern ending in ``dir/**`` now also matches a
  file named ``dir``, as well as the files under a directory of that name.
  Every other pattern matches the same files as before.

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
  custom ingress domain, ``educates local cluster create`` stops before
  creating anything when neither a root CA nor a wildcard certificate for the
  domain is cached, and prints the commands that generate a root CA,
  register the root CA of ``mkcert``, or cache a ``mkcert`` wildcard
  certificate. A local configuration with no ingress domain set instead
  defaults to a plain HTTP install on a ``<host-IP>.nip.io`` address and
  needs no CA, since that address cannot obtain a trusted certificate. Set
  ``ingress.insecure`` to request plain HTTP explicitly for a custom domain.
  See the New Features entry on installing without in-cluster TLS for the
  full picture.

* The local secrets cache stores certificate authorities in a new shape,
  and ``educates local secrets add ca`` and ``educates local secrets add tls``
  no longer take a ``--domain`` option: a root CA serves every ingress
  domain, a TLS certificate serves the domains of its wildcard DNS names,
  and a deploy decides the domain from ``ingress.domain``.
  ``educates local secrets add tls`` now requires both ``--cert`` and
  ``--key`` and refuses a certificate it cannot serve. A root CA named
  ``NAME`` is cached as the CA trust Secret ``NAME``, an ``Opaque`` secret
  holding the root certificate as ``ca.crt``, and the CA root key pair
  ``NAME-root-key-pair``, a ``kubernetes.io/tls`` secret holding the root
  certificate and its private key, which is never copied to a cluster. Each
  ingress domain gets the CA key pair ``<domain>-ca-key-pair``, a
  ``kubernetes.io/tls`` secret holding the intermediate CA followed by the
  root as ``tls.crt``, the intermediate's private key as ``tls.key`` and the
  root as ``ca.crt``, annotated with ``training.educates.dev/domain`` and
  ``training.educates.dev/root-ca``. A local deploy references the CA key
  pair as ``customCA.caKeyPairSecretRef`` and the root's CA trust Secret as
  ``customCA.caCertificateSecretRef``. A deploy and
  ``educates local secrets sync`` now copy into the ``educates-secrets``
  namespace every cached secret except CA root key pairs and any other
  secret holding a CA private key, apart from the CA key pair selected for
  the ingress domain. ``educates local secrets sync`` selects that CA key
  pair from the ``ingress.domain`` of the local configuration, minting it
  when needed, and without a local configuration copies none. When the
  certificates of the local configuration cannot be decided, such as a
  secure ``ingress.domain`` with neither a root CA nor a wildcard
  certificate for it cached, ``educates local secrets sync`` warns why and
  still copies every secret that holds no CA private key, while a deploy of
  that configuration fails.

  CAs cached by 4.0.0 alpha releases, ``kubernetes.io/tls`` secrets holding
  a CA certificate and its private key, are not migrated automatically, and
  a deploy no longer uses them: ``educates local secrets list`` shows them as
  ``invalid``. To keep a CA you have already imported into your trust store,
  re-register it as the root CA, then remove the old entry. With ``NAME``
  the name of the cached CA and ``ROOT`` a name for the root CA other than
  ``NAME``, such as ``educates-ca``, export the certificate and key of the
  cached CA, register them as the root CA, delete the exported key and
  remove the old entry:

  ```
  educates local secrets export NAME --pem > NAME.crt
  educates local secrets export NAME | awk '$1 == "tls.key:" {print $2}' | base64 --decode > NAME.key
  educates local secrets add ca ROOT --cert NAME.crt --key NAME.key
  rm NAME.key
  educates local secrets remove NAME
  ```

  If the CA was the root CA of ``mkcert``, register it straight from
  ``mkcert`` instead, then remove the old entry:

  ```
  educates local secrets add ca ROOT --cert "$(mkcert -CAROOT)/rootCA.pem" --key "$(mkcert -CAROOT)/rootCA-key.pem"
  educates local secrets remove NAME
  ```

  The cache holds one root CA, so if you cached a CA for each of several
  domains, re-register only one of them and remove the others with
  ``educates local secrets remove``: deploys for their domains mint their
  CAs from the root. Remove those CAs from your trust store too. Then delete
  and recreate the local cluster with ``educates local cluster delete`` and
  ``educates local cluster create``: an alpha cluster received the CA's
  private key, and still holds it in the old CA secret.

* The version 3 ``imagePuller`` configuration setting is replaced by
  ``imagePrePuller``, and in the CLI configuration kinds
  (``EducatesLocalConfig``, ``EducatesGKEConfig``, ``EducatesEKSConfig`` and
  ``EducatesInlineConfig``) it is now an object, with the same ``enabled``,
  ``includeDefaultImages`` and ``images`` fields, and the same meaning, as the
  ``imagePrePuller`` block of the ``SessionManager`` custom resource. The CLI
  passes the block to the ``SessionManager`` as written. The pre-puller is
  enabled by default in these kinds, so installs created with the CLI
  pre-pull the training portal and workshop base environment images onto
  every node ahead of time unless ``imagePrePuller.enabled`` is set to
  ``false``, which ``educates local config set imagePrePuller.enabled false``
  does for the local configuration. A ``SessionManager`` custom resource
  applied directly, ``EducatesConfig``, which passes the custom resource on as
  written, and the runtime Helm chart installed without the operator keep the
  default of disabled. The CLI rejects, when it loads the configuration, an
  ``images`` entry giving both or neither of ``name`` and ``image``, and
  ``includeDefaultImages`` set to ``false`` with no ``images`` while
  ``enabled`` is ``true`` or left unset. The boolean form is no longer
  accepted: a configuration holding ``imagePrePuller: true`` or
  ``imagePrePuller: false`` fails to load with an error naming the object
  form. Write ``imagePrePuller: {enabled: false}`` in place of
  ``imagePrePuller: false``, and drop ``imagePrePuller: true``, which is the
  default. See [pre-pulling workshop images](pre-pulling-workshop-images).

* The CLI's first-run migration of a version 3 ``values.yaml`` carries
  ``imagePuller`` over to ``imagePrePuller``, so the pre-puller keeps its
  switch and pulls the same images. ``imagePuller.enabled`` becomes
  ``imagePrePuller.enabled``. A ``prePullImages`` list that includes
  ``base-environment``, or no list, keeps the default images and adds each
  of its other names as a ``name`` entry of ``imagePrePuller.images``. Any
  other list, an empty one included, sets ``includeDefaultImages`` to
  ``false`` and lists ``training-portal`` followed by its names, the images
  version 3 pulled. A ``prePullImages`` name the version 4 image inventory
  does not hold is dropped: ``session-manager``, ``secrets-manager``,
  ``lookup-service``, ``node-ca-injector`` and ``pause-container`` always,
  and ``loftsh-kubernetes-v1.31`` and ``loftsh-kubernetes-v1.32`` unless
  ``imageVersions`` defines them. An ``imageVersions`` entry named
  ``pause-container`` is dropped too. The migration's notice lists the
  ``imagePrePuller`` fields it set and each entry it dropped. A version 3
  file without ``imagePuller`` leaves ``imagePrePuller`` unset, so the
  defaults apply. See [laptop installs migrate
  automatically](laptop-installs-migrate-automatically).

* The image pre-puller no longer runs anything from the images it pulls.
  Each pulled image used to run its own ``/bin/true``, so an image without
  one, such as a distroless or ``scratch`` image, failed to start and stopped
  every image after it from being pulled. The pre-puller now has a small
  helper of its own, ``image-puller``, which a first init container copies
  into a shared volume and every pulled image runs from there, and which is
  also the long-running container in place of the pause container. The
  ``educates-pause-container`` image is no longer built or published, and is
  replaced by ``educates-image-puller``, which the release image list
  includes for mirroring. An image override named ``image-puller``, in
  ``imageVersions`` in the CLI configuration or in
  ``SessionManager.spec.images.overrides``, sets the helper image and takes
  a ``repository:tag`` reference. An override named ``pause-container`` no
  longer has any effect. In the session-manager Helm chart, the
  ``imagePrePuller.pauseImage`` value is replaced by ``imagePrePuller.image``,
  with the same ``repository``, ``tag`` and ``pullPolicy`` fields. See
  [pre-pulling workshop images](pre-pulling-workshop-images).

* In the session-manager Helm chart, the ``imagePrePuller`` values take the
  same fields, with the same meaning, as the ``imagePrePuller`` block of the
  ``SessionManager`` custom resource. The ``imagePrePuller.images`` value no
  longer takes full image references that replace the default images: each
  entry gives either a ``name``, resolved through the chart's image inventory
  including the ``imageVersions`` overrides, or an ``image``, a full reference
  pulled as written, and the entries are pulled after the default images. A
  new ``imagePrePuller.includeDefaultImages`` value, ``true`` by default,
  drops the default images when set to ``false``. To keep a list written for
  the old value, write each reference as an ``image`` entry and set
  ``includeDefaultImages`` to ``false``. The chart's values schema rejects an
  entry giving both or neither of ``name`` and ``image``, and
  ``includeDefaultImages`` set to ``false`` with no ``images`` while
  ``enabled`` is ``true``. A ``name`` the inventory does not hold fails the
  install or upgrade with a message naming the entry, but only while
  ``enabled`` is ``true``. See [pre-pulling workshop
  images](pre-pulling-workshop-images).

* The browser JavaScript bundle for the workshop gateway application in the
  workshop base environment image is now generated using esbuild instead of
  browserify. This eliminates security alerts arising from the deprecated
  elliptic package which was an indirect dependency of browserify, but for
  which no fixed version is available.

* The ``educates`` Hugo theme used when rendering workshop instructions has
  been replaced with a new self contained implementation which no longer
  depends on JavaScript code generated from the classic renderer. The original
  theme is not included, and workshop instructions are always rendered with
  the new theme.

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
  4.3.1, ``hugo`` 0.167.0, ``skaffold`` 2.25.0 and ``kctrl`` 0.60.4. These
  updates pick up releases built with a patched Go toolchain and updated
  dependencies which resolve critical vulnerabilities reported against the
  previously bundled versions.

* The ``code-server`` release which provides the workshop editor has been
  updated from 4.125.0 to 4.141.0, bringing the embedded VS Code version from
  1.125.0 to 1.141.0. The update resolves a critical vulnerability and a number
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

* The Zot Registry used by the OCI image cache for workshop environments has
  been updated from version 1.4.3 to 2.1.18, resolving a large number of
  critical and high severity vulnerabilities reported against the old
  version. The format of the synchronization rules able to be supplied under
  ``environment.images.registries`` in the workshop definition is unchanged.
  Any image cache contents from an existing deployment are re-indexed or
  re-synchronized on demand, so no action is required when upgrading.

* The image registry deployed for workshop sessions and for workshop
  environment image mirrors has been updated from the CNCF Distribution
  registry version 2.8.3 to 3.1.2, with the registry binary additionally
  rebuilt against a patched Go toolchain and updated dependencies so that no
  known critical or high severity vulnerabilities remain. The registry
  continues to be configured through the same environment variables, so
  workshop definitions which enable a session image registry or an image
  mirror require no changes. Note that registry 3.x no longer serves
  manifests in the legacy Docker schema 1 format, which has been deprecated
  since Docker 1.10 and does not affect images pushed by current tooling.

* The set of ``kubectl`` versions bundled in the workshop base environment
  image has changed. The 1.31, 1.32 and 1.33 versions have been dropped and
  1.35, 1.36 and 1.37 have been added, so the supported range is now 1.34 to
  1.37. The ``kubectl`` version is still selected automatically to match the
  Kubernetes cluster the workshop session is connected to. For clusters older
  than the supported range the oldest bundled version, 1.34, is used, and for
  clusters newer than the supported range the most recent bundled version,
  1.37, is used. Workshops which target clusters running Kubernetes 1.33 or
  older may therefore see a larger client/server version skew and should be
  verified against a supported cluster version.

* The JDK versions included in the ``jdk8-environment``, ``jdk11-environment``,
  ``jdk17-environment`` and ``jdk21-environment`` workshop base images have
  been updated to the latest Eclipse Temurin patch releases, being 8u504-b01,
  11.0.32.1+1, 17.0.20.1+1 and 21.0.12.1+1 respectively. The bundled Maven
  and Gradle versions have also been updated. All four images now include
  Maven 3.9.16. The ``jdk8-environment`` and ``jdk11-environment`` images
  include Gradle 8.14.6, the latest version able to run on those JDK
  versions, while the ``jdk17-environment`` and ``jdk21-environment`` images
  include Gradle 9.8.1. Gradle 9 is a new major release which introduces
  breaking changes relative to Gradle 8, so any workshops which use Gradle in
  the JDK 17 or JDK 21 images should be verified against the newer Gradle
  version to ensure they still behave as expected. One behaviour change
  affects all four images: ``gradle init`` now aborts if the target directory
  already contains any files, including hidden files, where previously it
  would generate the new project alongside them. Since the home directory of
  a workshop session is never empty, any workshop which has users run
  ``gradle init`` directly in the home directory, or any other non-empty
  directory, will need its instructions updated to either pass the
  ``--overwrite`` option to ``gradle init`` or create the project in a new
  empty subdirectory.

* The ``conda-environment`` workshop base image has been updated to use
  Miniconda 26.7.1-1, with the conda environment it provides now based on
  Python 3.14 rather than Python 3.13. The bundled ``notebook`` and
  ``jupyterlab`` packages have also been updated, to versions 7.6.3 and 4.6.4
  respectively. Workshops which install additional conda or pip packages into
  the default conda environment should be verified against the newer Python
  version in case any of the packages they depend on do not yet support
  Python 3.14.

* The ``vcluster`` software used by the ``vcluster`` workshop application has
  been updated from 0.30.2 to 0.35.2, and the set of Kubernetes versions the
  virtual cluster can provision has been updated. The 1.31 and 1.32 versions
  have been dropped and 1.35 and 1.36 have been added, so the supported range
  is now 1.33 to 1.36 with 1.36 the default. Workshops which pin the virtual
  cluster to Kubernetes 1.31 or 1.32 through the ``vcluster`` application
  ``version`` property must be updated to a supported version. Note that the
  ``vcluster`` application can still provision Kubernetes 1.33, which is no
  longer available for local Kind clusters, and cannot yet provision 1.37.

* The virtual cluster of the ``vcluster`` workshop application no longer
  syncs its service accounts to the host cluster. Every pod it runs on the
  host now runs under the one service account ``vc-workload-my-vcluster``
  in the virtual cluster's namespace, which the session manager creates
  naming the installation's image pull secrets. A host service account
  synced for each virtual one was created together with the pods using it,
  so a pod could start before it named the pull secrets, and the virtual
  cluster's CoreDNS then failed to pull from a registry that requires them.
  Synced pods do not mount a host service account token, so this changes
  only host-side policies or identities that a workshop keyed on the host
  service account of a virtual one.

* The version of ``kind`` embedded in the ``educates`` CLI has been updated
  from 0.29 to 0.33. As a result the default Kubernetes version used when
  creating a local cluster with ``educates local cluster create`` has changed
  from 1.33 to 1.36. A specific node image can still be selected using the
  ``--kind-cluster-image`` option if a different Kubernetes version is required.
  Note that ``kind`` 0.33 no longer publishes a node image for Kubernetes
  1.33, so that version can no longer be selected through
  ``--kubernetes-version``.

* ``educates version`` now also reports the git commit the binary was built
  from, with a ``-dirty`` suffix when it was built from a modified working
  tree. This makes otherwise-identical development builds — which all report
  the floating ``latest`` or ``develop`` version — distinguishable. Release
  binaries continue to report their release version and additionally show the
  commit they were built from.

Removed Features
----------------

* The classic renderer for workshop instructions has been removed, together
  with the ``classic`` workshop template of the ``educates`` command line
  tool. A workshop selected the classic renderer by holding a
  ``workshop.yaml`` or ``modules.yaml`` file in its ``workshop`` directory,
  and the renderer had been described as deprecated in the documentation since
  version 3.0.0. Hugo, with the ``educates`` theme, is now the only renderer
  for workshop instructions, and the features of the classic renderer which
  Hugo does not provide, among them AsciiDoc pages, Liquid templating and the
  automatic ``ENV_`` data variables, are gone with it.

  The workshop base image no longer includes the classic renderer, nor the
  Node.js packages only it used, among them ``asciidoctor``, ``liquidjs`` and
  ``marked``. The original Hugo theme, which loaded its styles and scripts
  from the classic renderer and was included in the 4.0 pre-releases as
  ``educates-classic``, has been removed as well. Workshop instructions are
  always rendered with the ``educates`` theme, and neither a workshop session
  nor ``educates cluster workshop serve`` reads the
  ``WORKSHOP_RENDERER_THEME`` environment variable which selected a different
  theme, so a workshop which still sets it, to ``educates-classic`` or any
  other value, renders with the ``educates`` theme. The ``restart-workshop``
  command, which restarted the classic renderer in a workshop session and had
  no effect for a Hugo workshop, has been removed, and the ``WORKSHOP_PORT``
  environment variable, which gave the port the classic renderer listened on,
  is no longer set in workshop sessions.

  A workshop still in the classic format, or which sets the
  ``LOCAL_RENDERER_TYPE`` environment variable to ``classic``, no longer has
  its instructions rendered in a workshop session. Instead, the instructions
  panel shows a page saying that the classic renderer was removed and giving
  the address of the conversion guide, the same message is written to the
  setup log, and setup is marked as failed, so the workshop dashboard reports
  that the setup scripts failed.

  ``educates workshop new`` holds only the ``hugo`` template, which stays the
  default for its ``--template`` option, and naming ``classic`` or any other
  template fails with an error listing the templates the command holds.
  ``educates cluster workshop serve`` and ``educates workshop render`` fail on
  a classic workshop with a message saying that the classic renderer was
  removed in Educates 4.0 and giving the address of the conversion guide, and
  ``serve`` fails before it contacts the cluster.
  ``educates workshop publish`` is unchanged, so a classic workshop can still
  be published for an earlier version of Educates while it is being converted.

  To convert a classic workshop, see [Converting a classic workshop to
  Hugo](converting-a-classic-workshop).

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

* The ``content.files`` property of the ``Workshop`` custom resource, which
  was deprecated in version 2.0.0 in favour of ``workshop.files``, has been
  removed from the custom resource definition, along with the
  ``download-workshop`` script in the workshop base image which handled it.
  Since version 2.7.1 the value of the property was no longer passed to the
  workshop session, so setting it had no effect and no workshop content was
  downloaded. Workshops should use ``workshop.files`` instead, which downloads
  content from Git repositories, HTTP servers and OCI images using ``vendir``.
  A ``.eduk8signore`` file in workshop content is no longer used, so use
  ``includePaths`` and ``excludePaths`` in ``workshop.files`` to filter what
  is included. As ``kubectl apply`` validates against the schema strictly by
  default, a ``Workshop`` which still sets ``content.files`` is now rejected
  when it is applied, and the property needs to be removed from it.

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

* ``educates workshop publish`` and ``educates package publish`` no longer
  read any ``IMGPKG_*`` environment variable. Registry credentials come from
  the ``--registry-username`` and ``--registry-password``,
  ``--registry-token`` or ``--registry-anon`` flags, and otherwise from the
  Docker configuration that ``docker login`` writes, including the
  credential helpers it names. ``IMGPKG_USERNAME``, ``IMGPKG_PASSWORD``,
  ``IMGPKG_TOKEN``, ``IMGPKG_ANON`` and the ``IMGPKG_REGISTRY_*``
  credentials are ignored, as are ``IMGPKG_ENABLE_IAAS_AUTH`` and
  ``IMGPKG_ACTIVE_KEYCHAINS``, which turned on credentials for Google,
  Amazon ECR, Azure and GitHub registries. A pipeline which set one of them
  should instead run ``docker login``, add a ``credHelpers`` entry for the
  registry to the Docker configuration, such as ``ecr-login`` for Amazon ECR
  or ``gcloud`` for Google Artifact Registry, or pass the ``--registry-*``
  flags. Nothing is printed when one of these variables is set.

* The remote source types have been removed from ``spec.publish.files`` in
  the workshop definition. ``educates workshop publish`` now assembles the
  files itself and accepts only ``directory`` entries, and an entry using
  ``git``, ``hg``, ``http``, ``image``, ``imgpkgBundle``, ``githubRelease``,
  ``helmChart``, ``manual`` or ``inline`` fails the publish with an error
  naming the entry. Files which were fetched from a remote source need to be
  placed in the workshop directory, or in another local directory named by a
  ``directory`` entry, before publishing. An entry whose ``path`` leads
  outside of the image now also fails the publish. ``spec.workshop.files``,
  which sessions download workshop content with, is unchanged.

* The Carvel-based installer from version 3 has been removed. Educates is no
  longer packaged or installed as a ``kapp-controller`` package: the
  ``educates-installer`` package bundle and package repository are no longer
  published, and the CLI no longer requires or interacts with
  ``kapp-controller`` for installation. There is no in-place upgrade from
  version 3 — delete the existing version 3 installation and install version
  4 following the new installation guides. A migration guide mapping version
  3 configuration settings onto their version 4 equivalents is included in
  the documentation.

* The CLI binaries are no longer published as the ``educates-client-programs``
  imgpkg bundle. Download the binaries from the GitHub release for the
  version, or use the ``educates-cli`` container image when embedding the CLI
  in a container image.

* The ``kubectl-convert`` plugin is no longer included in the workshop base
  environment image. It existed to migrate manifests away from long removed
  Kubernetes API versions, nothing in Educates itself uses it, and each of
  the four bundled copies carried vulnerabilities which only a Kubernetes
  patch release can resolve. Workshops which run ``kubectl-convert`` from
  workshop instructions or setup scripts need to install it in a custom
  workshop image, or perform the conversion outside the workshop session.

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

* ``profile.d`` scripts are deprecated, whether supplied with workshop content
  or by an extension package, and will be removed in a future version. They
  are still sourced for now. Set environment variables from a ``setup.d``
  script instead, by writing ``NAME=VALUE`` lines to the file named by the
  ``WORKSHOP_ENV`` environment variable. Variables written there are set in
  the setup scripts which run afterwards and in the processes run in the
  workshop session, including the terminal shell.

* The ``external-dns.alpha.kubernetes.io/hostname`` annotation which the
  operator sets on the Envoy service of the ingress controller is deprecated.
  It is set alongside ``external-dns.kubernetes.io/hostname`` so that
  ``external-dns`` releases before 0.22 continue to publish the wildcard DNS
  record for the ingress domain, and will be removed in a future release. If
  you run your own ``external-dns`` against the cluster, update it to version
  0.22 or later before the older annotation is removed.

* The ``--kubeconfig`` and ``--context`` options of ``educates local registry
  deploy`` are deprecated, and are ignored. The command used to create the
  ``registry`` Service and EndpointSlice in the cluster the options, or the
  current kube context, named. It now configures every local cluster
  created by ``educates local cluster create``, each through its own kube
  context in ``~/.kube/config``, and also creates the
  ``local-registry-hosting`` ConfigMap in each, so a cluster which is not
  such a local cluster is no longer configured. A warning is shown when
  either option is given, and both will be removed in a future version.

Bugs Fixed
----------

* With Kyverno as the cluster security policy engine, the namespace of a
  workshop environment, in which its workshop session pods run, was given no
  ``pod-security.kubernetes.io/enforce`` label, so the default level of the
  cluster's Pod Security admission applied to it. On a cluster whose default
  enforce level was stricter than ``privileged``, workshop session pods which
  needed more than that level allows were rejected, such as those with Docker
  enabled, whose Docker daemon runs as a privileged container. The namespace
  is now labelled ``pod-security.kubernetes.io/enforce: privileged`` with the
  Kyverno engine, as it already was with the ``PodSecurityStandards`` engine.
  The bundled Kyverno cluster policies do not apply to that namespace, so
  what Kyverno enforces is unchanged. The namespaces of training portals and
  workshop sessions are still not labelled with the Kyverno engine, so the
  cluster's default level applies to them.

* On a cluster whose Pod Security admission enforces a default level stricter
  than ``privileged`` on namespaces without their own label, as Talos does
  with ``baseline``, pods Educates installs which need more than ``baseline``
  allows were rejected. These were the Envoy pods of the bundled Contour when
  they bind the node's ports 80 and 443 through ``hostPort``, and the
  ``node-ca-injector`` pods, which run a privileged container that mounts a
  directory of the node. The operator now labels the ``contour`` and
  ``educates`` namespaces with ``pod-security.kubernetes.io/enforce``,
  ``pod-security.kubernetes.io/audit`` and ``pod-security.kubernetes.io/warn``
  set to ``privileged``, whatever the cluster security policy engine, and
  sets those labels again the next time it reconciles the installation if
  they have been removed or changed. The ``cert-manager``, ``kyverno`` and
  ``external-dns`` namespaces are left unlabelled, since the pods installed
  there meet the ``restricted`` level. The operator's own pod now sets the
  ``RuntimeDefault`` seccomp profile, so it also meets the ``restricted``
  level and its namespace needs no label.

* When no reserved workshop session was available and the training portal
  created a new one for a user, it reported the ``Session/Started`` analytics
  event twice. For a workshop session requested through the training portal
  web interface, the event was reported when the user requested it and again
  when it was deployed. For one requested via the REST API, the event was
  reported when it was deployed, before the user had activated it, and again
  on activation. Its ``WorkshopSession`` resource was also given the
  ``Allocated`` phase when deployed, where a reserved workshop session
  requested via the REST API has the ``Allocating`` phase until activated.
  The event is now reported once, after ``Session/Created`` for a workshop
  session requested through the web interface, and on activation for one
  requested via the REST API, which has the ``Allocating`` phase until then.

* When workshop instructions were previewed with ``educates cluster workshop
  serve``, the ``workshop-instructions.css``, ``workshop-instructions.js`` and
  ``workshop-instructions.html`` files of the custom theme configured for the
  training portal were not applied. The instructions requested the CSS and
  JavaScript files from the local Hugo server, which did not have them, so
  the browser reported 404 errors for both. The command now downloads the
  theme files from the workshop session it previews and mounts them into the
  Hugo site as a workshop session does, so the preview applies the same
  styles, scripts and additional page head content. The ``educates workshop
  render`` command is unchanged and does not include the theme files.

* The documentation for ``workshop/config.yaml``, and the commented example
  in the ``workshop/config.yaml`` file created by the ``hugo`` workshop
  template, showed the titles of the pages of a pathway as a top-level
  ``modules`` list. Hugo reads them from ``pathways.modules``, as a map keyed
  by the name of each page, so titles given as shown were ignored. Both now
  show the ``pathways.modules`` form. A workshop with a top-level ``modules``
  section keeps rendering with the titles from the front matter of its pages,
  and rewriting the section under ``pathways`` as now shown makes its titles
  apply.

* The ``educates workshop render`` command ignored the ``params`` and the
  pathway settings in ``workshop/config.yaml``, so the data variables they
  define rendered as empty strings, and the title and description of the
  selected pathway did not replace those of the workshop definition. The
  command now applies them as a workshop session does. It selects the pathway
  named by the ``PATHWAY_NAME`` environment variable in ``session.env`` of the
  workshop definition, or else ``pathways.default``, and sets the
  ``pathway_name`` data variable to it. The top-level ``params`` and those of
  the selected pathway take their ``value``, overridden by an entry of
  ``session.env`` matching their name or ``aliases``. An entry of
  ``session.env`` set through ``valueFrom``, or whose value references another
  variable as ``$(name)``, is ignored, as are the data variables only a
  running workshop session provides, such as ``session_namespace``.

* The ``educates`` binary in the ``educates-cli`` container image embedded the
  Hugo theme for workshop instructions at the wrong location, so the
  ``educates workshop render`` and ``educates cluster workshop serve`` commands
  run with that binary could not find the ``educates`` theme. This affected a
  binary copied from the image into another container image. The binary in the
  image now embeds the theme at the same location as the binaries attached to
  the GitHub release, which were not affected.

* The local secrets cache directory and the files in it were created
  readable by every account on the machine, so other local users could read
  cached private keys and registry credentials. The CLI now creates the
  ``secrets`` directory of its data home with mode ``0700`` and writes each
  cached file with mode ``0600``. A cache directory created with looser
  permissions by an earlier release is changed to ``0700`` the next time the
  CLI writes to the cache, for example with ``educates local secrets add``.

* When a CA secret was configured for the cluster ingress without a wildcard
  TLS secret, the session manager did not copy the CA secret into the
  namespace of each workshop environment, as the copy was only made together
  with the wildcard TLS secret. Workshop sessions still mounted the CA secret,
  so their pods could not start. The CA secret is now copied into the
  workshop environment namespace whenever a CA is configured.

* When a ``TrainingPortal`` named its own TLS secret using
  ``portal.ingress.tlsCertificateRef`` without giving a different namespace
  for it, and a CA secret was configured for the cluster ingress, the session
  manager failed with an error while creating the training portal and kept
  retrying. The CA secret is now copied into the training portal namespace in
  this case as well.

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

* The Node.js packages bundled with the workshop gateway and editor helper in
  the workshop base environment image have been updated to their latest
  upstream releases, being ``axios`` 1.20.0, ``express`` 4.22.3, ``js-yaml``
  4.3.2, ``morgan`` 1.12.1, ``multer`` 2.4.0, ``joi`` 17.13.7 and
  ``fast-uri`` 3.1.8. These updates pick up fixes for vulnerabilities
  reported against the previously bundled versions. The behavior of the
  workshop dashboard is unchanged.

* The Python packages bundled with the session manager, secrets manager,
  lookup service, training portal and tunnel manager have been updated to
  releases which fix vulnerabilities reported against the previously bundled
  versions, being ``urllib3`` 2.8.0 and ``oauthlib`` 4.0.0 in all five
  components, ``Django`` 5.2.17 in the training portal, and ``PyJWT`` 2.15.1
  in the lookup service. The behavior of these components is unchanged.

* Deploying a workshop with ``educates docker workshop deploy`` crashed with a
  Go panic when an extension package listed under ``spec.workshop.packages``
  did not declare a ``files`` property. Such a package is now skipped, since
  there is nothing for ``vendir`` to download, and the remaining packages are
  still processed. A package declaring a malformed ``files`` property now
  reports an error naming the package instead of crashing.

* The workshop session init container which downloads content only ran when a
  workshop declared assets under ``spec.workshop.files``. A workshop whose only
  downloads were extension packages therefore downloaded them in the main
  workshop container instead, where the secrets holding any registry
  credentials are not mounted, so a package held in a registry requiring
  authentication could not be downloaded. The init container now runs whenever
  there is anything to download.

* A custom workshop image given as ``$(image_repository)/...`` could not be
  pulled when deploying a workshop with ``educates docker workshop deploy``
  against the local image registry. The reference was expanded to the name the
  local registry has on the ``educates`` Docker network, which only containers
  on that network can resolve and the Docker daemon pulling the image cannot.
  It is now expanded to the address the Docker daemon reaches the local
  registry by.

* When workshop instructions were rendered using the Hugo renderer with the
  default ``educates`` theme and the workshop provided no instructions pages,
  generation of the instructions failed with a division by zero error while
  rendering the progress bar in the page header. This caused the workshop
  setup scripts to be reported as having failed, and every workshop session
  displayed a dialog warning that workshop content may not be setup correctly.
  The progress bar is now omitted when there are no pages to show, so the
  instructions render successfully.

* When static workshop instructions were enabled by setting
  ``session.applications.workshop.path`` in the ``Workshop`` definition, the
  workshop gateway ignored the value and always served the static HTML files
  from the ``public`` subdirectory of the workshop files. This only worked for
  the documented example value of ``public``, and any other relative or
  absolute path resulted in the wrong content, or no content, being displayed
  for the workshop instructions. The gateway now serves the static HTML files
  from the directory given by ``path``, with a relative path being interpreted
  relative to the directory holding the workshop files, normally
  ``/opt/workshop``. Workshops using the value ``public`` are unaffected.

* When workshop instructions rendered using the default ``educates`` Hugo
  theme were made narrow in the workshop dashboard, by dragging the divider
  between the instructions and the terminals, the buttons in the navigation
  bar at the bottom of the instructions were squashed, overlapped each other,
  or had their labels wrapped onto a second line. As the instructions become
  narrower, the navigation bar now shows the back button as an arrow only,
  shortens the ``Finish Workshop`` button to ``Finish``, hides the step
  progress indicator, and at the narrowest widths shows the continue button
  as an arrow only.

* When workshop instructions were rendered using the Hugo renderer with the
  default ``educates`` theme, the ``workshop-instructions.css``,
  ``workshop-instructions.js`` and ``workshop-instructions.html`` files of a
  custom theme configured for the training portal were not applied. The
  theme files are mounted into the workshop session from a secret, where each
  file is a symbolic link, and Hugo skips symbolic links when copying files
  into the generated instructions. The theme files are now copied, with the
  symbolic links resolved, before the instructions are generated, so the
  custom styles, scripts and additional page head content are applied.

* After Docker was restarted, for example by restarting Docker Desktop, the
  local image registry and the registry mirrors could come back with
  different addresses on the ``kind`` Docker network, as Docker handed out
  addresses in whatever order the containers started. The ``registry``
  service in the ``default`` namespace of the local Kubernetes cluster kept
  the old address of the local image registry, so access to
  ``registry.default.svc.cluster.local`` from inside the cluster, such as
  downloading workshop content published to the local image registry, could
  fail or reach a registry mirror instead. The local image registry and the
  registry mirrors are now given fixed addresses at the top of the subnet of
  the ``kind`` network, which Docker keeps across restarts, and the
  ``registry`` service is updated in place rather than being deleted and
  created again. An existing local image registry or registry mirror is moved
  to its fixed address, keeping its content, the next time
  ``educates local registry deploy`` or ``educates local mirror deploy`` is
  run for it. Docker Engine 29.0.0 to 29.0.3 refuse fixed addresses on the
  ``kind`` network, as do older versions until Docker is restarted for the
  first time after the network is created, in which case Docker picks the
  address as before and a warning is shown.

* When ``educates admin platform deploy`` was run with an
  ``EducatesGKEConfig``, ``EducatesEKSConfig``, ``EducatesInlineConfig`` or
  ``EducatesConfig`` configuration, it checked for a secret named
  ``educates-custom-ca`` in the ``educates-installer`` namespace once the
  ``EducatesClusterConfig`` resource was ready, and stopped with an error when
  the secret did not exist. Nothing creates this secret and these
  configurations do not use it, so a deploy could fail even though the
  cluster configuration was valid. The check has been removed, and the deploy
  now proceeds directly to installing the secrets manager. The operator still
  validates any CA secret named in the ``EducatesClusterConfig`` resource,
  and reports a missing or invalid secret in the status of that resource.

* Deleting an ``EducatesClusterConfig`` in ``Managed`` mode, which
  ``educates admin platform delete`` does, uninstalled the Helm releases named
  ``cert-manager``, ``contour``, ``external-dns`` and ``kyverno`` and deleted
  the namespaces of the same names, whatever the resource said about how each
  service was provided. A cert-manager, Contour, external-dns or Kyverno the
  administrator had installed under those names was removed along with
  Educates. Each cleanup now runs only when Educates installed that service:
  cert-manager, whose cleanup also deletes the wildcard ``Certificate`` and the
  ``ClusterIssuer``, with the ``BundledCertManager`` certificates provider;
  Contour with the ``BundledContour`` ingress controller provider;
  external-dns with the ``BundledExternalDNS`` DNS provider; and Kyverno with
  the ``Bundled`` Kyverno provider and at least one policy engine set to
  ``Kyverno``.

* In ``Managed`` mode, the ``ValidationSucceeded`` condition of the
  ``EducatesClusterConfig`` stayed ``False`` after a validation failure, such
  as a missing CA secret, had been corrected, even once the resource became
  ready. It now returns to ``True`` once the spec and the secrets it
  references pass validation.

* In ``Managed`` mode, ``spec.imageRegistry.prefix`` of the
  ``EducatesClusterConfig`` moved the images of the cluster services but not
  those of the Educates runtime components, which take the prefix from
  ``status.imageRegistry``, a field the operator published only in ``Inline``
  mode. The secrets manager, lookup service, session manager and node CA
  injector, and the training portals and workshop sessions the session
  manager creates, kept pulling from ``ghcr.io``. ``Managed`` mode now
  publishes the field too. In both modes, a prefix with a path also replaced
  the ``educates`` namespace of the Educates images rather than keeping it:
  with ``registry.internal/educates`` the session manager was pulled from
  ``registry.internal/educates/educates-session-manager``, while a mirror of
  the release image list holds it at
  ``registry.internal/educates/educates/educates-session-manager``. Every
  Educates image now keeps its upstream repository path under the prefix, as
  the cluster-service images do, and the ``$(image_repository)`` data
  variable of workshop definitions resolves by default to
  ``<prefix>/educates``. In ``Inline`` mode, a mirror laid out for the old
  behavior, with the images at ``<prefix>/educates-<name>``, is still
  reached when the prefix ends in ``/educates``, by removing that last
  segment from the prefix. Any other such mirror needs the images copied
  into the layout the air-gapped installation guide describes.

* The image pull secrets named in ``spec.imageRegistry.pullSecrets`` of the
  ``EducatesClusterConfig`` in ``Managed`` mode, and in
  ``spec.inline.imageRegistry.pullSecrets`` in ``Inline`` mode, reached no
  pod the operator installs. A pod can use a pull secret only from its own
  namespace, and the secrets stayed in the operator namespace, so against a
  mirror that requires authentication the cluster services and the Educates
  runtime components could not pull their images unless the nodes held
  credentials for the mirror. The operator now copies each secret, under the
  same name, into the ``educates`` namespace in both modes and, in
  ``Managed`` mode, into the namespace of each cluster service it installs,
  whose chart it now gives the secrets for all of its pods, Helm hooks
  included. The copies follow their source secrets, are deleted when a name
  is dropped from the list, and are removed with the
  ``EducatesClusterConfig``. The session manager's rules that pass the
  secrets on to training portal and workshop environment namespaces now have
  them to pass on, and cover workshop session namespaces as well, adding the
  secrets to every service account there. Since a pod takes its service
  account's pull secrets only when it is created, the session manager also
  names them itself on every service account it creates, and on the
  ``default`` service account of every namespace it creates, before anything
  runs there, so that a pod created together with its service account, such
  as a session's vcluster, cannot start without them. A pod that names pull
  secrets of its own takes none of its service account's, so a workshop
  session pod that names the pull secrets of its packages also names the
  installation's. Workshop users can read the secrets in their session
  namespaces, so setting ``spec.imagePullSecretPropagation.sessionNamespaces``
  of the ``SessionManager`` to ``false`` keeps them out of those namespaces,
  except the namespace of a session's vcluster, which the session user
  cannot reach. A listed secret must exist in the operator namespace and have
  the type ``kubernetes.io/dockerconfigjson``; until it does, the
  ``EducatesClusterConfig`` reports ``Ready`` as ``False`` with a message
  naming it. Since the copies go with the
  ``EducatesClusterConfig``, deleting it in ``Inline`` mode now waits while
  any platform resource exists, as it already did in ``Managed`` mode,
  reporting ``Ready`` as ``False`` with the reason ``PlatformCRsPresent``.

* With an image registry prefix, the images a session's vcluster picks
  itself, which the session manager's image inventory does not pin, were
  still pulled from their upstream registries: the CoreDNS of the virtual
  cluster, such as ``docker.io/coredns/coredns:1.14.2``, and the init
  container that rewrites the hosts of stateful set pods,
  ``mirror.gcr.io/library/alpine:3.20``. The session manager now makes the
  prefix vcluster's default image registry, so they are pulled from their
  upstream repository path under it, such as
  ``<prefix>/coredns/coredns:1.14.2``, and the release image list includes
  them.

* When the values the operator computes for a Helm release it manages
  became empty, the release kept its previous values, because Helm reuses
  them on an upgrade given none. Removing ``spec.imageRegistry.prefix`` in
  ``Managed`` mode therefore left Kyverno pulling from the removed prefix,
  and the operator upgraded the Kyverno release on every reconcile, adding a
  revision each time. An upgrade now always replaces the release's values
  with the ones the operator computes.

* Helm releases the operator installed or upgraded at the same time could
  fail with an error naming a resource under the wrong API group, such as
  ``customresourcedefinitions.rbac.authorization.k8s.io ... is forbidden``,
  which left the release failed and its custom resource ``Degraded`` with
  the reason ``ReleaseFailed``. This could happen whenever several releases
  changed together, such as a change to the ``EducatesClusterConfig`` and to
  a platform resource made at once. The Helm clients the operator creates
  shared one Kubernetes client configuration, which Helm modifies for each
  resource kind it handles; each Helm operation now works on a copy of its
  own. A release already left failed this way is retried once its values or
  chart version change.

* When the workshop image already had a ``workshop`` directory in the home
  directory, workshop content downloaded using ``spec.workshop.files`` was
  copied into the home directory rather than into ``/opt/workshop``, but only
  the ``setup.d`` scripts under ``/opt/workshop`` were made executable after
  the download. A script under ``$HOME/workshop/setup.d`` which lost its
  execute permission when ``vendir`` unpacked an archive was skipped without
  any message. Scripts under ``$HOME/workshop/setup.d`` are now made
  executable as well.

* For about a minute after starting, the training portal, secrets manager,
  session manager, lookup service and tunnel manager each logged nine
  ``ERROR`` messages for failed requests to list
  ``customresourcedefinitions.apiextensions.k8s.io``, followed by the warning
  ``Not enough permissions to watch for resources``. The lookup service did
  the same for each cluster it manages. The kopf operator framework they run
  on watches custom resource definitions to notice ones created or changed
  while it runs, and their service accounts are not permitted to read them,
  so kopf fell back to serving the custom resources it found at startup.
  They now tell kopf to work that way from the start, so it no longer makes
  the requests or logs the errors and the warning, and what they serve is
  unchanged. An upgrade of Educates restarts them, which picks up any changed
  Educates custom resource definitions. The ``educates:remote-access``
  cluster role no longer has a rule for ``customresourcedefinitions`` in the
  core API group, which granted nothing, since custom resource definitions
  belong to the ``apiextensions.k8s.io`` API group.
