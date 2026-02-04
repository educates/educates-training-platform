Version 3.6.0
=============

New Features
------------

* Examiner test scripts can now be provided as part of an extension package.
  These should be placed under the `examiner/tests` directory of the package.

* When using editor clickable actions which accept `start` or `stop` params,
  you can now supply negative values. These will be interpreted as offset from
  the end of the file.

* When using editor clickable actions which accept `before` or `after`, if you
  supply `-1`, it will be interpreted as all lines before or after.

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
