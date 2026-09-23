Extension Package Images Test
=============================

Test workshop for verifying an extension package declared as an image, covering
the delivery it arrives by, the package hooks, the read only rule and the `bin`
directory on the `PATH`.

Unlike the other samples, this one ships the package it uses. The package
source is in `packages/greeter`, and it has to be published before the
workshop is deployed, because the workshop refers to it by image reference.

Publishing the package
----------------------

From the root of this workshop directory, publish the package to the same
image registry the workshop is published to:

```
educates package publish packages/greeter
```

With no `--image-repository` this publishes to the local registry at
`localhost:5001`, which is where `educates publish-workshop` also publishes by
default. Add `--dry-run` first to validate the package source and see the
digests which would be published without contacting a registry.

Deploying the workshop
----------------------

Then publish and deploy the workshop as for any other sample:

```
educates publish-workshop
educates deploy-workshop
```

The workshop can also be run locally against Docker, which delivers the package
the same way:

```
educates docker workshop deploy
```

The package
-----------

`greeter` is deliberately minimal. It ships a manifest, a `setup.d` script, a
`profile.d` script and a `bin/greeter` program built differently for each
architecture so the workshop can show which build was delivered.

```
packages/greeter/
  package.yaml
  common/
    setup.d/01-greeter.sh
    profile.d/greeter.sh
  linux-amd64/bin/greeter
  linux-arm64/bin/greeter
```

Files in `common` are shared by every architecture, and each platform directory
overlays it, which is how one package ships a different binary per
architecture under a single image reference.
