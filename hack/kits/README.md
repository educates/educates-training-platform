# Kits

A validation kit leaves a known set of states, healthy and failing, in an
existing cluster, for a person to inspect by hand and by eye: the columns
`kubectl get` prints, the status, the Events. It checks that the cluster
shows each state as the kit describes it, breaks and fixes states on
request, and removes everything it made when asked. Run one when a
component's implementation changes, such as when it moves to Go, to get an
environment to validate it in.

A kit differs from a [harness](../harnesses/README.md): a harness drives a
cluster, judges what it sees and cleans up after itself, while a kit leaves
its states in place for as long as you want to look at them.

| Kit | What it shows |
|---|---|
| [secrets-manager](secrets-manager/README.md) | The Go secrets-manager's SecretCopier, SecretExporter, SecretImporter and SecretInjector in sync, in each failing state, recovered, and refused at admission. |

## Running a kit

`kit.sh` needs Bash, 3.2 or later as macOS ships it, and kubectl, and for a
local cluster the `educates` CLI to read its list of local clusters.
Nothing else is installed. Every command runs to completion without
prompting, so an agent or a script can drive it.

```
hack/kits/kit.sh list                                         # the kits
hack/kits/kit.sh list secrets-manager                         # a kit's scenarios
hack/kits/kit.sh apply secrets-manager --local-cluster mycheck
hack/kits/kit.sh verify secrets-manager --local-cluster mycheck
hack/kits/kit.sh status secrets-manager --local-cluster mycheck
hack/kits/kit.sh events secrets-manager --local-cluster mycheck
hack/kits/kit.sh break secrets-manager immutable --local-cluster mycheck
hack/kits/kit.sh fix secrets-manager immutable --local-cluster mycheck
hack/kits/kit.sh delete secrets-manager --local-cluster mycheck
```

A scenario is named with or without its number, as `07-immutable` or
`immutable`. `kit.sh --help` lists the commands and options.

### Choosing the cluster

A kit acts on a local cluster, the default one or a named one, selected
with `--local-cluster <name>` or `EDUCATES_LOCAL_CLUSTER`, as the
`educates` CLI selects it; an empty name selects the default cluster. The
cluster must be running, as `educates local cluster list` shows it, and
kubectl reaches it through its own kube context in `~/.kube/config`, where
the CLI writes every local cluster's context whatever `KUBECONFIG` says.
`--cli` names the CLI to read the list with, by default `educates` on
`PATH`.

For any other cluster, name its kube context with `--context`, read from
`KUBECONFIG`. A kit never acts on the current context unless it is named
this way.

To validate a build without touching the clusters you already run, give it
a named local cluster of its own with the harnesses' cluster helper:

```
make local-build PACKAGE_VERSION=mytag
hack/harnesses/local-cluster.sh create mycheck --listen-address 10.10.10.2 --tag mytag
hack/kits/kit.sh apply secrets-manager --local-cluster mycheck
hack/kits/kit.sh verify secrets-manager --local-cluster mycheck
hack/harnesses/local-cluster.sh delete mycheck
```

### What each command does

- `apply` checks the kit's `requires` and applies nothing when the cluster
  does not meet them. It then applies the kit's `base.yaml` and the
  `apply.yaml` of each scenario named, or of every scenario, adding the
  labels `educates.dev/kit: <kit>` and `educates.dev/kit-scenario:
  <scenario>` to every resource through kubectl's built-in kustomize, so
  that the manifests stay plain. It records each scenario as `applied`. A
  scenario recorded as broken or fixed is not applied again: delete the kit
  and apply it again to start over.
- `verify` checks each applied scenario against the section of its
  `expect` file for the state it is recorded in. It checks again every 2
  seconds until every check holds or `--timeout` passes, 120 seconds by
  default, and prints each check with what it found when it does not hold.
  A check that held is checked again in a last pass over all of them once
  the others hold, so what `verify` reports held at one time. It exits 0
  when every check holds and 1 when one does not.
- `status` prints each scenario's state and whether it can be broken and
  fixed, and the kit's resources of the kinds its `kit.conf` names.
- `events` prints the Events about the kit's resources, oldest first. They
  are matched by the UID of each resource, so the Events of another kit, or
  of an earlier resource of the same name, never show. The Events of a
  cluster-scoped resource are recorded in the `default` namespace.
- `break` runs a scenario's `break.sh`, once, after `apply`. It first waits
  for the scenario's applied state, then runs the script, records the
  scenario as `broken`, and verifies its broken state.
- `fix` runs a scenario's `fix.sh`, once, from its broken state when it has
  a `break.sh`, and from its applied state otherwise. It first waits for
  that state, so the Events that come only with time, such as a Warning
  after a grace period, have come, and those that follow them on recovery
  come too. It then runs the script, records the scenario as `fixed`, and
  verifies its fixed state.
- `delete` deletes the kit's labelled resources of the kinds its `kit.conf`
  names, in every namespace, then its labelled namespaces, and with them
  everything in them, then its record. It leaves everything else as it was.

The record of each scenario's state is the ConfigMap `educates-kit-<kit>`
in the `default` namespace, with a key for each scenario applied, so any
terminal sees the same state. `kubectl get configmaps -n default -l
educates.dev/kit` shows the kits a cluster holds.

## Adding a kit

Give the kit a directory of its own here, named for the component it
shows, and add a row to the table above:

```
<kit>/
├── README.md        its scenarios, what each shows, what the kit needs
├── kit.conf         its description, kinds and path aliases
├── requires         checks the cluster must pass before apply (optional)
├── base.yaml        resources every scenario shares, such as namespaces (optional)
└── scenarios/NN-<name>/
    ├── expect       the state each resource reaches, for each state of the scenario
    ├── apply.yaml   the scenario's resources (optional: a scenario of rejections has none)
    ├── break.sh     breaks a healthy scenario (optional)
    └── fix.sh       recovers the scenario (optional)
```

`kit.conf` is sourced by `kit.sh`:

- `KIT_DESCRIPTION` is the line `kit.sh list` prints for the kit.
- `KIT_KINDS` lists the kinds `status` shows, `events` reports on and
  `delete` removes, by their full resource names. List the cluster-scoped
  kinds first, so that kubectl prints no empty namespace column for them.
- `KIT_PATH_<alias>` names a path an `expect` line may give by its alias,
  as `KIT_PATH_insync='{.status.summary.inSyncRatio}'`.

Name what a kit creates so that it can't clash with what another kit, or
anyone else, creates in the same cluster: `kit-<kit>-...` for its
namespaces and cluster-scoped resources, and short names inside its own
namespaces. Select its namespaces with a label only they carry, never one
another kit's namespaces could carry too.

### The expect file

An `expect` file holds a section for each state of the scenario,
`[applied]`, `[broken]` and `[fixed]`, each holding the checks that hold
in that state. Lines starting with `#` are comments, and the first comment
is the summary `kit.sh list` prints. A `requires` file holds checks of the
same form, outside any section. Each check is one line:

| Line | Holds when |
|---|---|
| `<resource> <path> = <value>` | the path reads the value; `!=` holds when it does not, and `~` when it holds the value |
| `<resource> absent` | the resource does not exist |
| `event <resource> <type> <reason>` | the resource has an Event of the type and reason |
| `event <resource> <type> <reason> = <count>` | it has exactly that many; an Event that stands for a series counts once for each occurrence |
| `reject <manifest> ~ <text>` | a server-side dry run of the manifest, in the scenario's directory, is refused with the text in its error, so nothing is created either way |

- A resource is written `<kind>/<name>` when it is cluster-scoped, and
  `<namespace>/<kind>/<name>` when it is not.
- A path is a kubectl JSONPath template, as
  `{.status.boundTo.name}`, written without spaces, or a Go template, as
  `{{index .data "token" | base64decode}}`, which may hold spaces and
  reads a Secret's data decoded, or an alias. `ready`, `reason` and
  `message` read the `Ready` condition's status, reason and message, and a
  kit's `kit.conf` adds its own.
- The value is the rest of the line, spaces included.

```
# A copy another writer made immutable can't take its source's change.
[applied]
secretcopier/kit-secrets-manager-immutable  reason  = InSync
kit-secrets-manager-target-b/secret/immutable  {{index .data "value" | base64decode}}  = v1

[broken]
secretcopier/kit-secrets-manager-immutable  reason   = TargetErrors
secretcopier/kit-secrets-manager-immutable  message  ~ the copy is immutable and was not updated
event secretcopier/kit-secrets-manager-immutable  Warning CopyNotUpdated  = 1
```

### break.sh and fix.sh

`kit.sh` sources a scenario's `break.sh` or `fix.sh` in the scenario's
directory, in a subshell with `set -euo pipefail`, where `kubectl` reaches
the selected cluster. Write them as plain kubectl commands. Create
resources with `kit_apply`, which applies the manifests named, or the
manifest on its standard input, with the kit's and the scenario's labels,
so that `status` shows them and `delete` removes them. A command that fails
stops `kit.sh` and leaves the scenario's recorded state as it was.

`kit.sh` and `lib.sh` run under Bash 3.2, as macOS ships it: they use no
associative arrays and never expand an array that may be empty. Check a
change to them, and a kit's scripts, with `/bin/bash` on macOS as well as a
current Bash.
