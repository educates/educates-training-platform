---
orphan: true
---

Version 2.6.3
=============

Bugs Fixed
----------

* When using `SecretCopier` or `SecretExporter`, if a target secret was
  originally created with no data in it, and the source secret was subsequently
  updated, then the target secret would fail to be updated. If the rule which
  triggers this was just one rule of a set of rules, the subsequent rules may
  not have been applied due to the uncaught error of failing to deal with a
  target secret which had no data.
