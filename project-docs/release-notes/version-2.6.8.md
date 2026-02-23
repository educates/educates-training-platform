---
orphan: true
---

Version 2.6.8
=============

New Features
------------

* A new experimental Docker Desktop extension is now provided for deploying
  workshops to a local docker environment. This provides a extension UI in
  Docker Desktop which under the covers uses `educates docker workshop deploy`
  to deploy workshops.

* The test examiner script URL endpoints can now be accessed from outside the
  context of the workshop session dashboard by supplying the services password
  as `token` query string parameter to the HTTP API call.

* Any clickable action can now set a `hidden` property. The result of this is
  that the clickable action will be hidden from view in the workshop
  instructions. Not being visible a user will not be able to click on the
  action, however it can still be triggered automatically if `autorestart`
  property is set, or if it follows a clickable action which has `cascade`
  property set. For more details see [Hiding clickable actions from
  view](hiding-clickable-actions-from-view).

* When using `educates deploy-workshop` you can now specify `--image-repository`
  option and the `registry` setting will be added against the workshop in the
  training portal with the supplied value replacing value of
  `$(image_repository)` in the workshop definition. This can be useful when
  using `educates publish-workshop --image-repository ...` to publish a workshop
  to a remote image registry and you need to test the deployment of the workshop
  using the remote image registry.

* When using `educates docker workshop deploy` you can force the use of a
  builtin workshop base image from the local image registry at `localhost:5001`
  used during development of Educates, by supplying `--image-version` as
  `latest`. You can also completely override what workshop image is used by
  instead setting the `--workshop-image` option.

* A Linux arm64 version of the `educates` CLI is now provided.

Features Changed
----------------

* When using `educates new-workshop` the Hugo workshop template, rather than
  the classic renderer template, is now the default.

* When using `educates serve-workshop --patch-workshop`, the access token to
  secure the connection is now generated the first time it is required and
  cached, so that it used for subsequent runs, rather than generating a new
  access token each time. This makes it possible to exit the CLI and run again
  while still in the same workshop session. If you need to force the replacement
  of the cached access token, you can use the `--refresh-token` option. For a
  specific run, you can still set your own access token using `--access-token`.

* The Hugo `param` shortcode has been overridden to return an empty value when a
  referenced data variable does not exist, or is set but empty. The original
  Hugo implementation for the `param` shortcode would error when a data variable
  existed but was empty as it couldn't distinguish between not set an empty.
  This makes it different to the original Hugo behaviour as it will now not
  raise an error when mistakenly reference a wrong variable name, but in context
  of workshop instructions, is preferred that rendering isn't completely broken
  in this case.

Bugs Fixed
----------

* Data variables for the Git server and image registry were not being made
  available when using the Hugo renderer for workshop instructions.

* The clickable action for file uploads was broken in 2.6.0 when the workshop
  user home directory was always placed in a volume. The feature also possibly
  didn't work before that point either when persistent storage was requested
  for the workshop session.
