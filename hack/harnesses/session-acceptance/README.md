# Session acceptance harness

This harness drives real workshop sessions on a local cluster the way an
attendee would, and reports what it saw. For each fixture workshop it:

1. requests a session from a test training portal of its own, as the
   portal's robot client, and activates it at once in Chrome;
2. runs `kubectl get ns <session namespace>`, `kubectl get ns` and `kubectl
   get pods` inside the workshop container, and reports each as allowed,
   forbidden, not found or an error, along with the namespace of the
   container's kube context;
3. walks every instruction page and clicks every action, skipping
   `dashboard:open-url`, which opens a browser window that stalls a
   headless page, and reports every line a terminal shows that looks like
   an error;
4. when the workshop has the console, opens the Console tab and holds it
   open, probing it periodically from inside its frame, and reports whether
   it answered, how often its dashboard process started, whether it
   panicked, the namespace it was started on and its selector shows, and
   what its namespace list and settings returned;
5. when the workshop has SSH access over the tunnel, connects to the session
   from this host with `ssh`, using `educates tunnel connect` from the CLI
   under test as the proxy command;
6. terminates the session.

At the end it deletes its test portal, which deletes the portal's
environments, and its fixture workshops, and waits for their namespaces to
go.

A fixture passes when its walk reached the last instruction page with every
action clicked and no terminal errors, and, for the SSH fixture, the tunnel
connected. The kubectl checks and the console are reported as observed, not
judged: they describe the build's current behaviour, which a change may set
out to alter, so read them against what the change intends.

## Requirements

- A running local cluster with Educates installed, the default one or a
  named one. [local-cluster.sh](../local-cluster.sh) creates a named one
  from a local build; see the [harnesses README](../README.md).
- The `educates` CLI of the build under test. It defaults to this checkout's
  `client-programs/bin/educates-<os>-<arch>`, which `make local-build`
  writes. The `educates` on your `PATH` may be another build.
- `kubectl` and `ssh` on the `PATH`, and Google Chrome. Chrome is found at
  its usual place on macOS, `/usr/bin/google-chrome` elsewhere, or where
  `--chrome` or `CHROME_PATH` say.
- Node 22.12 or later. `package.json` pins the version for Volta, and
  `run.sh` installs the dependencies from `package-lock.json` on first use.
- For the SSH fixture, the cluster's names must resolve on this host, as
  they do for a cluster under `educates.test` with the local DNS resolver
  running, and its CA must be trusted, as `educates local secrets add ca`
  arranges. Chrome needs neither: the harness maps the cluster's names to
  its listen address in Chrome itself and accepts its certificate.

## Running it

```
hack/harnesses/session-acceptance/run.sh --local-cluster <name> [--cli <path>] [--profile quick|full]
```

The cluster is selected as the CLI selects it: `--local-cluster <name>`,
else `EDUCATES_LOCAL_CLUSTER`, else the default local cluster. The harness
reaches the cluster only through its own kube context, `kind-educates` or
`kind-educates-<name>`, whatever the current context is, and runs every
`educates` command with the cluster selected. It reads the cluster's listen
address from `educates local cluster list`, and its ingress domain and
protocol from the cluster's `EducatesClusterConfig` status, so it works the
same over HTTPS and plain HTTP.

It never prompts. It prints its progress, writes the report, prints the
summary table and exits 0 when every fixture passed, 1 when one failed, and
2 when it could not start, as for an unknown option or a cluster that is not
running.

### Profiles

| Profile | Console hold | Probes | Use |
|---|---|---|---|
| `quick` (default) | 1 minute | every 30 seconds | A kind check after each change or fix |
| `full` | 15 minutes | every minute | An acceptance run, where a console that hangs after a few minutes shows |

Both profiles walk every fixture in full. With the default of three
sessions at once, the quick profile takes about 15 minutes on a laptop, and
the full one about 45. To repeat only what a fix touches, name the fixtures
with `--fixtures`, and shorten or lengthen the hold with `--hold`.

### Options

| Option | Meaning |
|---|---|
| `--local-cluster <name>` | The named local cluster to drive; `--local-cluster=` selects the default one. |
| `--cli <path>` | The `educates` CLI of the build under test. |
| `--profile quick\|full` | The console hold of the run. |
| `--fixtures <a,b,...>` | Run only these fixtures; `--list-fixtures` names them. |
| `--hold <duration>` | Hold each console this long instead, as `90s` or `5m`. |
| `--parallel <n>` | Sessions driven at once, 3 by default. |
| `--out <dir>` | Where the report goes, by default `results/<time>-<cluster>` here. |
| `--portal <name>` | The test portal's name, `session-acceptance` by default. |
| `--keep` | Leave the test portal and its workshops in place for inspection. |
| `--chrome <path>` | The Chrome to drive. |
| `--headful` | Show Chrome's windows instead of running it headless. |

### Examples

```
# The quick profile on the named cluster rbac-02, with the CLI its build produced:
hack/harnesses/session-acceptance/run.sh --local-cluster rbac-02 --cli client-programs/bin/educates-darwin-arm64

# The full profile, four sessions at once, the report in a directory of your choice:
hack/harnesses/session-acceptance/run.sh --local-cluster rbac-sessions --profile full --parallel 4 --out /path/to/results

# Only the fixtures under narrow roles, holding their consoles for 5 minutes:
hack/harnesses/session-acceptance/run.sh --local-cluster rbac-14 --fixtures terminal-view,terminal-custom-strict --hold 5m
```

## The test portal

The harness creates a TrainingPortal of its own, `session-acceptance`, with
anonymous registration, room for 20 sessions, and no reserved or initial
sessions. A portal left by an interrupted run is deleted first. Each
fixture is deployed to it with `educates cluster workshop deploy`, as a
Workshop named `session-acceptance-<fixture>`, with `orphaned` and `expires`
at 120 minutes: the harness holds sessions outside the portal's page, which
is what reports their activity, so a shorter `orphaned` time lets the
portal delete a session in the middle of a console hold.

Sessions are requested through the portal's REST API as its robot client.
The robot credentials come from the Secret the TrainingPortal's status
names under `status.educates.credentialSecrets.robot`, in the portal's
namespace, with the keys `username`, `password`, `client-id` and
`client-secret`. The report names the Secret.

Session requests are spaced 10 seconds apart, and one that fails with a
server error is retried twice, with a note in the report: the portal has
failed one of several requests made in the same second with "database is
locked".

## Fixtures

Each fixture is a directory under `fixtures/` holding the Workshop
definition, `workshop.yaml`, and `fixture.json`, whose `summary` describes
it, whose `sample` names the directory of `workshop-samples/` its
instructions come from, and whose optional `ignoreTerminalLines` lists
regular expressions for lines the workshop prints as expected output that
the walk would otherwise report as errors, such as the `Error Log File`
line of the server `lab-k8s-fundamentals` deploys. The harness copies the sample, puts the fixture's
definition in place of the sample's, publishes the workshop files to the
local registry with `educates workshop publish`, tagged with the cluster's
name, and deploys it. A fixture without a `sample` uses files already
published, as `lab-k8s-fundamentals` does.

| Fixture | Session |
|---|---|
| `lab-k8s-fundamentals` | `lab-k8s-fundamentals` 9.0 from its published files image: kubectl under the default `admin` role, with the console. |
| `terminal-view` | The terminal actions sample under `role: view`, with the console. |
| `terminal-custom-edit` | The terminal actions sample under `role: custom`, binding the built-in `edit` ClusterRole in the session namespace, with the console. |
| `terminal-custom-strict` | The terminal actions sample under `role: custom`, with a Role in the session namespace on `pods`, `pods/log`, `services` and `deployments` and nothing on namespaces, with the console. |
| `dashboard-console` | The dashboard actions sample, with the console. |
| `terminal-vcluster` | The terminal actions sample with a virtual cluster, where kubectl and the console use the virtual cluster. |
| `ssh-tunnel` | The SSH remote access sample, which connects back over the tunnel from inside the session, and from this host with the CLI under test. |

Every sample turns the session token off, and each fixture built from one
turns it back on, so its session has a service account token, as most
workshops do. session-manager refuses a Role in `session.objects` that
grants more than it holds itself, which is why `terminal-custom-strict`
lists its verbs and gives each API group a rule of its own.

To add a fixture, add a directory with its `workshop.yaml` and
`fixture.json`; the harness picks it up by its directory name. A fixture
that publishes files names its image after itself under
`$(image_repository)` and tags it `$(workshop_version)`, as the existing
ones do.

## The report

The results directory holds:

- `report.md`: the run's cluster, CLI, profile and robot credentials
  source, a summary table, and for each fixture its walk, each kubectl
  check with the first lines of its output, the console's probes, process
  and settings, the tunnel, and any problems;
- `report.json`: the same, as data;
- one directory per fixture with `result.json`, `log.jsonl`, the event log
  of the session, and screenshots of each instruction page and console
  probe.

The summary table is also printed at the end of the run. Results are
gitignored.
