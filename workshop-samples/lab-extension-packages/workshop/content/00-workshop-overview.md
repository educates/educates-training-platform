---
title: Workshop Overview
---

This workshop is a test suite for an extension package declared as an image.
It is not intended as an end-user workshop but rather as a functional test to
confirm that a package image is delivered correctly and behaves as the package
image contract says it should.

The workshop declares one package, `greeter`, whose source is in the
`packages/greeter` directory of this sample. It is published separately with
`educates package publish` before the workshop is deployed, and the workshop
refers to it by image reference:

```yaml
spec:
  workshop:
    packages:
    - name: greeter
      image: "$(image_repository)/greeter:1.0.0"
```

The `greeter` package is deliberately small. It ships:

- A package manifest, `package.yaml`, which names and versions it.
- A `setup.d` script, run once when the session starts.
- A `profile.d` script, sourced into every shell.
- A `bin/greeter` program, built differently for each architecture so that the
  pages can show which one was delivered.

The following pages check each part:

- **What the package delivered** — the package is on disk at a known path with
  its manifest, and the files carry the permissions they were published with.
- **How the package arrived** — the platform chooses between mounting the
  image and fetching its contents, and the session log says which happened.
- **The package is read only** — writing into a mounted package fails, which is
  why a package writes elsewhere.
- **The bin directory is on the PATH** — the packaged program runs by name
  without the package setting up the PATH itself.

Nothing on these pages depends on which delivery the platform chose. That is
the point: the workshop definition is the same either way.
