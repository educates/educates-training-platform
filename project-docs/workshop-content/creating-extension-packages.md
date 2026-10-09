(creating-extension-packages)=
Creating Extension Packages
===========================

An extension package bundles up an application, and any scripts needed to set
it up, so that workshops can use it without each workshop having to install it
and without building a custom workshop base image. A package is published once
as an image and declared by any workshop which needs it. See [adding extension
packages](adding-extension-packages) for how a workshop declares one.

This page covers creating and publishing a package. It describes the package
image form, which is the preferred way to publish a package. A package can
also be supplied as files for ``vendir`` to download, which remains the way to
work on a package locally or to bring one in from a Git repository or a web
server.

(package-source-layout)=
Layout of a package source
--------------------------

A package source is a directory holding a package manifest beside up to three
reserved directories:

```text
argocd/
  package.yaml
  common/
    setup.d/
      01-configure.sh
  linux-amd64/
    bin/
      argocd
  linux-arm64/
    bin/
      argocd
```

The ``common`` directory holds the files every platform shares, and each
platform directory holds the files for that platform alone. A platform
directory overlays ``common``, so a file in both is taken from the platform
directory. This is how one package ships a different binary per architecture
under a single image reference.

Only ``linux-amd64`` and ``linux-arm64`` are supported, and every directory is
optional. A package which ships no architecture specific files needs only
``common``, and a package which shares nothing needs only the platform
directories.

The layout above is all the ``educates package publish`` command needs. There
is no build file, and the files are placed in the workshop session exactly as
they appear here, with their permissions preserved. A package which has to be
built for each platform can instead be built with a ``Dockerfile``, see
[choosing how to build a package image](choosing-how-to-build-a-package-image).

Only regular files and directories can be published. A symbolic link, a hard
link, a device or a socket in the package source fails the publish and is
named in the error, rather than being silently dropped into an image which
behaves differently from the source. Where a tool would normally be installed
as a symbolic link, ship the file itself, or create the link from a ``setup.d``
script at a writable path. Anything beside the manifest and the reserved
directories is ignored rather than published, and is listed when publishing so
that a file in the wrong place is noticed.

The package manifest
--------------------

Every package source carries a manifest named ``package.yaml`` at its root:

```yaml
apiVersion: packages.educates.dev/v1alpha1
kind: ExtensionPackage
name: argocd
version: 2.10.6
description: The Argo CD command line client.
```

The ``name`` and ``version`` are required, and ``description`` is optional.
The ``version`` describes the package and is used as the image tag when no tag
is given at publish time.

Give the package a name which is a valid DNS label. Publishing does not check
this, but a workshop declaring the package as an image requires one, because
the name is used for the directory the package is delivered to.

The manifest is copied to the root of the published image, where it serves a
second purpose. When a workshop session starts, each package declared as an
image is checked for its manifest, and a package which did not arrive is
reported in the session's error logs. See [accessing workshop error
logs](accessing-workshop-error-logs). An image which is not an extension
package therefore fails visibly rather than leaving the session quietly
missing whatever the package provides.

(what-an-extension-package-can-provide)=
What a package can provide
--------------------------

A package is unpacked into ``/opt/packages/<name>`` in the workshop session,
where several directories in it are acted on by name.

Scripts in ``setup.d`` are run when the workshop session starts. They must be
named with a ``.sh`` suffix and must be executable, or they are skipped. Use
them for work which has to happen once per session, such as generating
configuration.

A setup script sets an environment variable an application needs by writing a
``NAME=VALUE`` line to the file named by the ``WORKSHOP_ENV`` environment
variable. The variable is then set in the setup scripts which run after it,
and in the processes run in the workshop session, including the terminal
shell:

```shell
#!/bin/bash

echo "ARGOCD_OPTS=--grpc-web" >> $WORKSHOP_ENV
```

See [running steps on container start](running-steps-on-container-start) for
more on setup scripts and ``WORKSHOP_ENV``.

A ``bin`` directory is added to the application search path defined by the
``PATH`` environment variable, so a program the package ships can be run by
name. Packages are set up one at a time, in the alphabetical order of their
names, and a package's ``bin`` directory is added just before its own setup
scripts run, after which it stays on the search path. A setup script can
therefore run programs from its own package and from the packages set up
before it, but never from a package which has not been set up yet. Each
``bin`` directory is added at the front of the search path, so where two
packages ship a program of the same name, the one from the package whose name
sorts last is found. The setup scripts supplied with the workshop content run
with every package's ``bin`` directory on the search path.

A package whose programs should not be on the search path ships them in a
directory with another name, such as ``libexec``.

The ``bin`` directory has to exist when the package's turn comes, which for a
package image means shipping it in the image. A package supplied as files
which only creates its ``bin`` directory from one of its setup scripts, such
as by downloading a program when the session starts, has nothing added for it.
It puts the directory on the search path itself, by writing a line setting
``PATH`` to the file named by ``WORKSHOP_ENV``. Escape ``$PATH`` in the line,
so that it is expanded when the line is read rather than replaced by the value
it has while the setup script runs:

```shell
#!/bin/bash

PACKAGE_DIR=$(cd "$(dirname "$0")/.."; pwd)

echo "PATH=$PACKAGE_DIR/bin:\$PATH" >> $WORKSHOP_ENV
```

A package may also supply ``supervisor`` configuration to run a background
process, and ``examiner/tests`` to add tests for the examiner. These are read
the same way as they are for a package supplied as files.

(extension-packages-are-read-only)=
Packages are read only
----------------------

Treat everything under ``/opt/packages/<name>`` as read only. Where the
cluster or the local Docker daemon supports it, the package is mounted into
the session from its image rather than being copied in, and writing to a mount
fails. A package which needs to write something must write it somewhere else,
such as under the home directory, from a ``setup.d`` script.

This also means a package must ship correct file permissions rather than
repairing them at startup. A package image preserves the permissions of the
files in the package source, including the execute bit, so this is a matter of
setting them correctly in the source. Packages supplied as files still need a
``setup.d`` script to restore execute permissions, because ``vendir`` does not
preserve them when unpacking an archive.

(choosing-how-to-build-a-package-image)=
Choosing how to build a package image
-------------------------------------

There are two ways to build a package image, and which suits a package depends
on where the files for each platform come from. Both produce the same image
layout, and a workshop declares the package the same way whichever was used.

Use the ``educates package publish`` command when the files for each platform
already exist before the build, such as a release binary downloaded for each
architecture, or when the package has no architecture specific files at all.
The command assembles every platform from one source directory, produces the
same digests when run twice on the same source, and needs no Docker daemon.

Use a ``Dockerfile`` built with ``docker buildx`` when the files have to be
built for each architecture, such as a program compiled from source. Building
for an architecture other than the one the machine runs on is easiest in a
container build, where each platform is built in its own build stage, without
setting up a toolchain for every architecture on the machine itself.

Publishing a package
--------------------

Publish a package source with the ``educates package publish`` command:

```text
educates package publish ./argocd --image-repository ghcr.io/myorg
```

The command publishes an image index with one child image per platform, so an
author writes one image reference whichever architecture a workshop session
runs on. It contacts the registry directly and needs no Docker daemon.

With no arguments the command publishes the package source in the current
directory to the local image registry at ``localhost:5001``, which is where a
package under development usually goes. The tag defaults to the ``version``
from the manifest, and ``--image-tag`` overrides it. Use ``--image`` to give
the full reference instead of a repository and a tag.

Use ``--dry-run`` to validate a package source and see what would be published
without contacting a registry:

```text
$ educates package publish ./argocd --dry-run
Dry run, nothing was published.
Package image: localhost:5001/argocd:2.10.6
Digest: sha256:0b3ba8bda2efccccbf0b85f9ef65584f5d17f0410d0c4072f3f2d4b896f50479
  linux/amd64 sha256:6fcdb09c1c9b2cbdf0b71855654f139c92b04fbc1a429f5a3a49d003446bc325
  linux/arm64 sha256:145a722252c3840e66bb04eeb56fd0d7208ba664992b1650ff17d7af7beed1f0
```

Two publishes of the same source produce the same digests, because file
timestamps are fixed rather than taken from the file system. Set
``SOURCE_DATE_EPOCH`` when a specific build timestamp is wanted instead.

Use ``--platform`` to publish for one platform rather than all of them, for
example ``--platform linux/arm64`` while developing on a machine of that
architecture. The command warns when it publishes a single platform, because
the resulting package cannot be used by a workshop session running on any
other architecture. Publish all platforms for a package others will use.

Publishing from a pipeline
--------------------------

The command is intended to be the whole of a publish step. A minimal pipeline
builds nothing and calls it once:

```text
educates package publish ./argocd \
  --image-repository ghcr.io/myorg \
  --image-tag ${GIT_COMMIT} \
  --digest-file digest.txt
```

Pass registry credentials with ``--registry-username`` and
``--registry-password``, or ``--registry-token``, or leave them out where the
environment already holds credentials for the registry. Use
``--registry-anon`` for a registry which needs none. A registry using a
private certificate authority is reached with ``--registry-ca-cert-path``, and
``--registry-insecure`` allows plain HTTP, which is intended for a local
registry rather than anything else.

``--digest-file`` writes the digest of the published image index to a file, so
a later step can refer to exactly what was published rather than to a tag
which may move.

Because a publish is reproducible, publishing the same source twice produces
the same digest, and a pipeline which republishes an unchanged package does
not produce a new image.

Building a package image with a Dockerfile
------------------------------------------

A package image is an ordinary OCI image, so it can be built with a
``Dockerfile``. A build stage produces the files for the platform being built,
and a final stage starting from ``scratch`` lays them out as the package:

```text
FROM fedora:44 AS build

RUN dnf install -y gcc make

COPY src/ /src/

RUN make -C /src && \
    install -D -m 0755 /src/mytool /package/bin/mytool

FROM scratch

LABEL dev.educates.extension-package="1"

COPY package.yaml /package.yaml
COPY common/ /
COPY --from=build /package/ /
```

The requirements are that the manifest is at the root of the image, and that
everything else is laid out as it should appear under
``/opt/packages/<name>``, so a program the package ships is at ``/bin`` in the
image. Here the package source holds ``package.yaml`` and ``common`` as it
would for the publish command, beside the ``src`` directory the program is
built from.

The build stage runs as the platform being built, so the compiler it installs
and the program it produces are for that platform. The program has to run in
the workshop session, so either build it on the same distribution as the
workshop base image, which is Fedora as in the example, or link it statically.

Build for both architectures and push the result as an image index:

```text
docker buildx build --platform linux/amd64,linux/arm64 \
  --tag ghcr.io/myorg/mytool:1.0.0 --push .
```

Building for the architecture the machine does not itself run uses emulation,
which Docker Desktop provides, and can be slow. Where the toolchain supports
it, a build stage can cross compile instead, by starting it with ``FROM
--platform=$BUILDPLATFORM`` and choosing what to build from the
``TARGETARCH`` build argument. An image built for one architecture only cannot
be used by a workshop session running on the other.

A package which needs no build stage can reproduce the overlay the publish
command does, taking the platform directory for the architecture being built,
so that one package source works either way:

```text
FROM scratch

ARG TARGETARCH

LABEL dev.educates.extension-package="1"

COPY package.yaml /package.yaml
COPY common/ /
COPY linux-${TARGETARCH}/ /
```

The label records that the image was built to this contract, and its value,
``1``, is the version of the contract rather than a flag. Nothing requires
the label: a workshop session tests for the manifest, not the label, which is
what makes building with a ``Dockerfile`` possible at all.

A ``Dockerfile`` build takes file timestamps from the build, so building the
same source twice gives different digests. For a reproducible build, set
``SOURCE_DATE_EPOCH`` in the environment, which ``docker buildx`` passes to the
build, and add ``rewrite-timestamp=true`` to the image output so the files in
the image take that timestamp too. Also turn off the provenance attestation
which ``docker buildx`` adds to the image index by default, because it records
details of each individual build and so changes the digest of the index even
when the platform images are identical:

```text
SOURCE_DATE_EPOCH=$(git log -1 --pretty=%ct) docker buildx build \
  --platform linux/amd64,linux/arm64 --provenance=false \
  --output type=image,name=ghcr.io/myorg/mytool:1.0.0,push=true,rewrite-timestamp=true .
```

The ``rewrite-timestamp`` option needs BuildKit 0.13 or newer.

Using a package in a workshop
-----------------------------

A published package is declared in the workshop definition by name and image
reference. See [declaring an extension package as an
image](declaring-an-extension-package-as-an-image) for the full set of
properties, including how to use a package from a registry which requires
authentication.

Note that the platform decides how the package reaches a workshop session,
either by mounting the image read only or by fetching its contents, according
to what the cluster or the local Docker daemon supports. The workshop
definition is the same either way, and so is the package.
