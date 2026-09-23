---
title: "What the Package Delivered"
---

A package is delivered into a directory named after the package, under
`/opt/packages`. The name in the workshop definition is what names the
directory, which is why it has to be a valid DNS label.

List what arrived:

```terminal:execute
command: |-
  ls -la /opt/packages/greeter
```

You should see the package manifest beside the three directories the package
ships: `bin`, `profile.d` and `setup.d`.

## The package manifest

Every package image carries a manifest at its root. It names and versions the
package, and the workshop session tests for it to confirm the package actually
arrived:

```terminal:execute
command: |-
  cat /opt/packages/greeter/package.yaml
```

An image which is not an extension package unpacks perfectly well but leaves
no manifest, which is how a wrong image reference is caught rather than
leaving the session quietly missing what the package provides.

## Permissions survive publishing

The package source has the execute bit set on its setup script and on its
program, and a package image preserves the permissions it was published with:

```terminal:execute
command: |-
  ls -l /opt/packages/greeter/setup.d/01-greeter.sh /opt/packages/greeter/bin/greeter
```

Both should be executable. This matters because a package which may be mounted
read only cannot repair its own permissions at startup.

## The setup script ran

The setup script runs once when the session starts. It wrote a configuration
file into the home directory:

```terminal:execute
command: |-
  cat ~/.local/share/greeter/config
```

It wrote there rather than into the package, for the reason the read only page
covers.
