---
orphan: true
---

Version 2.0.6
=============

Also refer to release notes for prior patch revisions of Educates version 2.0.

* [Version 2.0.0](version-2.0.0)
* [Version 2.0.1](version-2.0.1)
* [Version 2.0.2](version-2.0.2)
* [Version 2.0.3](version-2.0.3)
* [Version 2.0.4](version-2.0.4)
* [Version 2.0.5](version-2.0.5)

Bugs Fixed
----------

* Eliminate thread race condition in session manager operator which could result in error when requests for multiple workshop environments were received at the same time, which is typical when training portals are used. The workshop environment for which the error occurred would be failed with a transient error and should have been cleaned up after a few minutes and creation retried, but would mean there was a delay in it appearing as available in the training portal web interface. This is a second attempt at fixing this issue. The change made in 2.0.5 for the same problem didn't resolve it.

* Fix potential for race condition error when new workshop session was created via the training portal and a workshop session against a different workshop environment had to be deleted in order to keep the training portal under the specified capacity. The result would be a server error being returned by the training portal web interface to the browser and the user would have needed to navigate back to the training portal workshop catalog page to try again. The change made in 2.0.5 for the same problem wasn't implemented correctly and created a new problem.
