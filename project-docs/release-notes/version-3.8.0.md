Version 3.8.0
=============

This version is the first release of the 3.8 support line, which carries on
from the 3.7 support line. While development of the next major version of
Educates continues, the 3.8 support line will be used to keep the current
major version up to date with security fixes and bug fixes, with selected
features also able to be back ported where needed to support user
requirements. The changes in this release were developed on the main
development branch for the upcoming 4.0 release and have been back ported to
the 3.8 support line. They update container base images, language runtimes,
bundled tools and package dependencies to eliminate known security
vulnerabilities, and refresh the supported Kubernetes version range so the
release remains usable with current Kubernetes clusters for as long as
possible.

Features Changed
----------------

* The ``bombardier``, ``dive``, ``kwt`` and ``kctrl`` command line tools are
  no longer included in the workshop base image. None of them are used by
  Educates itself. ``kwt`` was archived by its authors in 2023, and each of
  the four was already pinned at the most recent release its authors have
  published while still carrying vulnerabilities with no fixed version
  available, so there was nothing to upgrade to. Workshops which run any of
  these commands from workshop instructions or setup scripts need to install
  them in a custom workshop image, or use an alternative tool. Note that
  ``kctrl`` here refers only to the copy bundled inside workshop sessions;
  the Carvel based installation of Educates itself is unaffected.

* The Kubernetes dashboard backend used for the workshop console is now
  rebuilt from the upstream 2.7.0 sources with a current Go toolchain and
  patched dependency versions, rather than being copied as a prebuilt binary
  from the upstream container image. This resolves all critical and the
  majority of high severity vulnerabilities reported against the previously
  bundled binary. The dashboard version and its behavior within workshop
  sessions are unchanged.

* Octant is no longer available as the web console for a workshop session and
  the Octant binaries are no longer included in the workshop base image.
  Octant was archived by its authors, with no release since ``0.25.1`` and no
  fixes available for the many vulnerabilities reported against the versions
  which were bundled. Workshops which set
  ``session.applications.console.vendor`` to ``octant`` will now be given the
  Kubernetes dashboard instead, with a warning logged against the workshop
  session. The ``session.applications.console.octant`` property has been
  removed from the ``Workshop`` custom resource definition. Note that Octant
  was never the console used by default, including for workshops which enable
  a virtual cluster, so workshops which did not request it explicitly are
  unaffected.

* The ``git-serve`` program bundled in the workshop base image, which
  provides the per-session Git server, is now built with patched versions of
  its Go module dependencies. The 0.0.5 release it is built from is the last
  its author published, so the dependency versions it pins carry known
  vulnerabilities and no newer release exists to pick up fixes. The flagged
  modules are raised to fixed versions at build time; the behavior of the
  per-session Git server is unchanged.

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

* The bundled versions of [reveal.js](https://revealjs.com/) used for workshop
  slides have changed. Version ``6.0.1`` is now bundled alongside ``4.6.1``
  and ``5.2.1``, and version ``3.9.2`` is no longer included. The 3.9.2 copy
  bundled a ``reveal-js-multiplex`` plugin package which is flagged as
  malicious (``MAL-2022-5772``) by scanners which consult the OpenSSF
  malicious-packages database, and the 3.x series of reveal.js has been
  unmaintained for years. Workshops which select the reveal.js version
  through the ``session.applications.slides.reveal.js`` property of the
  ``Workshop`` custom resource using a ``3.X`` selector need to move to
  ``4.X``, ``5.X`` or ``6.X``, and should review their slide decks against
  the upstream reveal.js upgrade guidance since the markup and plugin APIs
  changed after the 3.x series. If the requested version cannot be matched
  against a bundled version the slides are not served for the workshop
  session.

* The container base images used for the training portal, session manager,
  secrets manager, lookup service, tunnel manager, image cache and workshop
  base environment have been updated from Fedora 42 to Fedora 44, eliminating
  known vulnerabilities reported against operating system packages of the
  older base image. The Python package dependencies of the Python based
  platform services have also been updated to current versions.

* The training portal now runs on Python 3.14 and has been upgraded from
  Django 4.2 LTS to Django 5.2 LTS, along with updates to the other Python
  packages it uses. This ensures the key frameworks the training portal is
  built on continue to receive security fixes for an extended period.

* The assets server is now built as a static binary and runs from a minimal
  ``scratch`` container image instead of a Fedora based image, so it no
  longer includes any operating system packages against which security
  vulnerabilities could be reported.

* The browser JavaScript bundles for the workshop renderer and gateway
  applications in the workshop base environment image are now generated using
  esbuild instead of browserify. This eliminates security alerts arising from
  the deprecated elliptic package which was an indirect dependency of
  browserify, but for which no fixed version is available.

* The workshop base environment image has been updated to use Node.js 24, as
  the Node.js 20 version previously used has reached end of life. The NPM
  package dependencies of the gateway, renderer and helper applications of
  the workshop base environment, and of the Docker desktop extension, have
  also been updated to resolve reported package security advisories.

* Third party tools included in the workshop base environment image, such as
  ``yq``, Hugo and ``uv``, have been updated to current versions.

* The ``helm`` CLI included in the workshop base environment image has been
  updated from the 3.x series to 4.x. Helm 4 is a new major release which
  introduces breaking changes relative to Helm 3, so any workshops which use
  the ``helm`` CLI should be verified against the newer Helm version to ensure
  they still behave as expected.

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

* The version of ``kind`` embedded in the ``educates`` CLI has been updated
  from 0.29 to 0.32. As a result the default Kubernetes version used when
  creating a local cluster with ``educates local cluster create`` has changed
  from 1.33 to 1.36. A specific node image can still be selected using the
  ``--kind-cluster-image`` option if a different Kubernetes version is
  required.

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

* The ``sshd`` configuration used for SSH access to workshop sessions has
  been hardened. Public key authentication is now explicitly required, root
  login is disallowed, SSH agent forwarding is disabled, and TCP forwarding
  is restricted to the local direction, meaning a workshop session can still
  be used as an SSH jump host, but remote port forwarding is no longer
  permitted. The ``sshd`` log level has also been reduced from debug level,
  which the OpenSSH documentation notes violates user privacy, to the
  standard informational level.

* If a user ends a workshop session, or attempts to return to one, after their
  training portal login session has already expired, for example after leaving
  the browser window open for a long period after the workshop session had
  finished, they are now shown the workshop session finished page rather than
  being redirected to the portal login page. Previously an expired login in
  this situation could result in the portal login page being displayed, which
  was undesirable when workshop sessions are coordinated by a custom front end
  through the training portal REST API.

* The training portal has reduced the number of requests it makes against the
  Kubernetes REST API when checking for workshop sessions which were deleted
  out of band, such as when a ``WorkshopSession`` resource is deleted
  manually. A single query is now used to retrieve the set of deployed
  workshop sessions belonging to the training portal, rather than a separate
  query being made for each individual workshop session, and the check now
  runs once a minute instead of every fifteen seconds. On training portals
  hosting a large number of workshop sessions the previous behaviour could
  overload the Kubernetes REST API, resulting in errors when accessing the
  Kubernetes cluster. In addition, a workshop session database record which
  never had a corresponding deployment created, for example because the
  training portal was restarted at the wrong time, is now cleaned up after a
  grace period rather than indefinitely counting against the capacity of the
  workshop environment.

Bugs Fixed
----------

* The workshop base environment image shipped test fixtures belonging to npm
  packages bundled for the workshop dashboard gateway and renderer, and to
  the code editor. One of these fixtures, a ``package.json`` declaring the
  package ``monorepo-symlink-test`` inside the published tests of the
  ``resolve`` package, matches a known malicious npm package and is flagged
  as ``MAL-2022-4691`` by scanners which consult the OpenSSF
  malicious-packages database, and an editor test fixture carried a private
  key which is flagged by secret scanners. The test fixture directories are
  now stripped from the image build, so scans of the published images no
  longer report these findings. The fixtures were inert test data and were
  never executed as part of Educates.

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

* Every reconcile of the installer created a new version of the
  ``educates-config`` secret even when the rendered configuration was
  identical to the previous version, causing a rolling restart of the
  ``session-manager`` and ``secrets-manager`` operators on every reconcile.
  This was because the secret is populated using ``stringData``, which the
  Kubernetes API server converts to ``data`` on the live object, while the
  ``kapp.k14s.io/original`` annotation was disabled for all resources, so
  ``kapp`` compared the rendered resource against the live cluster state and
  treated every deploy as a change to the versioned secret. Versioned
  resources now retain the ``kapp.k14s.io/original`` annotation so that
  unchanged deploys are recognised as unchanged, and a new secret version,
  with the resulting restart of the operators, only occurs when the
  configuration has actually changed. The ``azure-config-file`` secret used
  by ``external-dns`` for Azure DNS suffered the same mismatch and is now
  populated using ``data`` with explicit base64 encoding, and the
  ``remote-access-token`` service account token secret is no longer
  re-applied on every reconcile as the token contents populated by
  Kubernetes are now preserved when calculating changes.
