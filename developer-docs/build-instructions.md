Build Instructions
==================

If you are hoping to contribute to Educates, before you attempt to build a local version of Educates and start modifying any code, read our [contribution guidelines](../CONTRIBUTING.md) and reach out to us to discuss any changes you are considering.

Local Kubernetes environment
----------------------------

To do development on the core Educates platform you will need access to a Kubernetes cluster. For this we recommend you use a local Kubernetes cluster created using [Kind](https://kind.sigs.k8s.io/). Rather than you create this Kind cluster yourself, you can create it using the `educates` CLI. This will ensure that the Kind cluster is setup properly for how the Educates code is structured for doing local development.

When creating a local Kubernetes cluster with Educates the `educates create-cluster` command is used. For the case of wanting to do local development on Educates itself, you need to disable installation of the services required by Educates, and the core Educates platform. Provided you are using version 3.0 or later of the Educates CLI this is done using the command:

```
educates create-cluster --cluster-only
```

You can subsequently delete the local Kubernetes environment by running:

```
educates delete-cluster
```

Ensure you read the main Educates documentation quick start guide for any requirements around running the local Kubernetes cluster and Educates, deployed using the `educates` CLI.

Local docker image registry
---------------------------

When the local Kubernetes cluster is created using `educates create-cluster`, a local docker image registry will also be deployed to the local docker environment.

The docker image registry will be available at `localhost:5001` and will be used to hold container images built from the Educates source code. This image registry will also be used as the source of images when Educates is deployed to the local Kubernetes cluster.

If over time the amount of storage consumed by the local docker image cache increases to the point where overall available space within the local docker environment runs low, you can try to clean out unreferenced image layers by running:

```
docker image prune
```

The local docker image registry created using the Educates CLI can also grow in size due to unreferenced images. To prune unreferenced image layers kept by the local docker image registry you can run:

```
educates local registry prune
```

If you need to delete the local docker image registry and redeloy it, you can run:

```
educates local registry delete
```

to delete it, and:

```
educates local registry deploy
```

to recreate it. You will however need to push any previously built images to the local docker image registry again if this is done.

Note that this later command will create/update service resources in the Kubernetes cluster which are used to map and make available the local docker registry in the cluster. The original `educates create-cluster` command will also configure `containerd` within the Kubernetes cluster to trust the local docker image registry. It is thus important to use the `educates` CLI to deploy the local docker image registry rather than attempting to deploy a local docker image registry yourself.

Defining installer configuration
--------------------------------

Before building and deploying Educates from source code, you will need a configuration describing the target cluster and what is to be installed. The local configuration is managed by the `educates` CLI itself and lives in the CLI data home (by default `$XDG_DATA_HOME/educates/config.yaml`). Create a minimal configuration by running:

```
educates local config init
```

You can inspect it with `educates local config view` and change it with `educates local config edit`. The local configuration uses the `EducatesLocalConfig` format and should contain at least:

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: 192-168-1-1.nip.io
```

The `EducatesLocalConfig` kind implies an opinionated configuration suitable for a Kubernetes cluster created using Kind: the operator deploys an ingress router for the cluster using Contour, cert-manager for TLS, and Kyverno for implementing cluster and workshop security policies.

The `domain` should be set to be a `nip.io` address mapping to the IP address of your local host where you are doing development, or some other FQDN which maps to your local host. If left unset, the deploy command falls back to a `nip.io` address derived from your host IP.

If the configuration requires additional secrets these will need added to the local Kubernetes cluster in the namespace required by the configuration. If these secrets had previously been added to the local secrets cache, you can copy them to the local Kubernetes cluster by running:

```
educates local secrets sync
```

Building Educates from source
-----------------------------

To build everything needed for local testing — the `educates` CLI, the core platform images and the installer operator image — run, from the repository root:

```
make
```

This is the default target (`local-build`). It:

* builds the `educates` CLI for your host platform into `client-programs/bin/educates-<os>-<arch>`, with compiled-in defaults pointing at the local image registry (`localhost:5001`, tag `latest`);
* deploys the local image registry container using that CLI if it isn't already running;
* builds the core platform images and the operator image **for your host architecture only** and pushes them to the local registry;
* regenerates the committed embedded artifacts (operator CRDs, the CLI-embedded operator chart and JSON schemas) when their sources changed — if `git status` is dirty after `make`, commit the regenerated files;
* packages the runtime subchart tarballs the operator embeds from their sources under `installer/charts/educates-training-platform/charts/`. These are gitignored build output rather than committed files, and every target which compiles the operator or builds its image packages them first. To run `go build` or `go test` directly in `installer/operator/` on a fresh clone, run `make package-local-charts` first, since `//go:embed` needs the files to exist.

Then deploy the locally built system:

```
client-programs/bin/educates-<os>-<arch> local cluster create
```

Because the CLI was built with a non-release version (`latest`), it identifies itself as a development build and automatically points every platform image — the operator, secrets-manager, lookup-service, session-manager, training-portal, the workshop base environment and the rest of the core set — at `localhost:5001/educates-<name>:latest`. No configuration editing is needed. Explicit `imageVersions` or `operator.image` entries in your local configuration always win over these defaults, so remove any manual overrides left over from earlier workflows unless you want them.

Make knobs (environment variables or `make VAR=value`):

* `TARGET_PLATFORMS` — image platforms. Defaults to the current host architecture only (e.g. `linux/arm64` on Apple silicon); multi-architecture builds are explicit opt-in (`TARGET_PLATFORMS=linux/amd64,linux/arm64`) and slow under emulation.
* `IMAGE_REPOSITORY` / `PACKAGE_VERSION` — registry and tag for images (default `localhost:5001` / `latest`).
* `PUSH_IMAGES=false` — load images into the Docker daemon instead of pushing.
* `CLI_VERSION` / `CLI_IMAGE_REPOSITORY` — the CLI binary's compiled-in defaults; a semver `CLI_VERSION` makes the binary behave like a release build (no local-image defaulting).

Any single image can be rebuilt with `make image-<name>`, for example `make image-training-portal` or `make image-operator` (the operator target refreshes the embedded subchart tarballs first). Run `make help` for the full target list. Building requires `docker`, Go and `helm` (used to repackage the runtime subcharts the operator embeds).

After rebuilding runtime component images, restart the deployed components to pick them up (dev tags are pulled with `Always`):

```
make restart-training-platform
```

For the operator itself, `make image-operator` then `kubectl rollout restart deployment -n educates-installer`. See the [operator README](../installer/operator/README.md) and the make targets there (`make smoke-test`, `make test`) for the operator development loop.

If the cluster is already up and you only want to (re)deploy the platform, run:

```
client-programs/bin/educates-<os>-<arch> admin platform deploy --local-config
```

To delete everything deployed to the cluster, use:

```
educates admin platform delete
```

Building additional workshop images
-----------------------------------

The default `make` only builds the main workshop base image. The workshop language images (Java and Python variants) are optional — when a workshop uses one and it isn't built locally, the published `ghcr.io/educates` image is used. To build them locally:

```
make build-workshop-images
```

or everything at once with `make build-all-images`. Note that this will consume a lot more storage space in the local docker environment. In general you will probably want to configure the local docker environment with 100Gi or more of storage space to be used across local image caching, the local docker image registry and the Kubernetes cluster itself.

Individual workshop images can be built with the generic image target:

```
make image-base-environment
make image-jdk17-environment
make image-conda-environment
```

If you want a locally built language image to be used by deployed workshops, add a matching `imageVersions` entry to the local configuration (only the core set is defaulted automatically), for example `name: jdk17-environment`, `image: localhost:5001/educates-jdk17-environment:latest`.

Building the Educates CLI program
---------------------------------

If needing to work on just the `educates` CLI it can be built using the command:

```
make build-cli
```

You can then run the `educates` CLI program from the `client-programs/bin` subdirectory. The name of the compiled CLI will incorporate the target system and machine architecture, e.g.: `educates-darwin-arm64`. (`make build-client-programs` remains as an alias.)

Note that when building the `educates` CLI from local source code, the embedded project version defaults to `latest` and the embedded image registry to `localhost:5001` — the CLI behaves as a development build, defaulting all platform images to your local registry as described above. The operator Helm chart and config schemas embedded in the CLI are refreshed from your source tree as part of the build. To build a CLI that behaves like a release binary, pass a semver version: `make build-cli CLI_VERSION=4.0.0 CLI_IMAGE_REPOSITORY=ghcr.io/educates`.

Running CI checks locally
-------------------------

The GitHub Actions workflows that gate pull requests
(`.github/workflows/client-programs-ci.yaml` and
`.github/workflows/installer-operator-ci.yaml`) invoke these same make
targets, so running them locally exercises exactly what CI runs — there is
no second copy of the step list to drift out of sync. From the repository
root:

```
make ci             # run all CI checks (CLI + operator)
make ci-cli         # only the client-programs checks
make ci-operator    # only the installer-operator checks
```

`make ci-cli` is what the client-programs workflow runs: it stages the
embedded theme files (see below), then runs `go vet`, `go build` and
`go test` against `client-programs`, and finally the embedded-chart and
CLI-schema drift checks (`verify-installer-chart`, `verify-cli-schemas`).

`make ci-operator` is what the installer-operator workflow runs: it runs
`hack/lint-chart-versions.sh`, `go vet` and `go build` against
`installer/operator`, the generated-CRD/RBAC and DeepCopy drift checks
(`make manifests`, `make generate-installer-rbac`, and `make generate`
followed by a `git diff`), the envtest
suite (`make test`), and `golangci-lint` (`make lint`). Because the drift
checks regenerate files in place and fail on any difference, a failure may
leave generated files modified in your working tree — review the diff,
which is exactly what CI is flagging, and commit it if it is a legitimate
regeneration.

The CLI's Hugo renderer embeds theme files via `//go:embed` from
`client-programs/pkg/renderer/files/`, but that directory is `.gitignore`d
and populated at build time from the workshop base environment
(`workshop-images/base-environment/opt/eduk8s/etc/themes`). Without it
`go vet`/`build`/`test` fail with `pattern all:files/*: no matching files
found`. Both `make build-cli` and `make ci-cli` stage it automatically; if
you ever need it on its own, run:

```
make stage-renderer-files
```

The operator module is pinned to a specific Go version (see
`installer/operator/go.mod`). If you have more than one Go toolchain
installed, set `GOTOOLCHAIN` to that version (e.g.
`GOTOOLCHAIN=go1.26.0 make ci-operator`) or ensure the pinned toolchain is
the one on your `PATH`, otherwise some build steps may mix toolchains and
fail with a `compile: version "…" does not match go tool version "…"`
error. CI is unaffected because it provisions a single Go version via
`actions/setup-go`.

Cleaning up available storage space
-----------------------------------

Running successive builds of the container images will incrementally result in more and more storage space being consumed as all layers of builds will be cached even if no longer being used.

Commands such as `docker image prune` can prune images from the local docker build cache, but will not reclaim storage for unused image layers in the local docker image registry.

To clean up available storage space across the local docker image build cache, the local docker image registry, and also any local file system space used to to local source code builds you can run:

```
make prune-all
```

Note that this will run `docker system prune` rather than `docker image prune`, which will also result in unused docker networks and volumes being cleaned up.

Also note that this doesn't reclaim space used by the image cache of `containerd` on the Kubernetes cluster nodes. If you are doing a lot of work on Educates, especially changes to the workshop base images and you deploy workshops using many successive versions of the images, eventually you can run out of storage space due to the `containerd` image cache. In this case there isn't really anything simple you do can except for deleting the Kubernetes cluster and starting over.

Building docs.educates.dev locally
----------------------------------

If you're working on updates or additions to the project documentation served at [docs.educates.dev](https://docs.educates.dev), you might want to preview your changes locally before opening a PR. To build and preview the docs locally, you can run:

```
make build-project-docs
make open-project-docs
```

Sample workshops for testing
----------------------------

A set of sample workshops are provided for testing and demonstrating different features of the Educates platform. These are not intended for end user consumption but serve as minimal pre-canned workshops for verifying platform functionality.

For details on the available sample workshops and instructions on how to deploy them, see the [workshop samples README](../workshop-samples/README.md).
