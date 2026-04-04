Version 3.7.1
=============

Features Changed
----------------

* Injection of environment variables corresponding to Kubernetes services which
  exist in the same namespace of the workshop session container, are now
  disabled. These were not being relied upon and were polluting the set of
  environment variables for the session.
