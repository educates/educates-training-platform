Version 3.5.0
=============

Features Changed
----------------

* Updated supported Kubernetes versions to 1.31 through 1.34. Support for older
  versions have been dropped.

* Updated Coder version of VS Code to 4.104. A newer version does exist, but
  it currently has issues with being able to disable the AI chat panel.

* Updated various tools bundled with workshop image to latest version. Eg.,
  Hugo, Carvel tools, `kubectl`, `yq`, `uv`, `dive`, `bombardier` etc.

* Updated version of Kind used for local Educates cluster.  Now creates
  Kubernetes cluster using Kubernetes 1.34.

* The Docker API version is now negotiated so that `educates` CLI is better
  able to support a wider range of Docker Desktop versions.

* The `educates new-workshop` command now supports new options for controlling
  what settings are enabled in the generated workshop definition.

* Updated version of `vcluster` to 0.30.2, and also removed `k3s` distro support 
  as it defaults to `k8s` distro and resources for `k8s` distro are not configurable. Supported k8s versions are 1.31 through 1.34.

* Now, vcluster internal ingress controller deploys only 1 contour pod replica, if enabled.

Bugs Fixed
----------

* A subset of the workshop security rules enforced by Kyverno were only being
  applied to the first workshop deployed to an Educates cluster and were not
  being applied to any workshop deployed after that. If you had a workshop
  which unknowingly was working okay, but now find fails due to proper
  enforcement of workshop security rules, you will need to add an exclusion
  for the failing rule to the workshop definition.

* Syncing services to/from vcluster was not working.
