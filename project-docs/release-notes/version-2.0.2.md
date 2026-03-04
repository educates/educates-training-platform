---
orphan: true
---

Version 2.0.2
=============

Also refer to release notes for prior patch revisions of Educates version 2.0.

* [Version 2.0.0](version-2.0.0)
* [Version 2.0.1](version-2.0.1)

New Features
------------

* When creating a virtul cluster and enabling the installation of Contour as the ingress controller, in addition to the ``$(session_namespace).$(ingress_domain)`` subdomain being routed to the virtual cluster, ``default.$(session_namespace).$(ingress_domain)`` will also be routed, and you can add extra subdomains to route in the configuration.

  For more information see:

  * [Provisioning a virtual cluster](provisioning-a-virtual-cluster)
