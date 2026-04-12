Version 3.8.0
=============

New Features
------------

* When creating a local Kubernetes cluster, you can now select the Kubernetes
  version to use with the ``--kubernetes-version`` flag on the
  ``local cluster create`` command. If not specified, the default version
  defined in the platform constants is used. This allows testing workshops
  against different Kubernetes versions without changing global configuration.

* Local clusters now support multi-node configurations. Worker nodes can be
  configured with custom labels and taints, enabling more realistic workshop
  environments that require node affinity or toleration scenarios.

* The installer now supports overlays, allowing platform-specific or
  environment-specific configuration patches to be applied on top of the base
  installer configuration. Overlays are organised by environment type (e.g.
  ``kind``, ``eks``, ``openshift``, ``vcluster``, ``minikube``, ``generic``,
  ``custom``) and are applied automatically based on the selected deployment
  target.

* A new ``local mirror list`` command has been added to list all configured
  image mirror registries associated with the local cluster.

* Docker Desktop extension workshops now support port mapping for Compose
  services, allowing workshop services to expose ports on the host. Missing
  flags have been added to the ``docker workshop deploy`` command to align it
  with the available workshop deployment options.

Features Changed
----------------

* The CLI command structure has been significantly refactored to improve
  consistency and maintainability. Commands are now grouped into a more
  uniform hierarchy with cleaner separation between ``cluster``, ``local``,
  ``docker``, ``workshop``, and ``tunnel`` command groups.

* Registry and mirror management logic has been consolidated to remove
  duplicated code across the local cluster and Docker workshop subsystems.

* The local registry and mirror containers are now assigned fixed IP
  addresses to improve stability across cluster restarts.

* Container labels are now applied consistently to all containers created by
  the CLI (registries, mirrors, secrets containers), making it easier to
  identify and manage resources created by Educates.

* The Docker Desktop extension UI dependencies have been updated and the
  extension backend now logs deploy request parameters and responses to
  stdout, making it easier to diagnose issues via ``docker logs``.

* Multi-architecture Docker builds now always push to the registry and
  subsequently pull to the local Docker daemon, ensuring the local image
  cache is always consistent with the registry. This prevents stale binaries
  in images that depend on other images built in the same pipeline.

* The ``educates`` binary in the CLI image is now built with ``CGO_ENABLED=0``
  to produce a statically linked binary, which is required for the
  ``scratch``-based container image to run correctly.

* The ``local secrets add ca``, ``local secrets add tls``, and
  ``local secrets add registry`` commands now accept an ``--as-string`` flag.
  When provided, secret data is stored in the ``stringData`` field instead of
  the binary ``data`` field. This is useful when creating secrets that must be
  compatible with tools such as cert-manager that expect plain-text values.

* The ``local secrets list`` command output has been improved. It now displays
  a table with NAME, TYPE, KEYS, and DOMAIN columns instead of listing only
  secret names.

* Secret management logic previously embedded in individual CLI command
  implementations has been consolidated into a dedicated ``secrets`` package.
  This removes duplicated code across the ``local secrets add ca``,
  ``local secrets add tls``, ``local secrets add registry``, and
  ``local secrets list`` commands.

* The node CA injector now supports both ``/config/ca/ca.crt`` and
  ``/config/ca/tls.crt`` as candidate CA certificate paths. The injector tries
  each path in order and uses the first one that exists. This allows the injector
  to work with CA secrets created by cert-manager, which stores the CA
  certificate under ``tls.crt``, as well as secrets that follow the ``ca.crt``
  convention.


Bug Fixes
---------

* Fixed an issue where the Docker Desktop extension image could contain a
  stale ``educates`` binary due to Docker BuildKit layer caching not being
  invalidated when the upstream ``educates-cli`` image was rebuilt.

* Fixed a recursive variable reference in the ``docker-extension/Makefile``
  that caused ``make source`` and other targets to fail with the error
  ``Recursive variable 'TARGET_PLATFORMS' references itself``.

* Fixed the ``docker-extension/Makefile`` not inheriting ``IMAGE_REPOSITORY``,
  ``PACKAGE_VERSION``, and ``BUILDX_BUILDER`` from the parent ``Makefile``
  when invoked as a sub-make, causing the extension to always be built
  against ``localhost:5001`` regardless of the configured registry.

* Fixed an issue where locally cached TLS and CA certificate secrets were
  unconditionally applied to the installation configuration, even when a CA
  certificate was already explicitly configured via
  ``clusterInfrastructure.caCertificate``. Cached secrets are now only applied
  when no infrastructure-level CA certificate reference is defined, preventing
  conflicts with cert-manager-managed certificates.
