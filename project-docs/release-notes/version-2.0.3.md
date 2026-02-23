---
orphan: true
---

Version 2.0.3
=============

Also refer to release notes for prior patch revisions of Educates version 2.0.

* [Version 2.0.0](version-2.0.0)
* [Version 2.0.1](version-2.0.1)
* [Version 2.0.2](version-2.0.2)

Bugs Fixed
----------

* Due to a bug in the Python operator framework being used, if there were multiple authentication failures with the Kubernetes REST API, the operator main loop would shutdown, but it wouldn't exit properly and so the process wouldn't shutdown. This denied the operator a chance to recover and operator functionality would no longer work. This was affecting the secrets manager, session manager and training portal. As well as implementing a workaround for the operator framework issue, improved readiness and liveness checks have been added to ensure respective applications can automatically recover when there are issues like this.
