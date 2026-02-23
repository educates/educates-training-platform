---
orphan: true
---

Version 2.5.1
=============

Features Changed
----------------

* It was not possible to use a local directory as source when using vendir to
  install extension packages. This was an omission as was possible for workshop
  files. Using a local directory with extension packages is now possible.

* Updated Maven version to 3.9.2.

Bugs Fixed
----------

* Extension package files could not be mounted directly into the `/opt/packages`
  directory from a secret or config map, as Kubernetes would usually set the
  permissions such that it is owned by root and not writable by the workshop
  user, resulting in a failure when setting up to download any other packages
  using vendir. A similar permissions issue existed with the `/opt/assets`
  directory but that may have not caused a failure. In both cases the directory
  is now pre-created in the workshop base image with the correct ownership and
  permissions.

* Kubernetes setup wasn't correctly checking for existence of the injected file
  `/opt/kubernetes/config` before attempting to copy it to `$HOME/.kube/config`.
  The error was ignored so setup would keep going, but it would result in a
  unexpected error message in the setup scripts log.
