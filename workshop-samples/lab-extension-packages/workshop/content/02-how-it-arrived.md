---
title: "How the Package Arrived"
---

A package declared as an image reaches the session in one of two ways. It is
either mounted from its image read only, which needs support from the cluster
or the local Docker daemon, or its contents are fetched, which works anywhere.

The platform chooses. The workshop definition is the same either way, and so
is the package.

## Reading which delivery you got

The session reports what happened to each package in the download log, one
line per package:

```terminal:execute
command: |-
  grep '^Package ' ~/.local/share/workshop/download-workshop.log
```

You should see one line naming this package and how it arrived, either:

```text
Package greeter: image mount
```

or:

```text
Package greeter: package fetch
```

A package which did not arrive is named on a line of its own saying that its
manifest is missing, and the session shows an error dialog rather than
starting quietly broken.

## Telling from the file system

The delivery is also visible in how the directory is mounted. A mounted
package is a mount point in its own right:

```terminal:execute
command: |-
  findmnt --noheadings --target /opt/packages/greeter || echo "not a mount point, so the contents were fetched"
```

You do not normally need to know which you got. This page exists because this
is a test workshop, and because the next page shows one way the two differ.
