---
orphan: true
---

Version 2.6.14
==============

Features Changed
----------------

* When using the `educates` CLI to create a local Educates cluster, a fixed
  version of `kapp-controller` is now installed, rather than always installing
  the latest version. This is to avoid problems where a breaking release of
  `kapp-controller` is made. Instead will always use a known good version. If
  necessary the version of `kapp-controller` installed can be overridden using
  the `--kapp-controller-version` option to the `educates create-cluster`
  command.
