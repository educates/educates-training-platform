Version 3.6.1
=============

Bugs Fixed
----------

* When a `TrainingPortal` spec lists the same `Workshop` resource multiple times
  with different `alias` values (e.g., to offer the same workshop with different
  environment variable overrides), the training portal correctly identifies each
  `WorkshopEnvironment` by its alias internally. However, the alias is never
  persisted on the `WorkshopEnvironment` Kubernetes resource - only the original
  `Workshop` resource name is stored in `spec.workshop.name`. This means the
  lookup service lacked the information which identified the workshop was to be
  requested via its alias and such workshops could not be requested as it always
  tried to use the name on the `Workshop`. An extra label is now added to
  `WorkshopEnvironment` listing the name of the workshop to be used when it is
  requested, ie., the alias if defined or otherwise the name of the `Workshop`.
  This is now used by the lookup service to properly expose the workshop
  allowing to be requested.
