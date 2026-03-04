Project Roadmap
===============

For long term project plans see the [project
roadmap](https://github.com/educates/educates-training-platform/blob/develop/developer-docs/project-roadmap.md)
included in the project source code.

Details of more immediate plans are listed below.

(upcoming-changes)=
Changes already completed
-------------------------

The following changes were made in Educates 3.0.0:

* Supply of Carvel repository packages for Educates has been stopped. You can
  still install Educates using Carvel packages using supplied `App` resource
  definitions, but the `PackageRepository` resource type which bundled access
  to multiple versions has been removed. This was done because in-place rolling
  updates of the Educates version isn't always feasible and it is always
  recommended to install from scratch rather than upgrading.

* The `educates` CLI's ability to install Educates and create a local Kind
  Kubernetes cluster has been overhauled. The experience is similar, but the
  requirement for `kapp-controller` to exist in the Kubernetes cluster has been
  removed. The `kapp-controller` package can still be optionally installed as
  it may be required by workshops that depend on it. The CLI also supports
  opinionated installs of Educates to IaaS providers such as AWS, GCP and
  Azure.

Upcoming changes
----------------

Note that the following features are deprecated and the current plan is that
they will be removed sometime in the 4.X series of Educates releases:

* The classic renderer for workshop instructions (Markdown and AsciiDoc) will be
  removed. All workshops should be ported over to use the Hugo (Markdown) based
  renderer.

* The older mechanism for downloading workshop files by specifying
  `spec.content.files` in the workshop definition will be removed. The `vendir`
  based mechanism for workshop files should be used instead.

* The Octant web console for viewing and interacting with a Kubernetes cluster
  will be removed. The standard Kubernetes dashboard should be used instead.

* The use of `profile.d` files has been superseded by adding environment
  variables to the `WORKSHOP_ENV` file from `setup.d` scripts. Support for
  `profile.d` files will be removed to more easily allow the set of shell
  scripts used to initialize a workshop container to be re-implemented as a
  standalone Go application. Workshops should switch to setting environment
  variables from `setup.d` scripts.

* Support for deploying Educates to a Kubernetes cluster which uses Pod Security
  Policies will be removed.

* The `skaffold` command line tool will be removed. If this is required for a
  specific workshop it will need to install it as part of workshop setup.

* The `buildah` command line tool has been included for some time but is
  believed to be non functional due to limitations in most Kubernetes
  environments. If this is confirmed it will be removed.

* The name of the workshop session UNIX user will be changed from `eduk8s` to a
  name not linked to the project name. Workshops should use `~/` or `$HOME/` in
  file system paths where possible, but may need to change paths where anchors
  for home directory cannot be used.

Other notable changes intended to be made with version 4.X of Educates are:

* First class support for OpenShift will be added back into Educates after
  having previously been removed. This will include support for using the
  OpenShift web console embedded as a dashboard tab, and RBAC for commonly
  used OpenShift specific namespaced resources.
