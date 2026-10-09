The extension package image contract
====================================

An extension package image is an OCI image index with one child per supported
platform, built from a source directory holding a ``package.yaml`` manifest
beside the reserved ``common``, ``linux-amd64`` and ``linux-arm64``
directories. The manifest is copied to the root of each child image, each
child carries the label ``dev.educates.extension-package``, and a package is
referenced by tag rather than by digest. This is hard to reverse once packages
are published against it, by us and by anyone else.

The parts, and why each is fixed
--------------------------------

**The reserved directory names.** A platform directory overlays ``common``, so
one source produces every child and a package ships a different binary per
architecture under a single reference. Reserving the names rather than
configuring them keeps the source layout the whole contract: there is no build
file to read, and a reader can tell what a directory becomes by its name.

**The manifest at the image root.** It names and versions the package, and it
is what a workshop session tests for to confirm the package arrived. That test
is the reason the manifest must be at a fixed path in the image rather than
merely in the source: an image which is not a package unpacks perfectly well
and would otherwise leave the session quietly missing what the package
provides. Because the manifest is the test, it works under both deliveries and
for a package image built by other means.

**The label.** It records that an image was built to this contract, and
carries the contract version so a future change can be recognised rather than
guessed at. Nothing rejects an image for lacking it: the manifest is the test,
which is what allows a package image to be built with a ``Dockerfile`` where a
pipeline already builds images.

**The label's name.** The key follows the convention for third party image
labels. Docker asks a tool to prefix each label key with the reverse DNS
notation of a domain it owns, and the OCI image specification asks the same of
annotation keys while reserving ``org.opencontainers`` for itself;
``dev.educates`` is ``educates.dev`` reversed. It is the shape imgpkg uses to
mark a bundle, ``dev.carvel.imgpkg.bundle``, which vendir's ``imgpkgBundle``
source relies on through imgpkg, and the shape Cloud Native Buildpacks uses for
``io.buildpacks.*``. The value is the contract version, currently ``"1"``,
rather than a flag. The index carries the predefined OCI annotations
``org.opencontainers.image.title``, ``version`` and ``created`` rather than
keys of our own. ORAS and the OCI artifact conventions identify content by the
manifest's ``artifactType`` or a custom config media type instead, but those
describe artifacts which are not runnable images, and a package image has to
be a real image, because Kubernetes image volumes and Docker image mounts
mount nothing else.

**Tags, not digests.** A digest reference would pin a package to one index,
and an index is exactly what must be resolved per architecture at delivery. It
would also defeat the mount path, where the kubelet resolves the reference.
Reproducible builds are what make this safe: two publishes of one source give
the same digests, so a tag which has not been republished names the same
bytes, and ``--digest-file`` records what was published for a pipeline which
wants to pin.

Consequences
------------

Adding a platform means adding a reserved directory name, which every existing
package source may then optionally use. Changing the manifest shape or the
directory names breaks published packages, so a change of that kind needs the
contract version in the label to move and both versions to be accepted for a
period.
