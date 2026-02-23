---
orphan: true
---

Version 2.2.1
=============

New Features
------------

* When entering full screen mode, when clicking on the icon to enable fullscreen
  mode, you can press shift while doing so and the result of this will be that
  the terminals in the main dashboard view will have their color scheme reversed
  so that black text on a white background is used. This makes readability
  better if using fullscreen mode for an interactive demo at a conference where
  a large screen projector is used.

* In the `Workshop` resource definition, you can now supply `session.volumes`
  and `session.volumeMounts` sections and these will be added into the
  `Deployment` resource definition for the workshop instance. In the case of
  `volumeMounts` these are only added to the main `workshop` container. If you
  need to mount volumes into one of the side car containers for some reason, you
  would still need to use `patches` to apply it.

* It is now possible to provide a `layout` setting pertaining to the embedded
  workshop instructions. This allows one to specify that the workshop
  instructions should be display in a tab of the workshop dashboard rather than
  to the left hand side.

Features Changed
----------------

* Where the `bash` terminal was used in the workshop instance, bracketed paste
  mode was previously disabled so that the clickable actions for executing a
  command in the terminal would work. If this wasn't done the newlines wouldn't
  actually be processed by `bash` and the command executed, until a newline was
  manually entered using the keyboard. When interacting with a separate `bash`
  instance outside of the workshop environment, such as one accessed using
  `kubectl exec` from another pod, or from a separate docker container, or VM
  instance, this wouldn't help and bracketed paste mode would have to be
  separately disabled in those shell environments for the clickable actions for
  commands to work.

  In this version bracketed paste mode is no longer disabled in `bash` and
  instead when clicking on an action to run a command in the workshop
  instructions, it will now be detected if bracketed paste mode is enabled in
  the front end terminal user interface and if it is, it will be disabled, the
  command injected into the terminal, and the prior state restored. This means
  that hacks to disable bracketed paste mode in any separate `bash` instance
  accessed from the terminal is no longer required.

  Note that bracketed paste mode will now be honored when using the clickable
  action for pasting text to the terminals where as before it wasn't since it
  was completely disabled.

* Updated version of Carvel tool suite.

* Updated versions of `yq`, `helm`, `skaffold` and `kustomize`.

* Updated version of Coder VS Code to 4.12.0.

* Updated `reveal.js` 4.X branch version to 4.5.0.

* Added `impress.js` 2.X branch version.

* Updated Java versions in Java workshop base images.

* Updated Gradle version to 7.6.1 in Java workshop base images.

* Updated Anaconda Python version to 23.3.1 in Python workshop base image.

* Updated `notebook` and `jupyterlab` versions in Python workshop base image.

Bugs Fixed
----------

* The Kyverno instance deployed as part of the cluster essentials component of
  Educates was not correctly pinned to the desired target version and the
  `latest` version of images were being used. This started to cause issues in
  the 2.2.1 alpha versions as the `latest` images were not completely compatible
  with some existing cluster policies for Kyverno as the custom resource
  definitions didn't match the latest version. Because image versions are pinned
  when creating Carvel packages, the older versions of the Educates packages
  should still work so long as GitHub container registry doesn't start garbage
  collecting images that aren't associated with a version tag.
