---
orphan: true
---

Version 2.6.10
==============

New Features
------------

* When a training portal has an event access code, you can now supply with the
  HTTP GET request for the login URL to the training portal, the password as a
  HTTP request query string parameter. This way you can share a URL which will
  automatically login a workshop user to the training portal.

* When running the `educates deploy-workshop` command you can now supply the
  `--open-browser` option and when the training portal hosting the workshop is
  ready the browser will be automatically opened on the training portal and you
  will be logged in. You will still need to select the workshop to run it
  from the training portal web interface.

Features Changed
----------------

* When deploying a workshop using the `educates deploy-workshop` command and a
  training portal instance is created as a side effect, or when explicitly
  creating a training portal instance using `educates create-portal`, the
  default for maximum number of concurrent workshop sessions is now 5 instead
  of the previous value of 1.

* When deploying a workshop using the `educates deploy-workshop` command the
  capacity for the workshop is now by default not set, meaning how many
  concurrent workshop sessions you can create will be capped by what the maximum
  number of sessions is for the training portal instance the workshop is
  registered with.

* When running `educates browse-workshops` to open the browser on the training
  portal, a URL will be used which will automatically log you into the training
  portal. The training portal credentials will only need to be supplied were
  the training portal URL was shared with another party and they were accessing
  the URL directly.

* The `educates request-workshop` alias has been removed. This primarily existed
  for testing the training portal REST API and is not something that would be
  used regularly. You can still use the `educates cluster workshop request`
  function if you need this functionality.

Bugs Fixed
----------

* When using the `educates cluster workshop request` command to test the
  training portal REST API, when the workshop was exited you would be sent back
  to the training portal, where if you entered the training portal access code
  you would get stuck in a browser redirect loop. When returning back to the
  training portal, given that it was a unique anonymous session due to the use
  of the REST API, you will now be logged out to clear the cookie state for the
  session and required to login again.
