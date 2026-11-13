CLI JSON Schemas
================

The `educates` CLI config kinds (`cli.educates.dev/v1alpha1`) each have a
JSON schema at
[client-programs/pkg/config/v1alpha1/schemas](../client-programs/pkg/config/v1alpha1/schemas).
The schemas are embedded in the CLI (`go:embed`) and drive command-time
validation, `local config set` path checks, and editor support.

* `EducatesLocalConfig`, `EducatesGKEConfig`, `EducatesEKSConfig` and
  `EducatesInlineConfig` are maintained by hand alongside their Go types.
* `EducatesLocalMirrors` is maintained by hand too. It is not a cluster
  config kind: it is the data home's `mirrors.yaml`, which declares the
  registry mirrors every local cluster shares, so it stays out of
  `EducatesAnyConfig` and has its own SchemaStore entry.
* `EducatesConfig` is generated from the operator CRD OpenAPI schemas —
  regenerate with `make generate-cli-schemas` (CI fails on drift).
* `EducatesAnyConfig` is a kind-discriminated umbrella (`oneOf` over the
  five kinds) used only for filename-based editor matching (SchemaStore).
  It is not embedded in the CLI. **When a new config kind ships, add its
  `$ref` here.**

Publishing
----------

The `publish-schemas` job in the release workflow deploys the schemas to
this repository's GitHub Pages site on every final release tag, at paths
matching the `$id` baked into each schema. Prerelease tags (alpha, beta
and rc) skip the job, so the published schemas only change with a final
release:

```
https://schemas.educates.dev/cli/v1alpha1/<Kind>.json
```

Forks deploy to `https://<owner>.github.io/educates-training-platform/...`
(no custom domain); the job is `continue-on-error` so forks that never
enabled Pages don't break their releases.

One-time setup (upstream)
-------------------------

1. Repository Settings → Pages → Source: **GitHub Actions**.
2. Repository Settings → Pages → Custom domain: `schemas.educates.dev`
   (GitHub provisions the TLS certificate).
3. DNS: `CNAME schemas.educates.dev → educates.github.io.`

Editor discovery
----------------

Two mechanisms:

* **Modeline** — `educates local config init` writes a
  `# yaml-language-server: $schema=…/EducatesLocalConfig.json` header, so
  any editor with a YAML language server validates the file immediately.
  Hand-written scenario-kind files can carry the same modeline with their
  kind's URL. The v3 migration writes `mirrors.yaml` with the
  `…/EducatesLocalMirrors.json` modeline, and a hand-written one can carry
  it too.
* **SchemaStore** — filename-based matching for files without modelines.
  Registration is a one-time PR against
  [SchemaStore/schemastore](https://github.com/SchemaStore/schemastore)
  adding the following to `src/api/json/catalog.json` (file the PR after
  the first release publishes the schemas, so the URLs resolve):

```json
{
  "name": "Educates CLI config",
  "description": "Educates training platform CLI configuration (cli.educates.dev/v1alpha1)",
  "fileMatch": ["**/educates/config.yaml", "**/educates/clusters/*/config.yaml", "*.educates.yaml"],
  "url": "https://schemas.educates.dev/cli/v1alpha1/EducatesAnyConfig.json"
},
{
  "name": "Educates CLI registry mirrors",
  "description": "Registry mirrors shared by every Educates local cluster (cli.educates.dev/v1alpha1)",
  "fileMatch": ["**/educates/mirrors.yaml"],
  "url": "https://schemas.educates.dev/cli/v1alpha1/EducatesLocalMirrors.json"
}
```

The `**/educates/config.yaml` pattern matches the default local cluster's
config in the CLI data home (`$XDG_DATA_HOME/educates/config.yaml`), and
`**/educates/clusters/*/config.yaml` a named local cluster's;
`*.educates.yaml` is the documented naming convention for scenario-kind
files kept in user repositories. `**/educates/mirrors.yaml` matches the
data home's registry mirrors file. SchemaStore requires a positive and negative test file
under `src/test/` — use a minimal `EducatesLocalConfig` (apiVersion +
kind) as the positive case.
