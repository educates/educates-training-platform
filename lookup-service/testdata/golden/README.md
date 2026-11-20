# Golden responses of the Python Lookup Service

This directory records what the Python Lookup Service answered to a fixed
scenario on two kind clusters: every route of its REST API, with the status
code, content type and body of each response. The Go service's REST suite
rebuilds the scenario in envtest and asserts that the Go service gives the
same answers, except where a fixture marks a deliberate change.

## Provenance

`capture.json` records where the responses came from: the commit the
clusters were built from, `jorgemoralespou/educates-training-platform@38515f3d`,
whose `lookup-service` code and subchart were those of `develop`; the
Lookup Service image and its digest; the Kubernetes version; the time of the
capture; and the two local clusters. The fixtures were captured live, through
the lookup cluster's ingress, except the one under `derived/`, which is
derived from the Python code.

## Layout

| Path | What it holds |
|---|---|
| `scenario/` | The resources of the scenario: the lookup configuration applied in `educates-config` on the lookup cluster, and the workshops and training portals of each cluster. |
| `states/<step>.json` | The training resources the service watched at each step of the scenario, as the service reads them, on both clusters. |
| `fixtures/NNN-<slug>.json` | One request each, in the order they were sent, with its response. |
| `derived/` | Fixtures derived from the Python code, for a case that cannot be captured live. |
| `capture.json` | The provenance of the capture. |
| `capture.py` | The script that ran the scenario and wrote the fixtures, states and provenance. |

## The scenario

### Clusters

- **cluster-a** is the cluster the Lookup Service runs on. Its
  `ClusterConfig` names no kubeconfig Secret, so the service reaches it
  through the mounted `remote-access` token and the in-cluster API server.
  Its labels are `location: a` and `environment: golden`.
- **cluster-b** is a remote cluster, reached through the kubeconfig in the
  Secret `cluster-b-kubeconfig`, under the default key `config`: the output
  of `educates admin lookup kubeconfig` for that cluster, with its server
  address rewritten to the cluster's kind control plane container,
  `https://educates-lsg-golden-b-control-plane:6443`, which the first
  cluster's pods reach. Its labels are `location: b` and
  `environment: golden`.

### Training portals and workshops

| Cluster | Portal | Session maximum | Labels | Workshop | Environment | Capacity | Reserved |
|---|---|---|---|---|---|---|---|
| cluster-a | portal-a | 4 | `region: east` | lab-alpha | portal-a-w01 | 2 | 1 |
| cluster-a | portal-a | 4 | `region: east` | lab-beta | portal-a-w02 | 2 | 0 |
| cluster-b | portal-b | none | `region: west` | lab-alpha, the alias of the Workshop lab-alpha-b | portal-b-w01 | 2 | 0 |

The workshops have no content: the capture needs the portal's session
phases, not a working workshop. Sessions expire 60 minutes after they are
allocated. The environment of cluster-b has `lab-alpha` as its
`training.educates.dev/portal.workshop` label and `lab-alpha-b` as its
`spec.workshop.name`, and its title, description and labels are those of
cluster-a's lab-alpha.

### Tenants

| Tenant | Selectors | Reaches |
|---|---|---|
| tenant-all | none | portal-a, portal-b |
| tenant-names | cluster names `*-b`, portal names `portal-*` | portal-b |
| tenant-labels | cluster label `location: a`; portal labels `region In [east]` and `retired DoesNotExist` | portal-a |
| tenant-none | cluster label `location Exists`; portal label `region NotIn [east, west]` | no portal |

### Clients

| Client | Roles | Tenants | `spec.user` |
|---|---|---|---|
| admin-client | admin | none | |
| tenant-client | tenant | `tenant-*` | |
| both-client | admin, tenant | `tenant-all` | |
| user-client | tenant | `tenant-all` | `fixed-user` |
| revoke-client | tenant | `tenant-all` | |
| rotate-client | tenant | `tenant-all` | |
| doomed-client | tenant | `tenant-all` | |

The passwords are those `scenario/lookup/clientconfigs.yaml` gives, such as
`golden-tenant-password` for tenant-client. rotate-client's password becomes
`golden-rotate-password-2` during the capture.

### Steps

Each fixture names the step whose state held when its request was sent, and
`states/<step>.json` gives the training resources of both clusters in that
state. The session requests that make a session come last in their step,
and the next step starts from the state they led to. A fixture's
`actions` list what was done to the clusters right before its request, such
as an update to a `ClientConfig`; a rebuilt scenario does the same before
sending the request. `activate session <session:LABEL>` means the session's
user opened its `sessionActivationUrl`, and the portal set the session's
phase to `Allocated`.

1. **initial**: both portals run, portal-a's lab-alpha holds one reserved
   session in phase `Available`, and no session has been requested. The
   requests are every login, the token checks, the client, tenant, portal
   and cluster routes, the workshop lists, the session requests that are
   refused before a session is made, and last tenant-client's request of
   lab-beta for `user-1`.
2. **beta-allocating**: `user-1`'s session waits for activation in phase
   `Allocating`. The admin reads portal-a and the session's environment, and
   the same request again reacquires the session. `user-1` then activates
   it.
3. **beta-allocated**: `user-1`'s session is `Allocated`. The same request
   again reacquires it, and the same request through tenant-names, which
   reaches no portal offering lab-beta, still returns it. Last, tenant-client
   requests lab-alpha for `user-2` through tenant-names, which gives a
   session on portal-b, on the remote cluster.
4. **remote-allocating**: `user-2`'s session on portal-b waits for
   activation in phase `Allocating`. The admin reads portal-b, and the same
   request again reacquires the session. Then user-client, whose `spec.user`
   overrides the request's `clientUserId`, requests lab-alpha through
   tenant-all and is given portal-a's reserved session, and tenant-client
   requests lab-beta without `clientUserId`. Both sessions are activated.
5. **portal-a-busy**: lab-beta is at its capacity of 2, both its sessions
   `Allocated`, and lab-alpha holds the `spec.user` session and a new
   reserved one the portal made. The admin reads cluster-a's inventory, and
   a further request for lab-beta is refused.
6. **remote-expired**: `user-2` never activated the remote session; the
   portal expired it 60 seconds after it was last requested and deleted it.
   The same request again gives a new session on portal-b, which is
   activated.
7. **reserved-stopping**: portal-a's session maximum has been lowered to 3,
   one less than its sessions, so the portal terminates its reserved
   lab-alpha session, which is in phase `Stopping` until it is deleted. The
   admin reads the environment and portal-a. This state was taken right
   after the step's requests, as the phase lasts seconds.
8. **sessions-settled**: every session left is `Allocated`. The admin reads
   both clusters' inventory, and then the client changes run: revoke-client
   logs out and is then updated, rotate-client's password is changed, and
   doomed-client is deleted and created again.
9. **label-without-value**: portal-b has gained a label without a value,
   `{name: featured}`, which breaks the Python service's tenant matching for
   every tenant whose clusters include cluster-b. The label is removed again
   at the end of the step.
10. **cluster-b-deleted**: cluster-b's `ClusterConfig` is deleted; its
    training resources stay.
11. **restarted**: the Lookup Service pod has been deleted and its
    replacement is Ready. The tokens issued before the restart are checked.

What the training portal does decides some of these states, and a rebuilt
scenario should do the same:

- A session request for an environment that holds a reserved session is
  given that session, which goes from `Available` to `Allocating`, and the
  portal then makes a new reserved session if the environment's capacity and
  the portal's maximum allow, as `session-request-spec-user` shows.
- A session requested through the REST API stays `Allocating` until it is
  activated, and becomes `Allocated` then. Reacquiring it while it waits
  restarts its 60 second activation window.
- A portal refuses a request for an environment at its capacity, and the
  Lookup Service answers 503 `Workshop not available` when no other
  candidate is left, as `session-environment-full` shows.
- An expired session that was never activated is deleted while still in
  phase `Allocating`. The portal sets `Stopping` only on reserved sessions
  it terminates and on the sessions of an environment that stops.
- A session requested without `clientUserId` belongs to a user the portal
  creates with a random UUID as its name, while the Lookup Service answers
  `clientUserId` with the empty string.

## Fixtures

A fixture is a JSON object:

| Member | What it holds |
|---|---|
| `id` | The capture order and a slug, `NNN-<slug>`. The slug is unique. |
| `step` | The step of the scenario the request was sent in. |
| `description` | What the request is. |
| `source` | `captured`, or `derived` for a fixture under `derived/`. |
| `derivedFrom` | For a derived fixture, the code and facts it was derived from. |
| `actions` | What was done to the clusters right before the request. |
| `request` | `method`, `path` with its query string, `headers` and `body`, the body exactly as sent or `null`. |
| `response` | `status`, `contentType`, `bodyFormat` (`json`, `text` or `yaml`) and `body`: a JSON value for `json`, a string otherwise. |
| `issuedToken` | For a login that issues a token: the token's placeholder `name`, its JWT `header` and `claims`, and `lifetimeSeconds`. |
| `change` | For a response the Go service deliberately answers differently: see "Deliberate changes". |

## Placeholders

Values that differ between captures, or between the kind clusters and an
envtest rebuild, are replaced by placeholders. A placeholder is a string in
angle brackets, standing for the whole value it replaces or, inside a URL
or a `jti`, for part of it.

| Placeholder | Stands for |
|---|---|
| `<token:NAME>` | An access token. In a login fixture's response it is the token the fixture issues, named by `issuedToken.name`; in a request's `Authorization` header it is the token issued under that name. A client's later token takes a suffix, as `<token:tenant-client#2>`. |
| `<token:expired>` | An HS256 token signed with the service's own key, with `sub: tenant-client`, tenant-client's `jti` at the time, and an `exp` one hour in the past. It was signed in the Python service's pod with its key. |
| `<token:foreign>` | An HS256 token with the same claims and an `exp` an hour ahead, signed with the key `not-the-service-key`. |
| `<expires-at>` | An integer, the Unix time a token expires at: `exp` of the token, 72 hours after it was issued. |
| `<uid:CLUSTER/KIND/NAME>` | The `metadata.uid` of that object on that cluster. `<uid:cluster-a/ClientConfig/doomed-client#2>` is the uid of the doomed-client created again. |
| `<generation>` | An integer: a portal's or a session's `metadata.generation`, or an environment's `status.educates.workshop.generation`. `states/` give the values captured. |
| `<session:LABEL>` | A session name. The label names the request that created the session, as `beta-user-1`, or is `alpha-reserved` for portal-a's first reserved session, or `<environment>-<n>` for a later session the portal made itself. |
| `<portal-user:LABEL>` | The user name the portal generated for the session `LABEL`, requested without `clientUserId`. |
| `<activation-token>` | The activation token in a `sessionActivationUrl`'s query string. |
| `<token>`, `<certificate-authority-data>` | In a kubeconfig, the user's token and the cluster's CA data. |
| `<server:cluster-b>` | In cluster-b's kubeconfig, the server address the capture wrote into its Secret. |

The same identity placeholder stands for the same value in every fixture of
the capture. A test binds `<uid:...>` to the objects it creates and
`<token:...>` to the tokens its logins return, and binds a `<session:...>`
or `<portal-user:...>` placeholder the first time a response shows it, every
later sighting having to equal it.

Everything else is literal: client, tenant, cluster, portal and environment
names, portal URLs, labels, counts, phases and error texts. A rebuilt
scenario gives its portals the captured `status.educates.url` and its
environments the captured names, as `states/` show them.

## Comparing a response

- The status code is equal.
- The content type is equal: `application/json; charset=utf-8` or
  `text/plain; charset=utf-8`.
- A `text` body is equal byte for byte. None ends in a newline, which Go's
  `http.Error` would add. The bodies of `unknown-route`, the 404 of a path
  no route serves, of `auth-login-wrong-method`, the 405 of a route asked
  with another method, and of every 500 are aiohttp's defaults.
- A `json` body is equal as a JSON value, once placeholders are bound:
  object member order and whitespace are free, as aiohttp writes `", "`
  and `": "` separators. Arrays compare as multisets, their order not being
  part of the contract: the Python service lists resources in the order it
  registered them, and an environment's users in no order.
- A `yaml` body, a kubeconfig, is equal as a parsed YAML document once
  placeholders are bound. PyYAML wrote it with sorted keys.

## Deliberate changes

A fixture whose response the spec deliberately changes has a `change`
member: a `summary` of what changes, the `spec` section and user story that
change it, and the `expected` response of the Go service. `expected` holds
`status`, and `contentType` and `bodyFormat` where they differ from the
captured response, and says what the body is in one of these ways:

| Member | The expected body |
|---|---|
| `body` | This body, in placeholder form. |
| `bodyContains` | A text body containing this string, which is all the spec fixes. With `body` too, `body` is the text this capture suggests. |
| `sameBodyAs` | The body of the fixture with that slug, captured in the same step. |
| `fields` | The captured JSON body with these top-level members replaced. |
| `items` | The captured JSON list body with members replaced in the listed elements: `{"portals": {"cluster-b/portal-b": {"allocated": 1}}}` replaces `allocated` in the element whose `cluster` is cluster-b and `name` is portal-b. |
| `only` | The captured JSON list body keeping only the listed elements, named as for `items`, or by `name` alone for a list of clusters. `capture.py` writes it when the Python service keeps showing a deleted cluster, which this capture did not see. |
| none of these | A body in `bodyFormat` that the spec does not fix. |

The changes the fixtures mark:

| Fixtures | Python | Go |
|---|---|---|
| `auth-login-malformed-json`, `auth-login-empty-body`, `auth-login-array-body`, `session-malformed-json`, `session-array-body` | 500, aiohttp's default body | 400, a plain-text body the spec does not fix |
| `session-missing-index-url` | 503 `Workshop not available`, after every portal refuses | 400 naming `clientIndexUrl`, before any portal is contacted; `Missing clientIndexUrl` follows the service's `Missing tenantName` and `Missing workshopName` |
| `allocating-*`, `remote-allocating-*` | `allocated` leaves out `Allocating` sessions | `allocated` counts them |
| `stopping-*` | `allocated` leaves out `Stopping` sessions | `allocated` counts them |
| `session-affinity-outside-tenant` | returns the user's session on a portal outside the tenant's reach | 503 `Workshop not available` |
| `auth-verify-revoked-after-update` | an update that keeps the password revives revoked tokens | 401 `Client identity does not match` |
| `auth-verify-after-password-change` | a password change leaves tokens valid | 401 `Client identity does not match` |
| `label-workshops-tenant-all`, `label-tenant-portals-tenant-names` | a label without a value crashes tenant matching with a 500 | 200, the label matching the empty string |
| `restarted-verify-old-token` | a restart invalidates every token | 200: tokens survive a restart |
| `D01-session-portal-timeout` | a portal that never answers its token endpoint gives a 500 after 300 seconds | 503 `Workshop not available` within seconds, after trying the other candidates |

A label without a value is shown as the Python service shows it,
`{"name": "featured"}`, with no `value` member.

## Derived fixtures

`derived/D01-session-portal-timeout.json` is the response to a session
request whose only candidate portal accepts connections and never answers
its OAuth token endpoint. Capturing it live takes a hung portal and a
300 second wait behind an ingress with a shorter timeout of its own, so it
is derived from the code its `derivedFrom` cites. It belongs to no step: a
test builds its scenario as its `scenario` member describes.

## Rebuilding the capture

`capture.py` runs the scenario against two named local clusters made from
one build with `hack/harnesses/local-cluster.sh`, and rewrites `fixtures/`,
`states/` and `capture.json`; it leaves `derived/` alone. It needs Python 3,
kubectl and the educates CLI of that build, and needs the Python Lookup
Service, which the repository no longer holds, so it can be rerun only on a
build of a commit that holds it, such as the one `capture.json` names. The
clusters must hold none of the scenario's resources and the Lookup Service
must have started since they were removed:

```
make local-build PACKAGE_VERSION=<tag>
hack/harnesses/local-cluster.sh create lsg-golden-a --listen-address 10.10.10.3 --tag <tag>
hack/harnesses/local-cluster.sh create lsg-golden-b --listen-address 10.10.10.4 --tag <tag>
python3 -I lookup-service/testdata/golden/capture.py --lookup-cluster lsg-golden-a --remote-cluster lsg-golden-b
hack/harnesses/local-cluster.sh delete lsg-golden-a
hack/harnesses/local-cluster.sh delete lsg-golden-b
```

The run takes a few minutes, most of it waiting on the portals. It fails when
a step's state does not come about in time, and then leaves `fixtures/` and
`states/` empty.
