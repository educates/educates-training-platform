---
orphan: true
---

Version 2.5.3
=============

This version was released in order to back port selected changes/fixes from
the development version of 2.6.0.

New Features
------------

* Added ability to override the session cookie domain for training portal and
  workshop session cookies. These default to being for the same host, but to
  allow embedding into sites which use an alternate domain, you can now set the
  cookie domain to be a common parent domain. The custom cookie domain needs to
  be set in the global Educates configuration.
