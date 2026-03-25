---
orphan: true
---

Version 2.6.6
=============

Bugs Fixed
----------

* When deploying a workshop to a local docker environment using the `educates`
  CLI, the setup scripts within the workshop session container would always be
  flagged as having failed. This was occurring as the builtin scripts were
  assuming that the SSH private/public keys always existed, but these are not
  created and injected into a workshop session when deploying to a docker
  environment, only when deploying to a Kubernetes cluster.
