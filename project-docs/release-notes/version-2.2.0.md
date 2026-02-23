---
orphan: true
---

Version 2.2.0
=============

New Features
------------

* An SSHD server can now be enabled and run in the workshop container. This
  allows a remote host with access to the SSH private key of the workshop
  session to connect to the workshop session and start a terminal session,
  execute a command, copy files or use sftp.

  In the case of the remote client running in a distinct pod of the same
  Kubernetes cluster, it can use the service hostname for the workshop pod to
  access the workshop container using SSH.
  
  For the case where the remote client is outside of the Kubernetes cluster, a
  SSH tunneling proxy server can be enabled for the workshop, with the remote
  SSH client able to use a special local proxy client to connect to the workshop
  container using SSH. This latter mechanism supports the ability to be able to
  use the remote workspace over SSH mechanism of VS Code and other IDEs running
  on a local machine.

  At present the local proxy client is embedded as a command in the Educates CLI
  to faciliate testing. Sample code is provided in Python and Golang for
  creating a standalone local proxy client, which would be recommended if you
  want to expose this capability to workshop users, given that the Educates CLI
  shouldn't at this point be given to external workshop users.

  For more information about this feature see [Enabling remote SSH access](enabling-remote-ssh-access).

* When using the clickable action ``files:download-file`` it is now possible
  to supply an additional ``download`` property to set the local name of the
  file when saved to the local file system in place of using the basename of the
  source file.

* When using the clickable action ``files:download-file`` it is now possible to
  supply an additional ``preview`` property, which when set to ``true`` will
  result in a preview of the file to be downloaded being displayed in the code
  body of the clickable action.

* A new clickable action ``files:copy-file`` has been added which is used in a
  similar way to ``files:download-files``, except that instead of downloading
  the file, it will copy the contents of the file into the browser paste buffer.

* The environment variable ``PLATFORM_ARCH`` is available in the workshop
  session terminals and for use by any custom services run in the workshop
  container. A corresponding ``platform_arch`` data variable is also available
  for use in the workshop definition where appropriate, and in the workshop
  instructions. This variable is set to the value of ``TARGETARCH`` as set by
  ``docker`` when building container images and will have the value of ``amd64``
  or ``arm64``. The variable is to allow workshop content to be customized based
  on what CPU architecture the workshop container runs as.

* When Kyverno is installed as part of the Educates cluster essentials package,
  it is now possible to specify a replica count for the number of Kyverno
  instances to be run. This is to enable a high availability configuration to be
  setup.

* The workshop definition now supports ``envFrom`` (in addition to ``env``), so
  that environment variables can be sourced from secrets and config maps. This
  could previously be done using ``patches``, but ``envFrom`` makes it easier.
  The way ``envFrom`` is used is the same as for Kubernetes pods.

* The workshop definition now supports ``valueFrom`` when declaring an
  environment using ``env``, so that the values of single environment variables
  can be sourced from secrets and config maps. Use of the downward API to create
  environment variables set using attributes of the deployment is also supported
  using ``valueFrom``.

* When using the REST API to request workshop sessions, it is now possible to
  supply parameters along with the request. These parameters will be stored in a
  secret specific to the workshop session which is allocated for the request.

  The secret created can be mounted to the workshop pod as environment variables
  or a volume (both of which would delay actual start of the workshop pod until
  it is allocated to a workshop user), or it can be used in conjunction with
  deployments created from ``session.objects`` or a new ``request.objects``
  property, the latter which are only created at the point where the workshop
  session is allocated to the workshop user, rather than when a workshop session
  may be created in advance as a reserved session. The parameters are also
  available, in addition to the standard builtin data variables, when
  ``request.objects`` is expanded.

  This mechanism means it is possible to perform late binding of details about a
  workshop user, such as a SSO username, as well as provide a means to inject
  credentials for separate distinct systems into a workshop session, such as a
  separate Kubernetes cluster. Further improvements to this new feature are
  likely to be made in future versions to make it more flexible and more easily
  enable other use cases, such as sourcing workshop content from different
  locations on a per session basis.

  The Educates CLI has been enhanced to allow workshop sessions to be requested
  via the REST API, with ability to supply request parameters, for the purposes
  of testing, without the need for a custom front end portal.

  For more information see [Passing parameters to a session](passing-parameters-to-a-session)
  and [Resource creation on allocation](resource-creation-on-allocation).

* When using the Educates CLI to deploy a local DNS resolver on macOS, it is now
  possible to override the target IP address to which hostnames are mapped. This
  is to allow one to override the use of the default calculated IP address for
  the local host and replace it with an IP address created by adding an alias
  to a network interface. This is to allow you to use a fixed IP address for
  accessing cluster ingress, and avoid needing to recreate the local Educates
  cluster when the primary network interface changes, or where the machine is
  moved to a different network and the IP changes.

* When using the Educates CLI to deploy a local DNS resolver on macOS, it is now
  possible to supply a list of additional domain names which should be mapped to
  the cluster ingress IP address. This is to support cases where wanting to use
  custom domains other than just that which Educates uses.

Features Changed
----------------

* The countdown clock for a workshop session will now show the number of hours
  separate from minutes and seconds when the time remaining is an hour or more.

* When using a clickable action in workshop instructions which modifies an
  existing file via the embedded VS Code editor, an explicit save of the file
  changes will be triggered. Previously changes were saved as a result of the
  auto save feature being enabled. The change is to ensure changes are saved
  and are available for subsequent steps, even were auto save disabled.

* Dropped deprecated fields ``Image`` and ``Files`` from print listing when
  running ``kubectl get workshops``.

* Target Javascript version for Typescript code in dashboard gateway and
  workshop renderer is now ES2017 instead of ES5. Older versions of browsers
  not supporting ES2017 may therefore no longer work.

* Added ``kubectl`` clients for 1.26 and 1.27. Dropped ``kubectl`` clients for
  1.20, 1.21 and 1.22.

* Updated Coder VS Code version to 4.11.0.

* Updated Maven version to 3.9.1.

Bugs Fixed
----------

* Fixed possible race condition with creation of a new workshop session on
  demand when the REST API was used and the time between requesting a workshop
  session and activating it was very short. The consequence of this was that
  session activation might fail. The resource for a workshop session is now
  created in the context of handling the request for a workshop session, rather
  than as a background job, to avoid it not being ready by the time the session
  activation request comes in.

* In version 2.1.0, when the virtual cluster package used was updated, storage
  classes were no longer being copied from the host Kubernetes cluster to the
  virtual cluster. This meant that any deployment in the virtual cluster which
  relied on the default storage classes resource existing, would fail.

* When a virtual cluster was created and the host Kubernetes cluster used a
  service CIDR block other than the Kubernetes default of ``10.96``, networking
  would fail to work in the virtual cluster.

* When the OCI image artifact containing the Educates CLI was published to the
  GitHub container registry, it was not correctly tagging it with the release
  version and it was always using a ``latest`` tag.
