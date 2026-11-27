---
name: educates-upgrade-host-images
description: Upgrade the container images the educates CLI runs as shared host services on a developer's Docker (the local registry and its mirrors, the DNS resolver, the router). Invoke when asked to "upgrade the host images", "bump haproxy / the router image / dnsmasq / the registry image", to fix vulnerabilities the host images scan reported, or to prepare a release.
argument-hint: "[registry|resolver|router] (omit to check all three)"
allowed-tools: "Read, Edit, Glob, Grep, Bash, WebFetch"
---

# Educates Host Images Upgrade Protocol

The CLI pins the images of its shared host services as Go constants, which
no dependency bot reads, so a rebuilt image with security fixes reaches
developers only when a **pin** moves. This skill moves them. The weekly
Host Images Scan workflow (`.github/workflows/host-images-scan.yaml`)
keeps one open issue while a pinned image has a fixable vulnerability of
High or Critical severity, and the release procedure runs this skill
before every release.

If `$ARGUMENTS` names a service (`registry`, `resolver` or `router`), work
on that one only; otherwise on all three.

## How each pin behaves

`hack/list-host-images.sh` is the list of pins: each service, the file and
constant that pin it, and the reference. A host image the CLI gains is
added there, and everything below reads it.

- **router**: `haproxy:<version>-alpine@sha256:<index digest>`, the Docker
  Official Image on the HAProxy long-term support branch the router
  targets, pinned by tag and digest. The CLI re-creates the router when it
  runs another image, which drops every open connection, terminals
  included, on every local cluster, so the pin moves only to an image that
  fixes something or brings a newer patch release. Moving to another LTS
  branch is a deliberate change: make it only when the user asks.
- **registry**: `docker.io/library/registry:3`, a major tag, used by the
  local registry and every registry mirror. The CLI pulls it when it
  creates one of those containers, so a rebuild or a 3.x release reaches a
  developer once they create the container again. Its constant changes
  only for a new major, and only when the user asks.
- **resolver**: `ghcr.io/dockur/dnsmasq:<dnsmasq version>`. Its image tags
  follow the `v<version>-r<n>` releases of `dockur/dnsmasq` on GitHub, and
  the CLI pulls it when it creates the resolver container.

A developer creates the registry, a mirror or the resolver again with
`educates local registry`, `educates local mirror` or
`educates local resolver`, `delete` then `deploy`.

## Steps

### 1. Scan the pins

```bash
hack/list-host-images.sh
GRYPE_CACHE_DIR=<a folder outside the repository> hack/scan-host-images.sh
```

The scan exits 1 when an image has a fixable vulnerability of High or
above, and 2 when an image could not be scanned: fix that before going on.
Done when you know, for each image in scope, its reference and its fixable
High and Critical vulnerabilities.

### 2. Find each image's candidate

- **router**: the newest image of the pinned branch, `<branch>` being the
  `major.minor` of the current tag:

  ```bash
  docker buildx imagetools inspect haproxy:<branch>-alpine
  docker buildx imagetools inspect haproxy:<branch>-alpine --format '{{json .Image}}'
  ```

  The first prints the index digest, on the `Digest:` line beside an
  image index media type; the second's `HAPROXY_VERSION` is its patch
  version. The candidate is `haproxy:<version>-alpine@<index digest>`;
  confirm that `haproxy:<version>-alpine` resolves to the same index
  digest. Always pin the index digest, never a platform manifest's, so the
  pin resolves on both arm64 and amd64.
- **resolver**: the newest release,
  `gh api repos/dockur/dnsmasq/releases/latest --jq .tag_name`, gives the
  tag `ghcr.io/dockur/dnsmasq:<version>`; confirm it resolves with
  `docker buildx imagetools inspect`.
- **registry**: `docker.io/library/registry:3` as it resolves today.

Done when each image has a candidate, or keeps its pin because nothing
newer exists.

### 3. Decide what moves

```bash
GRYPE_CACHE_DIR=<the same folder> hack/scan-host-images.sh <candidate>...
```

A candidate replaces its pin when it differs from it and fixes a
vulnerability or brings a newer release. When the newest image still
carries a vulnerability, upstream has not shipped its fix yet: record it
and keep the pin, since only upstream rebuilds these images. Done when each
image reads "moves to `<candidate>`, fixing `<CVEs>`" or "stays, because
`<reason>`".

### 4. Check a router candidate

From `client-programs/`, with the Go version `go.work` names:

```bash
ROUTER_IMAGE_CHECK=<candidate> GOTOOLCHAIN=go<version> \
  go test ./pkg/router -run TestRouterImageAcceptsTheRenderedConfiguration -count=1 -v
```

It runs the candidate's `haproxy -c` against a router folder rendered with
every kind of route. Done when it passes. When it fails, the new HAProxy
refuses what the router renders: stop and report its output, since
changing the rendering is a change of its own.

### 5. Move the pins

Edit each moving constant where `hack/list-host-images.sh` says it lives,
then search the repository for the old reference and the old version, and
update what still names them, such as the `educates local router status`
sample in `project-docs/getting-started/local-environment.md`. Done when
`hack/list-host-images.sh` prints the new references and the search finds
the old ones nowhere.

### 6. Release notes

In the release notes of the version being prepared
(`project-docs/release-notes/version-<X.Y.Z>.md`; the
`educates-release-notes` skill creates one), add an entry under Bugs
Fixed for a vulnerability fix, or Features Changed for a new version,
naming the image, its old and new versions, and the High and Critical
vulnerabilities it fixes. For the router, say that the CLI re-creates the
router once, on the next `educates local cluster create` or `start` or
`educates local router update`, closing open terminals. For a rebuild of
the registry's or the resolver's tag, give the commands that create the
container again. Match the file's prose: full sentences, wrapped lines,
double-backtick literals, and never an em dash.

### 7. Verify and report

Run `make ci-cli` from the repository root with the same `GOTOOLCHAIN`.
Report a table of each image, its old and new reference, and the
vulnerabilities fixed, followed by the images that stay and why. Commit
only when the user asks, as `fix(cli): ...` for a vulnerability fix or
`chore(cli): ...` for a version bump.

## The scanner

`hack/scan-host-images.sh` runs Grype from its own image, pinned by tag
and index digest in the script: Grype's releases are on
`https://github.com/anchore/grype/releases`. When the user asks to move
it, pin the new tag with its index digest, as for the router.
