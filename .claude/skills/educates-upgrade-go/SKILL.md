---
name: educates-upgrade-go
description: Upgrade the Go version across the entire educates-training-platform project. Invoke when asked to "upgrade go version", "bump golang", "update go", or "use go 1.xx". Updates go.work, all go.mod files, all Dockerfiles, and CI/CD workflow files.
argument-hint: "<new-go-version> (e.g. 1.24)"
allowed-tools: "Read, Edit, Glob, Bash"
---

# Educates Go Version Upgrade Protocol

Upgrade the Go version to `$ARGUMENTS` (e.g. `1.24`) across all files in the project.

## Step 1: Find the Current Version

Check the current Go version before making changes:

```bash
grep -rn "^go 1\." go.work client-programs/go.mod installer/operator/go.mod node-ca-injector/go.mod assets-server/go.mod image-puller/go.mod
```

## Step 2: Update All Files

You must update **all** of the following. Do not skip any.

### Go Module Files

- **`go.work`** — update the `go 1.xx` directive. The workspace covers
  `client-programs/`, `installer/operator/`, and `node-ca-injector/`.
- **`client-programs/go.mod`** — update the `go 1.xx` line
- **`installer/operator/go.mod`** — update the `go 1.xx` line (the kubebuilder operator)
- **`node-ca-injector/go.mod`** — update the `go 1.xx` line
- **`assets-server/go.mod`** — update the `go 1.xx` line (Docker-only module, not in `go.work`)
- **`image-puller/go.mod`** — update the `go 1.xx` line (Docker-only module, not in `go.work`)

Discover every module so none is missed (the platform modules are at the
repo root; ignore any `go.mod` under `workshop-samples/`, which is sample
workshop content with its own lifecycle):

```bash
git ls-files 'go.mod' '*/go.mod' | grep -v '^workshop-samples/'
```

Note: `tunnel-manager` is a Python (uv) component and has no `go.mod`.

### Dockerfiles

Use the plain `golang:1.xx` image tag — do not switch to Alpine, Buster, Trixie,
or other derivative tags. Only the `golang:1.xx` version changes — **do not add
or remove the `--platform=$BUILDPLATFORM` prefix** as part of a version bump.

- **`client-programs/Dockerfile`** — `FROM --platform=$BUILDPLATFORM golang:1.xx AS builder`
- **`installer/operator/Dockerfile`** — `FROM --platform=$BUILDPLATFORM golang:1.xx AS builder`
- **`node-ca-injector/Dockerfile`** — `FROM --platform=$BUILDPLATFORM golang:1.xx AS builder`
- **`assets-server/Dockerfile`** — `FROM --platform=$BUILDPLATFORM golang:1.xx AS builder`
- **`image-puller/Dockerfile`** — `FROM --platform=$BUILDPLATFORM golang:1.xx AS builder`
- **`workshop-images/base-environment/Dockerfile`** — `FROM --platform=$BUILDPLATFORM golang:1.xx as builder-image`

Search for any other Dockerfile referencing golang:

```bash
grep -rn "FROM .*golang:" --include="Dockerfile*" .
```

#### About `--platform=$BUILDPLATFORM`

It is not a standalone flag — it is correct **only** as a three-part
cross-compile set in the same stage:

1. `FROM --platform=$BUILDPLATFORM golang:1.xx AS builder` (builder runs on the
   build host, not emulated),
2. `ARG TARGETOS` / `ARG TARGETARCH`,
3. `... GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build ...` (cross-compile to the
   target).

With all three, a multiarch build cross-compiles natively instead of running the
Go toolchain under QEMU for each non-native arch — much faster. **Adding the
prefix to a stage that builds with a plain `go build` (no `GOOS`/`GOARCH`)
produces a wrong-architecture binary** — so the prefix and the `GOOS/GOARCH`
build must always go together.

All six Go Dockerfiles above currently cross-compile and carry the prefix. If
you ever add a new Go build stage, give it the full three-part set (or no
`--platform` at all) — never the prefix alone.

#### Pinned patch releases (`GOTOOLCHAIN`)

The `golang:1.xx` tag floats over patch releases, so a patch-only upgrade (for
example 1.26.9 to 1.26.10) leaves the `FROM` lines unchanged. Several source
rebuild stages pin the exact patch with `ENV GOTOOLCHAIN=go1.xx.y`, because the
image can lag a patch behind the release that carries stdlib security fixes.
These pins must move with the `go.mod` files: a stage pinned below the version a
`go.mod` it builds requires (the `package-fetcher-build` stage compiles the CLI
from source) fails with a `go.mod requires go >= ...` error.

```bash
grep -rn "GOTOOLCHAIN=go1\." --include="Dockerfile*" .
```

Update every pin that matches the old version, in `docker-registry/Dockerfile`
and the stages of `workshop-images/base-environment/Dockerfile`. Leave a pin on a
different minor alone: the `kustomize-build` stage deliberately uses a newer
`golang` image and `GOTOOLCHAIN` because kustomize needs a newer Go, as its
comment explains.

### CI/CD Workflows

The Go workflows (`.github/workflows/*.yaml` — note the extension) pin the
toolchain with `actions/setup-go` using `go-version-file:` pointing at a module's
`go.mod` (e.g. `client-programs-ci.yaml` → `client-programs/go.mod`,
`installer-operator-ci.yaml` → `installer/operator/go.mod`). Because the version
is read from `go.mod`, bumping the `go.mod` files above is normally enough — no
hardcoded version to edit.

Still confirm there's no stray hardcoded version left behind:

```bash
grep -rnE "go-version:|golang:1\." .github/workflows/
```

If any `go-version: '1.xx'` literal exists, update it (or, preferably, convert it
to `go-version-file:`).

## Step 3: Verify

Run in order, fix any failures before proceeding:

1. Sync dependencies in every module (run `go mod tidy` in each):

```bash
for m in client-programs installer/operator node-ca-injector assets-server image-puller; do
  (cd "$m" && go mod tidy)
done
```

2. Build the CLI and the operator:

```bash
make build-cli                        # CLI (build-client-programs is an alias)
make -C installer/operator build      # operator manager binary
```

3. Run tests (the operator's envtest is the most likely to surface a
   toolchain issue):

```bash
cd client-programs && go test ./...
make -C installer/operator test
```

## Step 4: Post-Verification Checklist

- [ ] `go.work` updated
- [ ] All five platform `go.mod` files updated (`client-programs`, `installer/operator`, `node-ca-injector`, `assets-server`, `image-puller`)
- [ ] All six Dockerfiles with `FROM ...golang:` updated, `--platform=$BUILDPLATFORM` prefix preserved where present
- [ ] Every `ENV GOTOOLCHAIN=go1.<old>` pin in Dockerfiles moved to the new version
- [ ] CI: no stray hardcoded `go-version:`/`golang:1.` literal left (versions come from `go-version-file: <module>/go.mod`)
- [ ] `go mod tidy` ran cleanly in all modules
- [ ] CLI builds (`make build-cli`) and operator builds + tests (`make -C installer/operator build test`)
- [ ] No remaining references to the old Go version (`grep -rn "golang:1\.<old>" --include="Dockerfile*" .`)

If `go mod tidy` changes `go.sum`, commit that change but confirm with the user first.
