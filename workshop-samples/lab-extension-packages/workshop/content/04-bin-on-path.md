---
title: "The bin Directory on the PATH"
---

A `bin` directory at the root of a package is added to the application search
path, so a program the package ships runs by name. The package does not need a
profile script to arrange this.

Run the packaged program:

```terminal:execute
command: |-
  greeter
```

Confirm it is the one from the package rather than something else of the same
name:

```terminal:execute
command: |-
  command -v greeter
```

It should resolve to `/opt/packages/greeter/bin/greeter`.

## The package did not set this up

The `greeter` package ships a profile script, but it contains no `PATH` line:

```terminal:execute
command: |-
  cat /opt/packages/greeter/profile.d/greeter.sh
```

The search path is arranged for every package, including packages supplied as
files, so an author does not repeat it.

## The right architecture was delivered

The package ships a different build of the program per architecture, under
`linux-amd64` and `linux-arm64` in the package source. One image reference
covers both, and the delivery picks the child matching the machine this
session runs on:

```terminal:execute
command: |-
  greeter --which; uname -m
```

The two should agree: `linux-amd64` on an `x86_64` machine, `linux-arm64` on
an `aarch64` one.

## The search path is set before setup scripts run

The package's setup script called `greeter` when the session started, before
any of these pages ran. Its output is in the setup log:

```terminal:execute
command: |-
  grep '^greeter:' ~/.local/share/workshop/setup-scripts.log
```

A setup script can therefore use the programs its own package ships.
