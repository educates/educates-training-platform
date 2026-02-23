---
orphan: true
---

Version 2.6.5
=============

New Features
------------

* Added `--with-services` and `--with-platform` options to the `educates
  create-cluster` command so that these can be set to `false`, resulting in
  those components not being installed. This is intended for when need to create
  an empty Kubernetes cluster which is then going to be used for working on
  developing Educates.

Features Changed
----------------

* The Kubernetes service object for the workshop session deployment previously
  exported port 10080, with it proxying through to port 10080 on the workshop
  pod. The exported port is now 80, with it still proxying through to port 10080
  on the workshop pod. This was only used internally and the change should not
  affect any workshops.

* The permissions of the `$HOME/.kube/config` file have been changed to 0600 so
  that Helm doesn't complain about it being readable by group and others.

* When the Git repository feature is enabled, the directory where the Git
  repositories are kept is now included in the workshop data volume. This means
  that when persistent storage is enabled the Git repositories will be stored
  in the persistent volume.

* The training portal image is now always pre-pulled to each node in the
  Kubernetes cluster when Educates is deployed so that creating training portal
  instances is quicker.

Bugs Fixed
----------

* The `vcluster.objects` and `docker.compose` sections of the workshop
  definition were being included in the limited version of the workshop
  definition available in the workshop session container. As these could have
  contained sensitive information, this should not have been the case.

* The certificate authority (CA) certificate when provided was not being
  injected into the `kapp-controller` configuration when creating a local
  Educates cluster. This was resulting in `kapp-controller` not trusting any
  secure ingress created by Educates.
