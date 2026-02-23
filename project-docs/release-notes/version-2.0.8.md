---
orphan: true
---

Version 2.0.8
=============

Also refer to release notes for prior patch revisions of Educates version 2.0.

* [Version 2.0.0](version-2.0.0)
* [Version 2.0.1](version-2.0.1)
* [Version 2.0.2](version-2.0.2)
* [Version 2.0.3](version-2.0.3)
* [Version 2.0.4](version-2.0.4)
* [Version 2.0.5](version-2.0.5)
* [Version 2.0.6](version-2.0.6)
* [Version 2.0.7](version-2.0.7)

Bugs Fixed
----------

* Attempting to set overrides for selected limit range values would result in the workshop session deployment failing.

* Default password for JupyterLab when using ``conda-environment`` base image was not being set correctly for latest JupyterLab versions.

* Kubernetes dashboard process was being started even when the console option was disabled.

* When the editor option was disabled, the workshop setup scripts would fail on startup.

* Disabling workshop instructions would cause the workshop container startup to fail.
