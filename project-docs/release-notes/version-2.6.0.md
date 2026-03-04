---
orphan: true
---

Version 2.6.0
=============

New Features
------------

* Add option for having workshop instructions rendered using Hugo. This mode
  will be automatically used if neither of the files `workshop/workshop.yaml` or
  `workshop/modules.yaml` used by the original workshop renderer exist. The
  Markdown files for the workshop instructions should still reside in the
  directory `workshop/content`. By default page ordering for instructions will
  be based on the sorted order of the page filenames. If any page contains
  metadata specifying a page weight, then default Hugo page ordering will be
  applied instead using page weight and page title. In either case, the page
  title can be specified in the page metadata. Where necessary a
  `workshop/config.yaml` file can optionally be supplied when Hugo rendering
  mode is used. This file can be used to specify additional page variables to be
  available when doing page rendering, as well as explicitly define one or more
  navigation pathways through the workshop instructions. Hugo shortcodes are
  provided for optionally including parts of instructions based on the pathway
  being used, as well as shortcodes for admonitions. Clickable actions still
  work by using fenced code blocks, as was done using the original workshop
  renderer. For more details see [Hugo renderer
  configuation](hugo-renderer-configuration) and [Workshop
  Instructions](workshop-instructions).

* Added an experimental command to the `educates` CLI which allows workshop
  files and instructions to be hosted locally on the users machine. Workshop
  instructions in this case will be rendered using the Hugo based renderer, with
  customized details obtained from the live workshop session. This can be used
  in conjunction with the new feature for internally proxying to separately
  hosted workshop instructions, to have workshop instructions running locally,
  but still be embedded in the workshop dashboard for a live workshop session.
  When this is being done, the local Hugo server will be configured to run in
  active reload mode, meaning the local Markdown files can be edited and changes
  will be automatically reflected in the instructions displayed in the dashboard
  for the workshop session. For more details see [Modifying workshop
  content](modifying-workshop-content) and [Proxy to local workshop
  content](proxy-to-local-workshop-content).

* Added new data variables for session name, session hostname and session URL.
  In a workshop definition these are applied using `$(session_name)`,
  `$(session_hostname)` and `$(session_url)`. Note that the intent is that
  `$(session_name)` be used in many places where in the past
  `$(session_namespace)` would have been used. This is to separate the session
  name from the fact that the session may be deployed to a Kubernetes cluster
  and access to a namespace is provided. The `$(session_namespace)` variable
  should only now be used where wanting to actually refer to the Kubernetes
  namespace given to the workshop session. Similarly named variables are
  available for use in workshop instructions and shell environment.

* Added `$(workshop_image)` and `$(workshop_image_pull_policy)` variables that
  can be used in the workshop definition and which expand to the workshop base
  image, or custom workshop image that is being used by the workshop session,
  and a corresponding image pull policy. These can be used in the definition of
  any init containers defined for the workshop to perform setup steps, or in
  jobs, cron jobs, or any other deployments defined in the environment, session,
  or request `objects`.

* Add support for enabling an image cache for a workshop environment. This can
  be configured as an on demand pull through cache for any images hosted on a
  remote registry, or can be configured to mirror just a subset of images from a
  remote image. For more details see [Shared OCI image
  cache](shared-oci-image-cache).

* Added new variation for how remotely hosted workshop instructions can be used
  with a workshop session, where instead of the iframe for workshop instructions
  triggering a redirect to the remote site, an internal proxy can instead be
  configured. The result is that the embedded workshop instructions will be
  accessed from a browser using the same workshop session hostname, with the
  remote instructions being under the `/workshop/content` URL path. Because the
  workshop instructions appear under the same hostname, they can include
  Javascript which accesses the parent workshop dashboard directly, or which
  makes HTTP calls to the workshop dashboard, without encountering CORS issues.
  At the same time, the remotely hosted workshop instructions could also include
  the Javascript bundle used by the builtin workshop renderer for handling
  clickable actions. Thus if the remotely hosted workshop instructions use a
  compatible renderer for handling fenced code blocks, clickable actions can be
  used from the remotely hosted workshop instructions. For more details see
  [External workshop instructions](external-workshop-instructions).

* Added an ability to download aspects of the workshop session configuration
  from a workshop session over HTTP. These are protected by the workshop session
  cookie based authentication, but a special config password can be used as
  authentication token to allow external access by other services. The config
  password is available from within the workshop session, but is also available
  via a new REST API call from the training portal. The latter allows a custom
  portal frontend to obtain the config password for a particular session and use
  it to obtain more information directly from the workshop session. The config
  information access is provided to includes the list of workshop environment
  variables, list of data variables available for interpolation in workshop
  instructions, SSH keys and Kubernetes kubeconfig file. These could be used by
  a custom portal frontend to deliver up customized workshop instructions which
  are filled out with session specific details, to inject additional data into a
  workshop session, or push images to a per session image registry. For more
  details see [Retrieving session configuration](retrieving-session-configuration).

* When requesting a workshop session via the REST API of the training portal, it
  is now possible to override the default activation timeout of 60 seconds for a
  workshop session. The purpose in allowing this is that a custom frontend
  portal could request a workshop session, setting a much larger activation
  timeout, with the frontend portal doing its own polling of the workshop
  (possibly by accessing configuration from the workshop session), or by using
  other checks, thus determine if resources required by a workshop session are
  available, such as a full Kubernetes cluster, before actually passing the URL
  to a workshop user to access. The longer activation timeout means more time is
  provided before an unclaimed workshop session is automatically deleted. For
  more details see [Requesting a workshop session](requesting-a-workshop-session).

* Added ability to override the session cookie domain for training portal and
  workshop session cookies. These default to being for the same host, but to
  allow embedding into sites which use an alternate domain, you can now set
  the cookie domain to be a common parent domain. This can be set in the global
  Educates configuration, or in the training portal definition. For more details
  see [Overriding session cookie domain](overriding-session-cookie-domain) and
  [Allowing the portal in an iframe](allowing-the-portal-in-an-iframe).

* Added ability to list the hostnames of sites embedding the training portal and
  workshop sessions in the global Educates configuration as well as in the
  training portal. For more details see [Allowing sites to embed
  workshops](allowing-sites-to-embed-workshops).

* Exposed session termination mechanism via Javascript events, so it can be
  triggered from workshop instructions embedded from a remote site by specifying
  `workshop.url` in the workshop application definition. For more details see
  [Triggering actions from Javscript](triggering-actions-from-javascript).

* Exposed ability to preview an image via a popup dialog that spans the whole
  workshop dashboard, so it can be triggered from workshop instructions embedded
  from a remote site by specifying `workshop.url` in the workshop application
  definition. For more details see [Triggering actions from
  Javscript](triggering-actions-from-javascript).

* Added ability to provide a refresh interval for workshops listed in a
  training portal definition. When the specified duration has been reached,
  the workshop environment for that workshop will be marked for deletion and
  replaced with a new workshop environment instance. Any existing workshops
  in the workshop environment will be allowed to complete before final cleanup
  of the old workshop environment is done, with new workshops requests going
  to the new workshop environment in the process. For more details see
  [Refreshing workshop environments](refreshing-workshop-environments).

* It is now possible when triggering the creation of a dashboard, or reloading
  of a dashboard tab from workshop instructions, from a clickable action, to
  have the operation done without switching to the tab and giving it focus. For
  more details see [Clickable actions for the
  dashboard](clickable-actions-for-the-dashboard).

* When supplying additional themes via secrets, it is now possible to select
  one of these as a default theme in the global Educates configuration. If the
  name of the theme to use is also supplied in the training portal configuration
  that will take precedence over the global default. For more details see
  [Overriding styling of the workshop](overriding-styling-of-the-workshop).

* If needing to supply init containers for a workshop session, these can be
  specified in the workshop definition under `initContainers` and they don't
  need to be applied using `patches` anymore. For more details see
  [Adding extra init containers](adding-extra-init-containers).

* Add mechanism in the workshop definition to specify how a workshop OCI image
  should be constructed (using vendir snippets). This is now used by the
  `educates publish-workshop` command and at the minimum it is now required to
  specify in the workshop definition what the form of the target image name
  should be. This command will be used to replace how workshop OCI images are
  published from the Educates GitHub actions. For more details see [Publishing
  of workshop content](publishing-of-workshop-content).

* Add command line options to specify authentication credentials when using
  `educates publish-workshop` to publish a workshop OCI image.

* When using the `educates` CLI to publish/deploy/update a workshop in a
  Kubernetes cluster or local docker, the workshop definition file can now
  contain `ytt` template markup to allow simple customizations. When publish a
  workshop OCI image, or explicitly exporting the workshop definition, the
  result that is used is the output of running the workshop definition through
  `ytt`.

* It is now possible to override the order of any fixed dashboard tabs. For more
  details see [Adding custom dashboard tabs](adding-custom-dashboard-tabs).

Features Changed
----------------

* How one specifies that static HTML files are to be used for workshop
  instructions has changed. The `workshop.renderer` setting of the workshop
  application introduced in 2.5.0 for this purpose is now no longer used, with
  the location of any static HTML files for the workshop instructions needing to
  be explicitly specified using the `workshop.path` property. For more details
  see [Static workshop instructions](static-workshop-instructions).

* When workshop instructions are supplied as static HTML files hosted inside of
  the workshop container, the static HTML must have been created with the
  assumption that the base URL path is `/workshop/content`. Previously the
  required base URL path wasn't specified and `/` was used, however this could
  result in a conflict between URL paths for the dashboard and builtin workshop
  renderer static resources. For more details see [Static workshop
  instructions](static-workshop-instructions).

* When workshop files are downloaded, permissions on any `*.sh` files in
  `workshop/setup.d` will have file mode bits overridden so the files are
  executable. Similarly, when extension packages are downloaded, any `*.sh`
  files in the `setup.d` directory for that package will be made executable.
  This was done because `vendir` when downloading an archive over HTTP from a
  web server, or from a GitHub repository package release, does not preserve
  file mode bits when extracting the archive. This problem in `vendir` has been
  reported a long time ago and they still aren't inclined to fix it so this
  workaround is being used instead. Do note that a `setup.d` script will need
  to be provided to fix up permissions on any other files such as programs in
  a `bin` directory as only scripts in `setup.d` are being adjusted. For more
  details see [Hosting using a HTTP server](hosting-using-a-http-server),
  [Adding extension packages](adding-extension-packages) and
  [Shared assets repository](shared-assets-repository).

* The name of the volume holding the Kubernetes cluster access token has been
  renamed from `token` to `cluster-token`. The volume declaration is now always
  declared even if not mounted in the main workshop container, so it can still
  be used in init containers.

* The file duplicate file `~/.local/share/workshop/workshop-definition.yaml`
  has been eliminated. Use  `~/.local/share/workshop/workshop-definition.json`
  if need to directly access or edit the local workshop definition to customize
  dashboard behaviour from a `setup.d` script.

* The examiner clickable actions within workshop instructions could be chained
  together, such that clicking on one would result in the next one being run
  when the first one was complete. This ability to chain together clickable
  actions, with success resulting in the next one being automatically run can
  now be done for any clickable action. Similarly, having any clickable action
  automatically triggered when the page loads, or a section expanded, is also
  possible. For more details see [Automatically triggering
  actions](automatically-triggering-actions).

* If using the clickable action to reload a dashboard tab, the dashboard tab
  will now be created if it doesn't exist. This can now be used in place of the
  clickable action for creating a dashboard with it not erroring if the
  dashboard already existed. For more details see [Clickable actions for the
  dashboard](clickable-actions-for-the-dashboard).

* When deploying a local Kind cluster using the `educates` CLI, a `registry`
  service is created within the `default` namespace mapping to the a docker
  registry deployed in the docker daemon of the local host system. In the past
  this service propagated port 5001, but it now exposes the docker registry on
  port 80 instead, with it mapping internally to port 5001 on the docker
  registry. The `image_repository` data variable and `IMAGE_REPOSITORY`
  environment variable reflect the change so if using those as expected the
  change should not be noticeable.

* The service name for the workshop environment assets repository has been
  simplified to `assets-server` instead of `assets-$(workshop_namespace)`.
  The `assets_repository` data variable and `ASSETS_REPOSITORY` environment
  variable reflect the change so if using those as expected the change should
  not be noticeable. For more details
  see [Shared assets repository](shared-assets-repository).

* When the workshop environment assets repository has been configured such that
  it is exposed via a public ingress, the `assets_repository` data variable and
  `ASSETS_REPOSITORY` environment variable will use the public hostname rather
  than the internal service hostname. For more details
  see [Shared assets repository](shared-assets-repository).

* The underlying HTTP server used for the assets repository has been changed
  from nginx to a custom HTTP server written in Go. This new HTTP server
  supports the ability to download a directory of files as a tar or zip archive
  by using a URL path that maps to the directory and adding a suffix of form
  `/.ext` where `.ext` is one of the support archive formats. For more details
  see [Shared assets repository](shared-assets-repository).

* The default template for creating a new workshop using the `educates` CLI
  has been changed from `basic` to `classic`. A new template called `hugo` has
  been added which sets up workshop instructions file to use the Hugo renderer.

* The home directory of the workshop user, `/opt/assets` and `/opt/packages`
  directories are now all stored as part of the same volume. By default this is
  an `emptyDir` volume but would be a persistent volume if `storage` setting
  is provided for a workshop session. A consequence of this change is that an
  init container now must always be run to copy files from the home directory
  of the workshop base image into that volume. This change was made so that
  when setting `storage` all will be on the persistent volume, avoiding the
  possibility they are in ephemeral storage and using all space on the node.

* The separate Educates GitHub action for publish workshops has been updated to
  use `educates publish-workshop` command to create the workshop image and
  publish it. This occurred in `v6` of the GitHub action. This change means the
  workshop definition must include a `publish` section with an `image`
  definition so it can be worked out where the workshop image is being published
  and under what name. The workshop definitions published by this version of
  the GitHub action will require Educates 2.6.0 or later.

* The ability of the separate Educates GitHub action to publish multiple
  workshops has been removed, as has the ability to build a custom workshop base
  image.

Bugs Fixed
----------

* An attempt to view training portal information in the admin pages of the
  training portal would result in a HTTP 500 internal server error.

* When using EKS, the Kubernetes version returned by `kubectl` could have a
  suffix on the minor version string. This would cause problems when working
  out what version of the `kubectl` binary should be used in the workshop
  container, with a version mismatch being reported when `kubectl` was used.
  Any suffix on the `major.minor` version will now be stripped.

* Catch and ignore individual errors when querying Kubernetes API groups for
  resource details, in process of attempting to remove finalizers on resources
  when a Kubernetes namespace cannot be deleted. Previously if there was an
  error on a single API group it was causing the whole process to abort.

* When running periodic job to look for workshop and session namespaces which
  are stuck and cannot be removed, when determining finalizers to forcibly
  remove, if an API group entry in Kubernetes is mucked up, or custom resource
  definitions was somehow invalid, and accessing the details of an API group
  failed, then the whole process of trying to unstick the namespaces so they
  could be deleted was being aborted. Now catch when an unexpected error occurs
  in querying a single API group and keep going in attempt to forcibly delete
  the namespace.

* When providing your own certificate authority, inactivity timeouts on workshop
  sessions were not working as the CA was not beeing used when the training
  portal was accessing the workshop session and so the check for when the
  workshop session was last accessed was failing. The training portal now uses
  the internal Kubernetes service over plain HTTP to do the check rather than
  the public ingress.

* When submitting request parameters via the REST API, the validation of values
  was not being done correctly, which meant that if a non string value was
  submitted, a HTTP 500 Server Error would likely result, rather than HTTP Bad
  Request.
