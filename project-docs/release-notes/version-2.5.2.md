---
orphan: true
---

Version 2.5.2
=============

Features Changed
----------------

* When using an extension package, any `supervisord` config files now need to
  be located in the `supervisor` directory instead of `etc/supervisor` to be
  consistent with directory layout for workshop files.

Bugs Fixed
----------

* Downloading of workshop assets and packages would be marked as having failed
  if the files were injected via a volume mount as permissions on files and
  directories could not be updated.

* When accessing a workshop container using `kubectl exec` and a bash login
  shell was started, the wrong directories were being searched for `profile.d`
  script files.

* Was not correctly catching backend errors when the front end was making REST
  API calls to get details of the remaining time for a session, extending a
  session or reporting events. The result for the first case was that the
  countdown clock would stop decrementing and would not be displayed if the
  dashboard was reloaded. This problem was being triggered due to the access
  token used by the workshop container to make API requests not being refreshed
  when it expired after 10 hours. Errors are now caught correctly and the access
  token is refreshed prior to it expiring.
