Version 3.3.3
=============

New Features
------------

* Added a ``baseurl`` short code for when using Hugo. This will resolve to
  ``/workshop/content`` URL path, which is where workshop instructions reside.
  This short code is to avoid hard coding ``/workshop/content`` when adding
  images to the ``static`` directory and referring to them from markdown. This
  will allow the URL path workshop instructions appear under to be modified at
  some point or when generating them in a different way, for example, to create
  an offline set of HTML static files for viewing. For more information see
  [Embedding images and static assets](embedding-images-and-static-assets).

Features Changed
----------------

* The ``EDITOR`` environment variable is now set to ``/usr/bin/vim`` for
  terminal sessions as some tools can require the ``EDITOR`` environment
  variable be set.

* For reasons that have been forgotten, the ``XDG_CONFIG_HOME`` environment
  variable was being set to ``/tmp/.config``. Overriding this was problematic
  when some tools/scripts assumed standard location of ``~/.config``, but
  others honoured ``XDG_CONFIG_HOME``. No longer setting this environment
  variable. If a workshop has issues when standard location is used, it will
  need to override it as part of workshop.

* Update Anaconda Python versions in ``conda-environment`` workshop base image.

* Docker images, including workshop base images, updated to use Fedora 42.

* Updated versions of Java 8, 11, 17 and 21. Maven has also been updated to
  latest available at this time. Gradle remains at version 8.5 for Java 8, 11,
  and 17, and version 8.8 for Java 21, due to build issues.

Bugs Fixed
----------

* The custom resource definition for `Workshop` was missing the `path` property
  when defining `ingresses`.
