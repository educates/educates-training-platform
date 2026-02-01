Version 3.6.0
=============

New Features
------------

* Examiner test scripts can now be provided as part of an extension package.
  These should be placed under the `examiner/tests` directory of the package.

Features Changed
----------------

* When using the test examiner feature, test scripts can now be put in a sub
  directory of the `/opt/workshop/examiner/tests` directory. The name of the
  sub directory path should then prefix the test name when using the clickable
  action for the test. Checks when running test examiner scripts have also
  been beefed up to ensure that directory traversal cannot be used to execute
  a program which resides outside of the tests directories.
