---
orphan: true
---

Version 2.6.11
==============

Features Changed
----------------

* When running `educates publish-workshop` command with default registry of
  `localhost:5001`, it would check local docker environment to see if the image
  registry existed. This would preclude using the command inside of a container
  where the image registry may be mapped into the container from outside and
  docker wasn't available inside of the container, as publishing would then fail
  as a result. The command now assumes the image registry will exist and it is
  up to the user to ensure it has previously been deployed. The image registry
  should exist if `educates created-cluster` was used, or if needed for local
  docker workshop deployment, if `educates admin registry deploy` command had
  been run.

Bugs Fixed
----------

* When deploying a workshop to local docker environment using the `educates
  docker workshop deploy` command, the `$(platform_arch)` variable wasn't
  being substituted in the workshop definition.
