---
orphan: true
---

Version 2.6.4
=============

Bugs Fixed
----------

* When using the `educates serve-workshop` CLI command to host workshop content
  in live reload mode embedded in a workshop session, and default sorting based
  on page name was relied on, when a change was made to any page the pages
  would then display in the wrong order. This was due to a bug in Hugo which
  although reported last year and a patch provided by another Hugo user, has
  still not been released as a fix to Hugo. A workaround for the bug in Hugo
  has now been implemented in the Educates `hugo` renderer to avoid the issue.
