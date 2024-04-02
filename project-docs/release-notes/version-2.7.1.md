Version 2.7.1
=============

Bugs Fixed
----------

* If a `SecretCopier` contained multiple rules and a namespace was matched by a
  rule which was in a terminating state, the attempt to create the secret in
  that namespace would fail but not be caught. This meant that any rules which
  followed that rule were not being applied on that pass and would only be
  applied some time later after the terminating namespace had finally been
  deleted. To reduce chance of this occuring, a namespace which is not in the
  active state will be skipped for matching. Also, any unexpected exception
  will be explicitly caught and logged rather than being propogated back to the
  caller.
