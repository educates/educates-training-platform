Version 3.6.0
=============

Bugs Fixed
----------

* When printing workshop instructions only the first page would be printed.
  This has only been fixed for Hugo renderer and not the deprecated classic
  renderer.

* When using the `terminal:execute-all` clickable action, if `clear` was set
  to `true`, the terminals were not being cleared.

New Features
------------

* It is now possible to restrict permissions given to the session manager so
  that it does not have full cluster admin access. Workshops which only need
  access to the session namespace will still work, but workshops will not be
  able to be setup to use cluster wide resources such as custom resources for
  operators. For more details see
  [Restricting session manager permissions](restricting-session-manager-permissions).

* Examiner test scripts can now be provided as part of an extension package.
  These should be placed under the `examiner/tests` directory of the package.

* When using editor clickable actions which accept `start` or `stop` params,
  you can now supply negative values. These will be interpreted as offset from
  the end of the file.

* When using editor clickable actions which accept `before` or `after`, if you
  supply `-1`, it will be interpreted as all lines before or after.

* You can now set `toggle: false` on `section:end` clickable action. In this
  case if have prior clickable action which cascades to `section:end`, the
  section will not be closed. If also have `cascade` set on `section:end`
  then, clickable action following `section:end` will still be triggered. Thus
  can automatically trigger clickable action after a section without closing
  the section.

Features Changed
----------------

* When using the test examiner feature, test scripts can now be put in a sub
  directory of the `/opt/workshop/examiner/tests` directory. The name of the
  sub directory path should then prefix the test name when using the clickable
  action for the test. Checks when running test examiner scripts have also
  been beefed up to ensure that directory traversal cannot be used to execute
  a program which resides outside of the tests directories.

* Enhanced session and terminal reconnection logic to prevent rapid reconnection
  attempts. The system now implements increasing delays between retry attempts
  using exponential backoff, eventually ceasing reconnection efforts entirely
  after a specified duration. This prevents excessive browser activity when
  session connectivity is disrupted.

* When using `cooldown` value for any clickable action, you can now use `-1`
  to indicate an infinite period of time, ie., block triggering clickable
  action again.

* When using `retries` value with `examiner:execute-test` clickable action,
  you can now use `-1` to indicate an infinite number of retries.
