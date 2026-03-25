---
orphan: true
---

Version 2.6.12
==============

New Features
------------

* Provided an implementation for `xdg-open` which communicates back to the
  workshop dashboard to request opening of a URL in the browser. Note that due
  to possible focus issues (or general browser restrictions), this is not
  always working the first time used, but if triggered a second time it then
  works. The issue is still being investigated.

* Added data variables for use in the workshop definition called
  `$(workshop_environment_uid)` and `$(workshop_session_uid)`. These can be used
  in `environment.objects`, or `session.objects` and `request.objects`
  respectively, for setting up owner references in resources created, against
  the `WorkshopEnvironment` and `WorkshopSession` resources for a workshop. This
  is most useful when using `App` in `objects` in order to apply overlays to
  generated resources to set owner references so can then set `noopDelete: true`
  and still have resources cleaned up when cluster scoped resources are used.
  This works around problems when using `App` where the service account used
  for `App` can be deleted before `App` reconciliation gets to run, resulting
  in a stuck namespace and orphaned cluster scoped resources.

Features Changed
----------------

* When using `educates serve-workshop` with a local Kubernetes cluster, it would
  set the proxy host to the equivalent of `localhost.$(ingress_domain)` to
  access the local Hugo server instance. This has been changed to use
  `loopback.default.svc.cluster.local`, which is an external name service which
  in turn maps to `localhost.$(ingress_domain)`. This is to enable ability to
  override the target for the service for a cluster when running Educates in
  Educates. Note that it was realised after release this should actually be
  `loopback.default.svc.$(cluster_domain)` in order to work where a Kubernetes
  cluster doesn't use `cluster.local`. A change to this will be made in a
  subsequent release.

* Progress message logging was added to `educates` CLI commands such as
  `publish-workshop`, `deploy-workshop`, `browse-workshops` and
  `serve-workshop`, so you knew what was happening.

Bugs Fixed
----------

* When using `educates serve-workshop --patch-workshop` the local settings
  directory for the CLI wasn't being created if it didn't already exist.

* When the training image was added to the list of images to pre-pull to the
  cluster, the ability to disable the image pre-puller by setting the list
  of workshop images to pre-pull in the data values file to an empty list was
  lost. This ability to disable image pre-puller has been restored.

* When the temporary directory for file uploads was changed to be in the same
  volume as the home directory, the `uploads` directory was used as the
  temporary directory. Problem was that the user could change what the uploads
  directory was and it actually defaults to the home directory. Because the
  image upload code always creating the temporary directory even when uploads
  was not enabled, you ended up with a `uploads` directory polluting the home
  directory. The temporary directory for uploads has now been changed to
  `$HOME/.local/share/uploads` so is out of view.
