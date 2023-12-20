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

* Update of tools such as jq, yq, device etc to latest available version.