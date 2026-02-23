---
orphan: true
---

Version 2.0.4
=============

Also refer to release notes for prior patch revisions of Educates version 2.0.

* [Version 2.0.0](version-2.0.0)
* [Version 2.0.1](version-2.0.1)
* [Version 2.0.2](version-2.0.2)
* [Version 2.0.3](version-2.0.3)

Bugs Fixed
----------

* When ``kapp-contoller`` was periodically reconciling packages, it was clearing out the data for the persistent REST API tokens for the secrets and session manager instances. This was causing the REST API tokens to be refreshed, which was the opposite desired result of having persistent REST API tokens. Result was that the operator pods were restarting because of login authentication failures.
