Package images are fetched by a purpose built fetcher, not vendir
=================================================================

Where an extension package declared as an image cannot be mounted, its
contents are fetched by ``educates-package-fetcher``, a small Go program built
into the workshop base image, rather than by ``vendir``. Packages declared
with ``files`` are still downloaded by ``vendir``, so a reader will find two
fetch paths in one container and should know why.

Considered options
------------------

Reusing ``vendir`` was the obvious option, since it is already in the base
image and already downloads packages. It fails on two requirements of the
package image contract.

It cannot resolve an image index to the architecture the session runs on. A
package image is published as an index with one child per architecture, so
that an author writes one reference whichever node a session lands on.
``vendir`` would need a per-architecture reference, which would either push
the architecture into the workshop definition, where the author cannot know
it, or require the renderers to construct one, which means the renderers must
know what the node will be. The mount path has no such problem, because the
kubelet selects the child, so using ``vendir`` would make the two deliveries
behave differently for the same declaration.

It also does not preserve file permissions. ``vendir`` drops the execute bit
when unpacking, which is why packages declared with ``files`` need a
``setup.d`` script to repair their own permissions. A package which may be
mounted read only cannot repair anything at startup, so the permissions have
to survive delivery. The fetcher preserves the modes the package was published
with, which makes one contract hold under both deliveries.

Consequences
------------

The base image carries a second fetching tool, and its own Go build stage, for
a binary of the order of fifteen megabytes. The two paths are kept apart
rather than unified: ``vendir`` handles content and ``files`` packages, the
fetcher handles package images, and each is configured by its own file so that
the presence of that file is what decides whether it runs.

The session derives which packages were mounted from the absence of a fetcher
entry, so anything writing that configuration must leave mounted packages out
of it.
