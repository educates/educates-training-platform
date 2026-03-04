---
orphan: true
---

Version 2.0.7
=============

Also refer to release notes for prior patch revisions of Educates version 2.0.

* [Version 2.0.0](version-2.0.0)
* [Version 2.0.1](version-2.0.1)
* [Version 2.0.2](version-2.0.2)
* [Version 2.0.3](version-2.0.3)
* [Version 2.0.4](version-2.0.4)
* [Version 2.0.5](version-2.0.5)
* [Version 2.0.6](version-2.0.6)

Bugs Fixed
----------

* Support for websockets was not enabled for Contour on the ingress used to proxy requests through to the Contour instance installed into a virtual cluster.

* Files copied into the workshop home directory for VS Code were owned by root instead of the workshop user. This wasn't causing any known issues but was still incorrect.

* If the open-vsx.org service used to store VS Code extensions is offline, installation of the VS Code extension for Educates integration would hang for five minutes before then completing. As this was done when the workshop container was being setup, this would delay the workshop being available to access. The reason for contacting open-vsx.org was telementry in the VS Code extension installation, even though the extension was being installed from a local file, but the telemetry cannot be selectively turned off. Solution was to null out the URL for the extensions gallery when installing the extensions.

Features Changed
----------------

* Updated VS Code editor to the latest version.
