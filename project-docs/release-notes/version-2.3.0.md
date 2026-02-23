---
orphan: true
---

Version 2.3.0
=============

New Features
------------

* A self created certificate authority (CA) certificate used to sign the
  wildcard cluster ingress certificate can now be supplied when deploying
  Educates. This will be injected into workshop sessions, docker side car and
  other components as necessary, so the cluster ingress certificate can be
  trusted when workshops need to contact services via any exposed ingresses
  Educates creates using the wildcard cluster ingress certificate. This CA will
  also be automatically injected into cluster nodes when using the ``educates``
  CLI to deploy a local kind cluster with Educates. Injection of the CA into the
  nodes of an arbitrary Kubernetes cluster can be enabled if desired, however
  the nodes of the Kubernetes cluster must use a Debian based operating system
  and use ``containerd`` as the container runtime. For more details see
  [Defining configuration for ingress](defining-configuration-for-ingress).

* A separate TLS certficate can now be supplied corresponding to a hostname
  supplied as override when creating a training portal. This is to handle the
  case where the desired hostname for the training portal doesn't match the
  wildcard cluster ingress certificate. Do note though that the hostname for the
  training portal must still share a common parent domain with the wildcard
  cluster ingress certificate due to restrictions on cross domain cookies. For
  more details see [Overriding the portal
  hostname](overiding-the-portal-hostname).

* Multiple web user interface themes for Educates can now be supplied via
  secrets created in the Kubernetes cluster. Which theme is used can then be
  selected when deploying a training portal, thus allowing different training
  portals on the same Kubernetes cluster, and the workshops created from them,
  to have different styling for the web user interfaces. Themes created in this
  way can include additional assets such as images, which the CSS or Javascript
  can reference when changing the visual appearance of the web user interface.
  For more details see [Overriding styling of the
  workshop](overriding-styling-of-the-workshop) and [Selecting the user
  interface theme](selecting-the-user-interface-theme).

* In the training portal definition, as a global setting, or against specific
  workshops, it is now possible to supply a startup timeout. If this timeout
  value is exceeded when requesting a workshop session via the training portal
  web interface, the workshop session will be automatically deleted and the
  workshop user redirected back to the training portal web interface, or front
  end portal, as appropriate. Note that when using the ``educates`` CLI to
  deploy workshops, it will set a default startup timeout of 2 minutes for any
  workshop session. For more details for [Timeout for accessing
  workshops](timeout-for-accessing-workshops).

* Added ``httpie`` to the workshop base image as alternative to using ``curl``
  in workshop instructions or workshop setup scripts.

* Added the ``sqlite`` package to the training portal image such that the Django
  ``dbshell`` management command can be used within the training portal
  container to access the database used by the training portal.

Features Changed
----------------

* Previously the OAuth based authentication workflow for workshop sessions
  required TLS certificates to be signed by an official certificate authority
  and self signed certificates were not supported. A self signed wildcard
  cluster ingress certificate can now be used as workshop sessions will contact
  the training portal API to authorize the session using the internal cluster
  service, rather than the public URL and as such it doesn't need to be able
  to trust the certificate.

* When a dialog is displayed indicating that a workshop session has expired,
  when it is being terminated explicitly, when errors occur doing workshop
  setup, or when a special dialog is displayed on starting a workshop, the
  background behind the dialog will now be filled in and hide the workshop
  dashboard behind. The workshop dashboard behind will become visible when the
  dialog is dismissed.

* When the ``files`` download mechanism is enabled for a workshop session,
  access was previously only possible from the web browser as access was
  controlled by the cookie based authentication. In addition to this, it is now
  possible to download files from the workshop session outside of the browser
  session, by supplying the workshop session services password via the ``token``
  query string parameter of the URL for the file to be downloaded. For more
  details see [Enabling workshop downloads](enabling-workshop-downloads).

Bugs Fixed
----------

* When using the ``educates`` CLI to deploy the training platform and the local
  secrets cache contained multiple secrets, the secret corresponding to the
  cluster ingress domain may not be matched and used correctly.
