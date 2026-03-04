---
orphan: true
---

Version 2.6.9
=============

New Features
------------

* Added top level aliases in the `educates` CLI for `create-portal`,
  `list-portals` and `delete-portal`, as well as aliases for `session-status`,
  `extend-session` and `delete-session`.

* Any clickable action can now set a `hidden` property. The result of this is
  that the clickable action will be hidden from view in the workshop
  instructions. Not being visible a user will not be able to click on the
  action, however it can still be triggered automatically if `autorestart`
  property is set, or if it follows a clickable action which has `cascade`
  property set. For more details see [Hiding clickable actions from
  view](hiding-clickable-actions-from-view).

Features Changed
----------------

* The `educates project version` sub command has been moved such that is now
  possible to use `educates version` instead to detemine the versions of
  Educates the CLI is for.

* The `educates cluster session terminate` command has bee changed to be
  `educates cluster session delete`. An alias has been kept for `terminate`
  mapping to `delete`.

Bugs Fixed
----------

* When deploying a workshop to the local docker environment, a local image
  registry was being created if one didn't exist, even though not strictly
  required. The registry will now not be created, but if the registry exists due
  to it being manually created using the `educates` CLI, or as the result of
  deploying a local Kubernetes environment using the CLI, the deployed workshop
  will be linked to the local image registry and a host entry injected so it can
  be accessed if required.

* The `educates` CLI help strings were showing an `options` sub command which
  didn't exist when it should have instead being directed users to use the
  `--help` option.
