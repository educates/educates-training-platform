# Harnesses

The harnesses in this directory check Educates by hand on a local kind
cluster, driving it the way a user would. They cover what unit tests and
render checks don't: session-manager's handlers, the training portal,
lookup-service and tunnel-manager have almost no tests, and checks that only
rendered resources or ran fixtures have missed real defects. They are not
run in CI.

| Harness | What it checks |
|---|---|
| [session-acceptance](session-acceptance/README.md) | Real workshop sessions, requested from a test portal and walked in Chrome: instruction actions, kubectl inside the workshop container, the console, the SSH tunnel. |

## Running a harness on a cluster of its own

A harness runs against a local cluster: the default one or a named one,
selected with `--local-cluster <name>` or `EDUCATES_LOCAL_CLUSTER`, as the
`educates` CLI selects it. To check a build without touching the clusters
you already run, build it, give it a named cluster of its own with
`local-cluster.sh`, run the harness against that cluster, and delete the
cluster when you are done:

```
make local-build PACKAGE_VERSION=mytag
hack/harnesses/local-cluster.sh create mycheck --listen-address 10.10.10.2 --tag mytag
hack/harnesses/session-acceptance/run.sh --local-cluster mycheck
hack/harnesses/local-cluster.sh delete mycheck
```

Every command runs to completion without prompting, so an agent or a
script can drive the whole sequence.

### The cluster helper

`local-cluster.sh` brings up a named local cluster from a local build and
removes it again:

```
local-cluster.sh create <name> --listen-address <ip> --tag <tag> [--domain <domain>] [--cli <path>]
local-cluster.sh delete <name> [--cli <path>]
local-cluster.sh show <name>
```

`create` takes the cluster's name, the `PACKAGE_VERSION` the build was made
with, and the CLI that build produced, which defaults to this checkout's
`client-programs/bin/educates-<os>-<arch>`. Before it changes anything it
checks that the CLI reports that version, that the local registry holds the
build's images under that tag, that no local cluster uses the listen
address, and that the cluster has no config yet. It then writes the
cluster's config with `educates local config init`, records in the cluster's
folder of the CLI data home that it created the cluster, and runs `educates
local cluster create`. A CLI built by `make local-build` deploys the images
of its own build, so the cluster runs exactly that build, and the shared
`:latest` images stay as they are.

- The listen address must be an address of the host that no other local
  cluster uses, such as an alias added to a network interface, as
  "Running several local clusters" in the local environment guide
  describes. A cluster publishes its ports 80 and 443 there only, never on
  loopback.
- The domain defaults to `<name>.educates.test`. A domain under
  `educates.test` is served over HTTPS with a certificate the CLI issues
  from the CA root in its secrets cache, and resolves on the host through
  the local DNS resolver and `/etc/resolver/educates.test`, as for the
  default cluster.
- Kind makes the cluster it creates the current kube context; `create` puts
  back the current context there was before, or none, so other terminals
  keep their cluster.
  Reach the new cluster through its own context, `kind-educates-<name>`.
- When `create` fails before the kind cluster exists, it removes the config
  it wrote. When it fails later, it leaves the cluster for inspection and
  says how to delete it.

`delete` runs `educates local cluster delete` and `educates local config
delete` for the named cluster, which leaves every other local cluster and
the shared registry, registry mirrors and DNS resolver as they were, and
refreshes the resolver so it stops serving the cluster's domain. It refuses
a cluster `create` did not create, so it never removes a cluster someone
made by hand. The CA key pair the CLI issued for the cluster's domain stays
in the CLI's secrets cache, where a later cluster on the same domain reuses
it.

`show` prints what `create` recorded: the build tag, the CLI, the listen
address, the domain and the kube context.

## Adding a harness

Give the harness a subdirectory of its own here, holding everything it
needs: its code, dependencies, fixtures and a README saying what it checks,
what it needs on the host, and how to run it without prompts. Add a row to
the table above. A harness drives a local cluster selected the way the CLI
selects one, so it works with `local-cluster.sh` and needs no change to
another harness.

A Node harness pins its Node version for Volta in its `package.json`, with
`volta pin node@<version>`, and pins its dependencies exactly with a
committed `package-lock.json`. Its results go in a gitignored directory of
its own, never in a tracked path.
