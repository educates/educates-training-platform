---
orphan: true
---

Version 2.5.0
=============

New Features
------------

* When using `files:download-file` and `files:copy-file`, it is now possible to
  supply a `url` property giving an alternate backend from which the file should
  be downloaded. Because of cross domain restrictions, the hostname for the
  alternate backend service must fall within the same parent ingress domain used
  by Educates. For more details see [Clickable actions for file
  download](clickable-actions-for-file-download).

* When using the clickable action for `examiner:execute-test`, it is now
  possible to supply a `url` property giving an alternate backend service to
  which the HTTP POST request for the action will be delivered. Because of cross
  domain restrictions, the hostname for the alternate backend service must fall
  within the same parent ingress domain used by Educates. This mechanism could
  be used in conjunction with a separate service from the workshop session so
  that any steps taken to check a users progress is not run in the workshop
  container itself, avoiding any problems with a user interfering with the
  checks. For more details see [Clickable actions for the
  examiner](clickable-actions-for-the-examiner).

* Add the ability to specify an additional set of inputs which should be
  supplied by a user for the `examiner:execute-test` clickable action. In order
  to allow the input values to be supplied a HTML form will be rendered as part
  of the clickable action. This mechanism can be used to implement quiz like
  questions which require user input, or could be used to capture data to
  customize a workshop session and any subequent instructions. For more details
  see [Clickable actions for the examiner](clickable-actions-for-the-examiner).

* Added new clickable actions `files:upload-file` and `files:upload-files` for
  uploading one, or multiples files into the workshop container from the users
  local machine. For more details see [Enabling workshop
  uploads](enabling-workshop-uploads) and [Clickable actions for file
  upload](clickable-actions-for-file-upload).

* Added distinct option for workshop instructions to be provided by static files
  hosted from the workshop container. This is in place of using the files
  download feature to use static files for workshop instructions. For more
  details see [Static workshop instructions](static-workshop-instructions).

* Added binary for Hugo static site generator, to enable workshop setup scripts
  to generate workshop instructions as static files.

Bugs Fixed
----------

* When a webhook was supplied for analytics, and the front end workshop
  dashboard had lost the network connection with the workshop session,
  attempting to terminate the session would get stuck on the cleaning up session
  cover page.
