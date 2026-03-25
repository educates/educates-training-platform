---
orphan: true
---

Version 2.6.16
==============

New Features
------------

* In special case where the workshop base image was being used to run services
  in a separate deployment from the workshop pod, using the default command for
  the image, it is now possible to set the `SUPERVISOR_ONLY` environment
  variable. This will have the affect of ensuring the gateway process which runs
  the workshop dashboard, and other processes such as the editor and console are
  not run, unless explicitly enabled by respective environment variables.
  Further, in this mode, if an error occurs in downloading files using `vendir`
  or when running setup scripts, the start script for the container will be
  exited, with the result that the pod will be restarted, instead of ignoring
  the errors and continuing on to running the process supervisor.

Features Changed
----------------

* Examiner functionality was incorrectly enabled by default in the workshop base
  image. If a workshop had inadvertantly been making use of this it may now
  fail, in which case it should explicitly enable the examiner feature in the
  workshop defintion as was originally intended to be done.

* If the Hugo renderer is being used, and there are no workshop instructions
  or an error occurs when rendering the instructions, a default page will be
  shown in the workshop instructions with a warning that an error occurred,
  rather than showing a HTTP page not found error.

Bugs Fixed
----------

* When an error occured rendering workshop instructions using the Hugo renderer,
  the details were not logged to the `setup-scripts.log` file accessible from
  inside of the workshop container, and were only visible by using `kubectl log`
  on the pod corresponding to the workshop container.
