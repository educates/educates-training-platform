---
title: Workshop Summary
---

If every page behaved as described, an extension package declared as an image
is working correctly in this environment.

What was checked:

- The package was delivered to `/opt/packages/greeter` with its manifest, and
  the files carry the permissions they were published with.
- The session reported how the package arrived, in `download-workshop.log`.
- The package hooks ran: a setup script once at startup, and a profile script
  in every shell.
- The package directory is read only where the package was mounted, and the
  package writes what it generates somewhere else.
- The package's `bin` directory is on the search path, before setup scripts
  run, and the build matching this machine's architecture was delivered.

None of the pages needed to know which delivery the platform chose. A package
author writes one package and one workshop definition, and the platform
decides how the package reaches the session based on what the cluster or the
local Docker daemon supports.

## Publishing the package

The package used here is built from the `packages/greeter` directory of this
sample. To rebuild and republish it:

```text
educates package publish packages/greeter
```

Add `--dry-run` to validate the package source and see the digests which would
be published without contacting a registry.

See "Creating Extension Packages" in the Educates documentation for the full
description of the package source layout, the manifest and publishing.
