---
orphan: true
---

Version 2.6.15
==============

Note that this release primarily related to changes in development processes,
packaging and release, rather than functionality changes or bug fixes. Thus be
observant for any issues related to configuration and installation in both
local development and production environments.

New Features
------------

* Added new custom labels for describing aspects of a workshop. Note that this
  is distinct from Kubernetes metadata resource labels and is defined within
  the workshop specification itself.

Features Changed
----------------

* Updated versions of Conda and Jupyter notebook packages used in the
  `conda-environment` workshop base image.

* The generated `id` field returned by the REST API for a workshop when listing
  workshops hosted buy a training portal has been removed as there is no valid
  way one can generate a unique identifier suitable to determine if two deployed
  workshops are actually the same. If a workshop author wants to provide their
  own unique workshop identifier, they can use labels instead.

Bugs Fixed
----------

* Restored mapping for local image registry located at `localhost:5001` in
  `containerd` of Kind cluster created for local Educates environment. This
  previously existed but was removed, but in doing so it broke ability to use
  Carvel packages which referenced docker images from the local image registry.
