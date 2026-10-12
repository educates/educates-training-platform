Version 3.8.1
=============

This version is a patch release of the 3.8 support line. It updates package
dependencies, bundled command line tools and container images to eliminate
known security vulnerabilities. There are no functional changes to Educates
itself.

Features Changed
----------------

* The ``kubectl`` versions bundled in the workshop base environment image have
  been updated to the latest patch releases for Kubernetes 1.34, 1.35 and
  1.36. The supported range remains 1.33 to 1.36.

* The other command line tools bundled in the workshop base environment image
  have been updated to their latest releases. This includes ``helm`` 4.3.0,
  ``kustomize`` 5.8.3, the Carvel tools, ``skaffold``, ``hugo``, ``yq``,
  ``uv`` and the ``code-server`` editor.

* The JDK workshop images have been updated to the current Eclipse Temurin
  releases for Java 8, 11, 17 and 21. Gradle has been updated to the newest
  release able to run on each JDK, being 8.14.6 for the Java 8 and 11 images
  and 9.8.1 for the Java 17 and 21 images.

* The conda workshop image has been updated to the current Miniconda release
  for Python 3.14, along with the latest ``notebook`` and ``jupyterlab``
  packages.

* The Docker daemon image used for the Docker-in-Docker side car container,
  when docker support is enabled for a workshop, has been updated from Docker
  27 to Docker 29. This resolves security advisories in the daemon which were
  only fixed in the Docker 29 release series, and matches the version of the
  ``docker`` command line client already installed in the workshop base
  environment image. Workshops which rely on daemon behaviour specific to
  Docker 27 should be retested.

* The Educates runtime components, the ``educates`` command line client and
  the supporting container images have been rebuilt with updated dependencies
  and Go toolchain. A small number of advisories remain open against third
  party packages for which no fixed version has been published. None of them
  affect the way Educates uses those packages.
