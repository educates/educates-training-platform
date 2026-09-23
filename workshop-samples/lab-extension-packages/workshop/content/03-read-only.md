---
title: "The Package Is Read Only"
---

Treat everything under `/opt/packages/<name>` as read only. Where the package
is mounted from its image, the mount is read only and writing to it fails.

Try it:

```terminal:execute
command: |-
  touch /opt/packages/greeter/scratch-file
```

Where the package was mounted, this fails with a read-only file system error.
Where the contents were fetched instead, the write succeeds, because a fetched
package is ordinary files on disk.

That difference is exactly why a package must not rely on writing into itself.
A package which works only under a fetch would break the first time it landed
on a cluster which mounts, and the author would have no reason to expect it.

Clean up if the write did succeed:

```terminal:execute
command: |-
  rm -f /opt/packages/greeter/scratch-file
```

## What a package does instead

The `greeter` package generates a configuration file when the session starts,
which is the kind of thing a package might be tempted to write into its own
directory. Its setup script writes it under the home directory instead:

```terminal:execute
command: |-
  cat /opt/packages/greeter/setup.d/01-greeter.sh
```

The profile script then points at that location through an environment
variable, so the program finds it wherever it was written:

```terminal:execute
command: |-
  echo "GREETER_CONFIG=$GREETER_CONFIG"
```

## Permissions are shipped, not repaired

A package supplied as files often needs a setup script to restore execute
permissions, because the download does not preserve them. A package image
does preserve them, and a mounted package could not repair them anyway. Ship
the right permissions in the package source instead.
