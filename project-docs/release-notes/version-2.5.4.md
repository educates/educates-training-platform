---
orphan: true
---

Version 2.5.4
=============

This version was released in order to back port selected changes/fixes from
the development version of 2.6.0.

New Features
------------

* Added the ability to set a custom cookie domain for the training portal and
  workshop sessions in the training portal configuration. This will override
  any global configuration to set the cookie domain.

Bugs Fixed
----------

* When using EKS, the Kubernetes version returned by `kubectl` could have a
  suffix on the minor version string. This would cause problems when working
  out what version of the `kubectl` binary should be used in the workshop
  container, with a version mismatch being reported when `kubectl` was used.
  Any suffix on the `major.minor` version will now be stripped.
