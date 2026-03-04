---
orphan: true
---

Version 2.7.2
=============

Upcoming Changes
----------------

For details on significant changes in future versions, including feature
deprecations and removals which may necessitate updates to existing workshops,
see [Upcoming changes](upcoming-changes).

Bugs Fixed
----------

* A workshop environment could technically get stuck in `STARTING` state as seen
  by the training portal if the kopf operator framework coalesced events for
  `ADDED` and `MODIFIED` together and only reported a single `ADDED` event. This
  is because the training portal was only looking for a `MODIFIED` event. Thus
  it could miss when the workshop details were updated in `WorkshopEnvironment`
  and so not mark the workshop environment as `RUNNING`.

* It was not possible through the training portal admin pages to forcibly
  refresh a workshop environment that was stuck in `STARTING` state.
