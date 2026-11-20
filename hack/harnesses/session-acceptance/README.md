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
6. when the fixture asks for it, presses the terminal copy and paste
   shortcuts under Windows, Linux and macOS, as
   [the terminal copy and paste step](#the-terminal-copy-and-paste-step)
   describes;
7. terminates the session.

At the end it deletes its test portal, which deletes the portal's
environments, and its fixture workshops, and waits for their namespaces to
go.

A fixture passes when its walk reached the last instruction page with every
action clicked and no terminal errors, for the SSH fixture, the tunnel
connected, and, for a fixture with the copy and paste step, every shortcut
behaved as the key behaviour table says under every platform. The kubectl
checks and the console are reported as observed, not judged: they describe
the build's current behaviour, which a change may set out to alter, so read
them against what the change intends.

## Requirements

- A running local cluster with Educates installed, the default one or a
  named one. [local-cluster.sh](../local-cluster.sh) creates a named one
  from a local build, served over HTTPS, or over plain HTTP with
  `--insecure`; see the [harnesses README](../README.md).
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
  running, or on its `nip.io` name, and over HTTPS its CA must be trusted,
  as `educates local secrets add ca` arranges. Chrome needs neither: the
  harness maps the cluster's names to its listen address in Chrome itself
  and accepts its certificate.

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

To reach a cluster served over plain HTTP, the harness's Chrome runs with
its `HttpsFirstBalancedModeAutoEnable` feature turned off. Chrome turns its
HTTPS-First balanced mode on by itself, and with it on, headless Chrome
refuses a page served over plain HTTP with `net::ERR_BLOCKED_BY_CLIENT`.
This makes the harness's Chrome looser than a user's, so a run over plain
HTTP doesn't show what a user's Chrome does with that cluster.

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
sessions at once, the quick profile takes about 18 minutes on a laptop, and
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
instructions come from, whose optional `ignoreTerminalLines` lists
regular expressions for lines the workshop prints as expected output that
the walk would otherwise report as errors, such as the `Error Log File`
line of the server `lab-k8s-fundamentals` deploys, and whose optional
`terminalCopyPaste`, when `true`, runs
[the terminal copy and paste step](#the-terminal-copy-and-paste-step) in
its session. The harness copies the sample, puts the fixture's
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
| `terminal-copy-paste` | The terminal actions sample under the default `admin` role, with the terminal copy and paste step. |

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

## The terminal copy and paste step

A fixture whose `fixture.json` sets `"terminalCopyPaste": true` has the
copy and paste shortcuts of its session's terminals pressed after the walk,
the console hold and the tunnel. The fixture runs in a Chrome of its own,
since the clipboard belongs to the browser and other fixtures' walks click
copy actions.

The step loads the training portal's page for the session, which shows the
dashboard in its iframe, once for each of Windows, Linux and macOS. Before
the page loads it sets the platform the browser reports through the
DevTools protocol, which reaches every frame of the page:
`navigator.platform` reads `Win32`, `Linux x86_64` or `MacIntel`, and the
user agent names the same operating system. It reads `navigator.platform`
back in the dashboard's frame, and runs nothing under a platform the frame
doesn't report.

In terminal 1 it then runs six cases. For each it types a command that
runs a small probe the step writes into the workshop container, as
`/tmp/session-acceptance-keys`, and presses the shortcut while the probe
runs:

| Case | What the probe does |
|---|---|
| Ctrl+C, text selected, then again | Shows a word, which the step double-clicks to select, and prints every byte the terminal delivers until three seconds pass without one, with the terminal raw so that ^C and ^V arrive as bytes. It then does so once more, for Ctrl+C pressed a second time with nothing else between. |
| Ctrl+C, nothing selected, sleep running | Sleeps ten seconds and prints whether an interrupt ended the sleep. |
| Ctrl+V | Prints the bytes delivered, with known text put on the clipboard first from the portal's own page. |
| Ctrl+Shift+C, text selected | Shows a word, which the step double-clicks to select, and prints the bytes delivered. |
| Ctrl+Shift+C, nothing selected | Prints the bytes delivered, with nothing selected. |
| Ctrl+Shift+V | As for Ctrl+V. |

It observes what a workshop user would: what the page's `copy` event put
on the clipboard, read from the event since the clipboard can't be read
over plain HTTP; whether the page's `paste` event carried the clipboard's
text; what reached the shell, whether the pasted text, ^C, ^V or nothing,
and for a second press of Ctrl+C, what reached the shell then; whether the
selection was kept or cleared; whether the sleep was interrupted; and
whether the page cancelled the key's default action. It judges each case
against the key behaviour table:

| Platform | Ctrl+C | Ctrl+V | Ctrl+Shift+C | Ctrl+Shift+V |
|---|---|---|---|---|
| Windows | Copies a selection and clears it, with nothing reaching the shell, so that a second press sends ^C; interrupts with none. | Pastes. | Cancelled; copies a selection and keeps it; does nothing with none. | Pastes. |
| Linux | ^C, on a second press too, which interrupts with no selection. | ^V. | As on Windows. | Pastes. |
| macOS | ^C, on a second press too, which interrupts with no selection. | ^V. | Left to the browser; copies nothing and sends nothing. | Sends nothing. |

The fixture passes only when all eighteen cases match.

Chrome's own copy and paste key bindings follow the operating system Chrome
runs on, not the platform it reports. Chrome on Windows and Linux runs its
copy, paste and paste-as-plain-text commands for Ctrl+C, Ctrl+V and
Ctrl+Shift+V when the page leaves the key alone; Chrome on macOS runs none
of them. Before each platform the step presses those keys in a textarea of
a page of its own, reporting that platform, and watches for the `copy` or
`paste` event. Where this host's Chrome runs the command itself, the step
presses the key as it is, and the report says the browser's own action ran
natively. Where it doesn't, as on a macOS host, the step sends the key with
the command attached through the DevTools protocol, which Chrome runs only
when the page leaves the key's default action alone, as it does its own
binding, and the report says the action was simulated. On Windows and Linux
Ctrl+Shift+C opens Chrome's inspector, a browser action no page can observe
and the step doesn't run, so the step judges it by whether the page
cancelled the key, and the report says the inspector was not run.

The step's full pass is established on a macOS host, whose Chrome binds
none of these keys. On another host, such as Linux, the host Chrome's own
bindings act under every platform the step fakes. Under macOS, Ctrl+Shift+V
then pastes as plain text, which fails its case, and the report names the
host's command as the browser's own action. With `--headful`, macOS's
Ctrl+Shift+C, which the page leaves to the browser, opens the host Chrome's
real inspector.

## The report

The results directory holds:

- `report.md`: the run's cluster, CLI, profile and robot credentials
  source, a summary table, and for each fixture its walk, each kubectl
  check with the first lines of its output, the console's probes, process
  and settings, the tunnel, the copy and paste cases, and any problems;
- `report.json`: the same, as data;
- one directory per fixture with `result.json`, `log.jsonl`, the event log
  of the session, and screenshots of each instruction page and console
  probe, and of the terminal after each platform's copy and paste cases.

For a fixture with the copy and paste step, the summary table counts the
cases that matched the key behaviour table, and a second table shows each
case per fixture and platform: whether it matched, what was observed of
what the table judges, and, in brackets, the browser's own action and
whether it ran natively or was simulated. The fixture's section shows, per
platform, the platform its dashboard's frame reported, what this host's
Chrome does for each key unaided, and each case with what the table says,
what was observed, whether they match and the browser's own action.

The summary table is also printed at the end of the run. Results are
gitignored.
