---
orphan: true
---

Version 2.6.13
==============

New Features
------------

* Added to the `educates cluster workshop request` CLI command used to test
  training portal REST API access, the `--environment-name`, `--user`,
  `--timeout`, and `--no-browser` options.

Bugs Fixed
----------

* RBAC access for the training portal related to finalizers on the
  `WorkshopSession` resource were missing causing workshop session creation to
  fail on OpenShift. This was required due to OpenShift requiring finalizers for
  RBAC access where plain Kubernetes clusters do not usually enable this
  requirement.

* The `educates serve-workshop` command would not work when patching the in
  place workshop definition and the cluster domain was something other than
  the typically used `cluster.local`.
