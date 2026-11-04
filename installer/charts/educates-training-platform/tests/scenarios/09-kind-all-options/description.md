# Scenario 09 — kitchen sink: every subchart on, every value block set

The "complete install" scenario: all five subcharts enabled (including
lookup-service and remote-access, which no other scenario turns on) and
every session-manager value block populated. One run validates that the
chart's full typed-values surface serialises into the runtime config and
that the optional subcharts coexist on a single kind cluster.

## Layout of the test

1. `educates local cluster create` provisions kind + Contour + Kyverno
   (no cert-manager — TLS material is generated offline, as in 02).
2. `pre-install.sh`:
   - Generates (or reuses, via the runner's `--tls-*`/`--ca-*` flags) a
     CA + wildcard leaf for `*.${DOMAIN}` and publishes them as
     `wildcard-tls` / `wildcard-ca` in `educates-secrets` (same as 02).
   - Creates two website-theme Secrets in `educates-secrets`:
     `scenario-theme` (referenced via `websiteStyling.themeDataRefs` and
     set as `defaultTheme`) and `extra-theme` (copied via
     `secretPropagation.upstream.websiteThemes`).
   - Creates a dummy docker-registry Secret `scenario-pull-secret` in
     `educates-secrets` (copied in via `secretPropagation.upstream.
     imagePullSecrets`, then propagated by name).
3. `helm install` lands the chart with everything enabled.
4. `post-deploy.sh` asserts the full chain (see below).

## What this proves

- **Subchart coexistence** — lookup-service (with its Ingress at
  `lookup.${DOMAIN}`), remote-access, node-ca-injector, and the
  image-puller DaemonSet all deploy alongside session-manager and
  secrets-manager in one release. The image-puller DaemonSet rolls out
  only once every listed image is pulled: the defaults, an inventory
  name and a full reference, neither of the last two having `/bin/true`.
- **Typed-values serialisation** — `post-deploy.sh` reads the live
  `educates-config` Secret and asserts every populated block landed:
  sessionCookies, clusterNetwork.blockCIDRs, dockerDaemon (MTU +
  proxyCache), all four workshopAnalytics providers, websiteStyling
  (defaultTheme + frameAncestors), the appended `imageVersions` entry,
  and the `config:` escape-hatch marker.
- **Secret plumbing** — the cross-namespace TLS/CA refs, both theme
  Secrets, and the pull Secret are all copied into the release
  namespace by the SecretCopiers the chart renders.
- **Theme end-to-end** — `defaultTheme` points at the external
  `scenario-theme` Secret; the portal HTML must serve its marker. The
  inline theme assets are asserted at the `default-website-theme`
  Secret level.
- **Kyverno extras on both paths** — cluster-wide and per-workshop
  marker policies (same shape as scenario 06; asserted at the
  ClusterPolicy level only, 06 owns the deep per-environment checks).

## Out of scope

- Per-pod operational knobs — `image.{repository,tag,pullPolicy}`,
  `imagePullSecrets`, and `resources` on each subchart, plus
  `development.imageRegistry`. Left at defaults so the test runs the
  released image refs; the header comment in `chart-values.yaml` lists
  them and how to set them.
- `clusterRuntime.class` — left unset; kind has no alternative runtime
  class (e.g., kata) installed.
- `clusterStorage.user` — left null; chowning volumes to a UID is for
  NFS-backed classes and conflicts with pod security enforcement.
- `development.imageRegistry` and per-pod `image:` overrides — these
  point the install at locally-built images; the scenario tests the
  released refs.
- Workshop-level pulls through `scenario-pull-secret` — the secret
  carries dummy credentials; only its copy/propagation is asserted
  (scenario 05 proves real authenticated pulls).
- Deep per-environment Kyverno assertions (scenario 06) and real
  containerd CA pulls through node-ca-injector (scenario 08).

## Notes for the runner

- TLS resolution order and flags are identical to scenario 02 (supply
  `--ca-cert`/`--ca-key` from mkcert for a browser-trusted run).
- `remoteAccessTokenMount` is on by default in lookup-service, and
  remote-access is enabled, so the token Secret the Deployment mounts
  exists — don't disable one without the other.
