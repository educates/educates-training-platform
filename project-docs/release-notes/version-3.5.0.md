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
