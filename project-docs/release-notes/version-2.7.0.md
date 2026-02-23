---
orphan: true
---

Version 2.7.0
=============

New Features
------------

* Added the clickable action ``terminal:select`` such that a specific terminal
  session can be selected and given focus, without needing to send any command
  or text to the terminal. If the dashboard containing the terminal is hidden,
  the dashboard will first be exposed. This is mainly of use when wanting to
  expose a dashboard tab containing multiple embedded terminals, and you want
  to ensure a specific terminal is given focus, rather than the last used in
  that dashboard tab.

* It is now possible to provide a list of custom labels with a training portal
  definition for the purposes of identification of a training portal when
  interacting with multiple training portals via their REST APIs. These custom
  labels are distinct from Kubernetes resource labels. They will be returned
  along with the portal details when using the REST API of a training portal to
  get the list of workshop environments.

* It is now possible to provide additional custom labels for workshops hosted
  from a training portal for the purposes of identification of a workshop when
  interacting with a training portal via it's REST APIs. These custom labels
  supplement labels defined in the workshop definition. If the additional label
  is defined for a specific workshop, it will override any in the workshop
  definition if there is a name conflict. If an additional label is defined as a
  default for all workshops hosted by the portal, and a name conflict occurs,
  that in the workshop definition will still take precedence. The labels will be
  returned along with the workshop details when using the REST API of a training
  portal to get the list of workshop environments.

* When using the REST API of the training portal to get a list of workshop
  environments, it is now possible to filter list of workshops returned based on
  workshop name and workshop labels. For more details see [Listing available
  workshops](listing-available-workshops).

* Add new training portal REST API endpoint for returning available workshops
  where rather than returning list of workshop environments with the workshop
  details nested within it, instead return list of workshop details with the
  details of the current running workshop environment nested within it. This
  does not provide a way of getting access to workshop environments which are
  currently in the process of being stopped and the existing REST API endpoint
  for getting list of workshop environments should be used for that. For more
  details see [Listing available workshops](listing-available-workshops).

* Add new training portal REST API which returns details for a single workshop
  environment by name. For more details see [Workshop environment
  status](workshop-environment-status) and [Listing all workshop
  sessions](listing-all-workshop-sessions)

* Added `jdk21-environment` workshop base image.

* Added ability to map services from/to a virtual cluster. To support this, also
  added a `$(vcluster_namespace)` variable that can be used in the workshop
  definition to refer to the namespace used by virtual cluster control plane.
  For more details see [Provisioning a virtual
  cluster](provisioning-a-virtual-cluster).

* A default command can now be specified for all terminal sessions by supplying
  a ``terminal.sh`` file in the ``workshop`` directory. Where a terminal script
  exists for a specific session at the same time, it will take precedence.

* When adding an additional ingress points for a workshop session and the target
  uses the ``https`` protocol, it is now possible to supply the ``secure``
  property set to ``false`` to indicate that certification verification should
  be skipped. For more details see [Defining additional ingress
  points](defining-additional-ingress-points).

* Path based prefix routing can now be used in conjunction with additional
  workshop ingress points. This allows one to combine multiple backend services
  under one ingress hostname at different URL paths. If necessary the path can
  be rewritten as it passes through the proxy. For more details see [Defining
  additional ingress points](defining-additional-ingress-points).

Features Changed
----------------

* The version used for the local Kubernetes cluster has been updated to version
  1.27.

* Updated version of Miniconda used in Python workshop base image as the conda
  package resolver would fail to run when rebuilding the workshop base image
  using older version of Miniconda.

* Now set connect and server timeouts for Kubernetes REST API connections
  created by kopf based operators. This is to try and avoid reported problems of
  the operators not working on gke clusters when the number of nodes in the
  Kubernetes cluster is scaled up or down.

* The current working directory for an examiner test script is now set to the
  home directory of the workshop user. Previously it was wrongly inheriting the
  working directory of the workshop dashboard process.

* When using clickable actions that act against a specific terminal session,
  that terminal should now remain selected and keep focus such that any text
  subsequently entered manually will be directed to the terminal. In the case
  of a clickable action acting on multiple terminal sessions, the first terminal
  session will keep focus.

* If a dashboard tab is exposed which contains a single embedded terminal, the
  terminal will now automatically get focus and text can be entered without
  needing to first select the terminal. If a dashboard tab contains multiple
  terminals, then the default terminal (first) will automatically be selected
  the first time the dashboard tab is exposed, or the last terminal which had
  focus if the terminals in the dashboard tab had previously been used.

* When a clickable action is used which targets an embedded terminal, that
  terminal will now always get focus, even if the terminal were on a hidden
  dashboard tab and the dashboard tab had to be exposed first.

* The version of Bootstrap used by workshop dashboard, instructions renderer and
  training portal user interface upgraded from 4 to 5. This required some
  structural changes to HTML which may affect theme overrides. Some existing
  colours in use may have changed due to changes in Bootstrap defaults.

* The version of FontAwesome package used by workshop dashboard and instructions
  rendererer upgrade from 5 to 6.

* When redirected back to the training portal using a URL which contained a
  query string parameter for the purposes of displaying a notification banner,
  the URL as shown by the browser is rewritten so as not to include the query
  string parameters. Reloading the page or bookmarking the page for later use
  will now not result in the notification being persisted.

* The version of Django used by the training portal was update from 3 to 4. This
  necessitated changes to trust model for CSRF tokens and CORS to make it more
  strict on allowed domains. This may have implications where workshops are
  being embedded within a separate web site so testing should be done before
  switching to the newer version of Educates.

* The version of Fedora used by workshop base images, and other container images
  used by Educates operator and training portal, was updated to 39.

* Update Kubernetes tools such as kubectl, kustomize, skaffold, Carvel tools,
  k9s etc to latest available version.

* Update of VS Code editor version to latest available.

* Update of tools such as jq, yq, dive etc to latest available version.

* Bracketed paste mode was re-enabled for the workshop dashboard terminals. This
  was previously explicitly disabled in `bash` terminals as the frontend user
  interface implementation required it to be in order for clickable actions
  which paste into the terminal to work. The issue with the frontend user
  interface was previously addressed, but the option to disable bracketed paste
  mode in `bash` wasn't removed at the time when it should have.

* When using a editor clickable action, you could use a target path starting
  with `~/` to denote a file relative to the home directory of the workshop
  user. You can now also use the prefix `$HOME/`. Both are to avoid hard coding
  the `/home/eduk8s` path, which could in the future change if the name of the
  workshop user were ever changed.

* Updated version of vcluster package used. Virtual clusters will now default to
  being created using Kubernetes 1.27. Kubernetes versions 1.22 through 1.24 are
  no longer supported and if selecting Kubernetes versions must be in range 1.25
  through to 1.28.

Bugs Fixed
----------

* Kubernetes resource generation numbers were stored in the training portal
  database as the Django IntegerField type. This can only hold signed 32 bit
  values but the Kubernetes resource generation number can technically grow
  larger than 32 bits and uses 64 bits. When workshop updates are enabled, this
  might cause workshop environments to be refreshed whenever an event is
  generated for a workshop definition, even if there was no actual change as
  the comparison of the generation number would suggest it had changed since
  an incorrect value would have been stored in the database. The generation
  number was also stored for the training portal resource as well but it was
  not used internally beyond exposing it in data for a training portal via the
  REST API.

* Filtering workshop environments based on state when requesting catalog of
  workshops via the training portal API wasn't working and only workshop
  environments in running state were ever returned.

* When using ``env`` defaults for all workshops, or specific workshops in the
  training portal definition, any changes to these in the training portal
  definition after the initial workshop environment had been created for a
  workshop, were not being reflected in subsequent workshop sessions created
  after that point.

* Processing of Kyverno rules was not correctly injecting a namespace selector
  targeting workshop session namespaces when the Kyverno rule used a `match.any`
  or `match.all` condition. Consequence was that these rules were being applied
  to all namespaces and thus could have affected other applications deployed to
  the Kubernetes cluster besides Educates. The affected rules were
  `disallow-ingress-nginx-custom-snippets`, `restrict-annotations`
  `restrict-ingress-paths` and `prevent-cr8escape`.

* Including TLS wildcard certificates embedded in the data values file was not
  working as there was a typo when looking up the data value. This meant that
  a secret was not created for the TLS wildcard certificate when embedded in
  data values file and Educates was only configured for plain HTTP and not HTTPS.
  This issue was inadvertantly added when support was added for supplying the
  TLS wildcard certificate and CA secrets as actual secrets rather than
  embedded in the data values file.

* The generated CA secret was incorrectly setting the secret type to
  `kubernetes.io/tls` which resulted in Kubernetes rejecting it as it didn't
  contain `tls.crt` and `tls.key` data attributes as required by Kubernetes
  for that type of secret. Secret type should have been left as default generic
  opaque data secret. This issue was inadvertantly introduced when support was
  added for providing the CA secret as an actual secret rather than being
  enmbedded in the data values file when deploying Educates.

* Addressed possible issue with training portal whereby if a transient error
  occurred when looking up workshop environment using Kubernetes REST API
  immediately after creation, that database update would be rolled back but
  the workshop environment in the cluster would still exist, meaning that an
  attempt would me made to use the same workshop environment name the next
  time one is created, resulting in a conflict and inability to create any
  new workshop environments against that training portal.
