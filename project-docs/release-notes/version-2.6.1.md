---
orphan: true
---

Version 2.6.1
=============

Bugs Fixed
----------

* In version 2.6.0, the `host` field of an additional ingress in the workshop
  definition was inadvertantly made a required field when previously it could be
  left out and would default to `localhost`. Behaviour was reverted back so
  `host` field is optional.

* In version 2.6.0, the changes to workshop instruction renderers caused the
  dashboard gateway process to fail on startup when workshop instructions were
  disabled.
