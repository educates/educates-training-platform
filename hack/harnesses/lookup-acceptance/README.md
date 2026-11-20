# Lookup acceptance harness

This harness checks, on real training portals on two local clusters, that
the Lookup Service spreads new sessions across the clusters and gives a
returning user their session back. It creates two named local clusters
from a local build, each running a training portal that offers the same
workshop at the same capacity, registers both clusters with the Lookup
Service on the first, requests sessions through it as a tenant API client
does, and deletes the clusters at the end.

## What it checks

With the same workshop at the same capacity on both clusters, each session
request counts the sessions the requests before it placed, so every pair of
requests, the first and second, the third and fourth, and so on, places one
session on each cluster. Which cluster of a pair comes first is random,
since the Lookup Service orders equally ranked candidates at random, so
"alternating" means one session of each pair on each cluster, not a fixed
order.

A portal names a session after its environment and a suffix it derives
from the environment's creation time and the session's number. The run
deploys both portals at once, so their environments are usually created in
the same second and the two clusters hold sessions of the same names, such
as `lookup-acceptance-w01-fv7t` on each. The checks tell sessions apart by
cluster and name, and so must the Lookup Service.

A run:

1. creates the clusters with [local-cluster.sh](../local-cluster.sh), one
   after the other, the lookup cluster first and then the remote cluster;
2. deploys on each a training portal, `lookup-acceptance`, offering the
   workshop `lookup-acceptance`, which has no content, with room for one
   session more than half the sessions the run requests, in the portal and
   in its environment, and no reserved sessions, so the ordering rather
   than a full portal decides every placement;
3. registers both clusters with the Lookup Service on the lookup cluster:
   the lookup cluster through the service's own remote access token, and the
   remote cluster through a kubeconfig Secret made with `educates admin
   lookup kubeconfig`, whose server address it rewrites from the host's
   loopback port to `https://educates-<remote cluster>-control-plane:6443`,
   the remote control plane container that the lookup cluster's pods reach
   on Docker's `kind` network; and adds a tenant that reaches every portal
   on those two clusters, selected by a label the run gives their
   `ClusterConfig`s, an admin client and a tenant client, with passwords made
   up for the run;
4. waits until the Lookup Service lists both portals, each with one running
   environment at the run's capacity and no session;
5. requests the run's new sessions one after the other, one user each, and
   activates them only once the series is done, so that every session placed
   before a request still waits for activation in phase `Allocating`, the
   case the Lookup Service once failed to count;
6. activates each session by opening its activation URL, as the user's
   browser would;
7. has the first user placed on each cluster ask for the workshop again;
8. deletes the clusters, their configs and the CA key pairs minted for
   them.

The run passes when every check passes:

| Check | Passes when |
|---|---|
| New sessions alternate between the clusters | Every request got a new session, on one of the two clusters, and each pair of requests put one session on each cluster. |
| Every session's activation URL opens it | Each activation URL redirected to its session's page on its portal. |
| The clusters hold the sessions placed | Read with kubectl rather than through the Lookup Service, each cluster holds exactly the sessions placed on it, each `Allocated` to the user it was requested for. |
| A returning user on `<cluster>` gets their session back | One check for each cluster: the user got the same cluster, portal, environment and session as their first request, and its activation URL opens it. |
| Returning users add no session | The clusters still hold exactly the sessions the series placed. |

A failed check says what it saw, such as `requests 3 and 4 both went to
lookup-acceptance-a`. An error that stops the run, such as a cluster that
can't be created or a Lookup Service that never lists the remote cluster's
portal, fails the run with the error and what it last saw.

A run during which the host sleeps fails too, whatever its checks say. The
clusters' clocks stop while the host sleeps and jump forward when it
wakes, so the portals expire sessions the run placed, which then go
missing from a later check, and a check may pass or fail on what the jump
did rather than on what the Lookup Service did. The harness notes the
moments it runs every few seconds, and a gap of more than a minute between
two of them, from the first cluster to the last check, fails the run with
`The host slept`, saying when and for how long.

## Requirements

- A local build, made with `make local-build PACKAGE_VERSION=<tag>`, which
  writes the CLI of the build to `client-programs/bin/educates-<os>-<arch>`
  and pushes its images, the Lookup Service's among them, to the local
  registry under that tag. The harness runs that CLI; the `educates` on your
  `PATH` may be another build.
- Two addresses of the host that no other local cluster uses, such as
  aliases added to a network interface, as "Running several local clusters"
  in the local environment guide describes. The defaults are `10.10.10.18`
  and `10.10.10.19`.
- What `educates local cluster create` needs for a cluster served over
  HTTPS: Docker, the local DNS resolver for `*.educates.test`, and the CLI's
  CA root in its secrets cache, as `educates local secrets add ca` arranges.
  Both clusters' certificates chain to that root, which is how the Lookup
  Service trusts the remote cluster's portal; the harness checks that the
  two clusters' CA certificates are the same before it goes on. Clusters
  that a run reuses with `--reuse` may both be served over plain HTTP
  instead: the harness then reads no CA certificate and makes no such
  check.
- `kubectl` on the `PATH`.
- A host that stays awake for the whole run. On macOS, `run.sh` runs the
  harness under `caffeinate -i`, which keeps the host from sleeping while
  idle, but a laptop on battery still sleeps when its lid closes.
- Node 22.12 or later. `package.json` pins the version for Volta. The
  harness has no dependencies beyond Node itself.

## Running it

To check a branch, build it, then run the harness with the build's tag:

```
make local-build PACKAGE_VERSION=mytag
hack/harnesses/lookup-acceptance/run.sh --tag mytag
```

On a host whose free addresses are other than the defaults, name them:

```
hack/harnesses/lookup-acceptance/run.sh --tag mytag --lookup-address 10.10.10.6 --remote-address 10.10.10.7
```

A run takes about 15 minutes on a laptop, most of it creating the two
clusters. It never prompts. It prints its progress and the checks, writes
the report, and exits 0 when every check passed, 1 when one failed or the
run stopped on an error, and 2 when it could not start, as for an unknown
option, a CLI of another build, or a cluster of the run's name that already
exists.

The run deletes the clusters it used at the end, whether it passed or not.
With `--keep` it leaves them, with the portals, the Lookup Service's
configuration and the sessions, for inspection.

### Clusters an earlier run left

A run refuses to start when one of its clusters exists already, as after a
run with `--keep` or one that was interrupted. Then either delete them:

```
hack/harnesses/lookup-acceptance/run.sh --delete
```

or run on them with `--reuse`, which uses a cluster created by
`local-cluster.sh` from the same build tag on the same listen address, and
creates a missing one. On a cluster it reuses, the run first deletes the
portal an earlier run left, which takes the portal's sessions with it, and
applies the Lookup Service's configuration again. `--reuse --keep` repeats
the checks on the same clusters without creating them each time.

### Options

| Option | Meaning |
|---|---|
| `--tag <tag>` | The `PACKAGE_VERSION` of the local build to check, `lookup-acceptance` by default. The CLI must be that build's. |
| `--cli <path>` | The CLI of the build, this checkout's `client-programs/bin/educates-<os>-<arch>` by default. |
| `--lookup-cluster <name>` | The cluster whose Lookup Service is used, `lookup-acceptance-a` by default. |
| `--lookup-address <ip>` | Its listen address, `10.10.10.18` by default. |
| `--remote-cluster <name>` | The cluster the Lookup Service reaches as a remote cluster, `lookup-acceptance-b` by default. |
| `--remote-address <ip>` | Its listen address, `10.10.10.19` by default. |
| `--sessions <n>` | The new sessions to request, an even number from 2 to 10, 6 by default. The portal deletes a session that is not activated within 60 seconds of its request, so the whole series has to fit in that window. |
| `--reuse` | Use the clusters an earlier run left instead of refusing to start. |
| `--keep` | Leave the clusters, and everything the run made on them, in place. |
| `--delete` | Delete the clusters an earlier run left, and exit. Pass the cluster names when they are not the defaults. |
| `--out <dir>` | Where the report goes, by default `results/<time>` here. |

## The report

The results directory holds:

- `report.md`: the build, the clusters, the checks with their results, every
  session request with the cluster and session it got and what its
  activation URL answered, and the sessions each cluster held at the end;
- `report.json`: the same, as data;
- `run.log`: the run's progress, with the output of `local-cluster.sh`;
- `lookup-service.log`: the Lookup Service's log, which names the cluster,
  portal and environment of every placement that failed, and why.

Results are gitignored.

When the Lookup Service never lists the remote cluster's portal, the run
stops with what it last saw, and the Lookup Service's log says why it could
not connect to the remote cluster. Its pods reach the remote control plane
container by name through Docker's DNS, and the remote portal's host name
through the host's resolver for `*.educates.test`, so both clusters must run
on the same Docker host, with Docker Desktop's "Allow privileged port
forwarding to external machines" setting on, as `educates local cluster
create` requires.

## Tests

The checks a run is judged by, the kubeconfig rewrite and the detection of
a sleep have unit tests, which need no cluster:

```
cd hack/harnesses/lookup-acceptance && npm test
```
