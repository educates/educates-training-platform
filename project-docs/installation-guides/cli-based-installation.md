CLI Based Installation
======================

The instructions below pertain to installing Educates into an existing Kubernetes cluster using the Educates CLI. The CLI installs the operator Helm chart (a copy is embedded in the CLI binary), applies the four platform custom resources derived from your configuration file, and waits for each to report ready.

Creating a configuration file
-----------------------------

Write a configuration file for your scenario using one of the `cli.educates.dev/v1alpha1` kinds. For example, for GKE:

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesGKEConfig
gcp:
  project: my-gcp-project
domain: workshops.example.com
acme:
  email: admin@example.com
```

See [configuration settings](configuration-settings) for all configuration kinds and their fields, and [infrastructure providers](infrastructure-providers) for provider-specific prerequisites (cloud IAM, DNS zones).

Previewing the installation
---------------------------

To see exactly what would be installed without touching the cluster:

```shell
educates admin platform render --config config.yaml
```

This prints the operator chart values followed by the four custom resources, in deploy order, as one YAML stream whose first document is the chart values. The output is deterministic for a given config file and CLI version, which makes it suitable for diffing, review, and committing to a Git repository for [GitOps-driven installation](helm-based-installation). To commit them, write the chart values and the custom resources to files of their own, for `helm install -f` and `kubectl apply -f`:

```shell
educates admin platform render --config config.yaml \
  --values-file values.yaml --resources-file resources.yaml
```

Standard output keeps the part not written to a file, so either part can also be piped to its command with `-f -`.

Deploying the platform
----------------------

```shell
educates admin platform deploy --config config.yaml
```

This performs, in order:

1. `helm upgrade --install` of the `educates-installer` operator chart (CRDs included), then wait for the operator Deployment to roll out, so the operator the chart names is the one that reconciles the resources applied next.
2. Apply `EducatesClusterConfig`, wait for `Ready=True` — in Managed mode this is where the operator installs cert-manager, Contour, and the other cluster services.
3. Apply `SecretsManager`, wait for `Ready=True`.
4. Apply `LookupService` (if enabled) and `SessionManager`, wait for both.

Each wait accepts `Ready=True` only once the operator has reconciled the configuration the command applied: the condition's `observedGeneration` must have reached the resource's generation. On a redeploy that changes a resource, the `Ready` condition from before the change does not count. A redeploy that leaves a resource unchanged, such as one with a newer release of the CLI and the same configuration, keeps its generation, so the command accepts the resource's existing `Ready` condition while the new operator may still be upgrading the component.

Progress for each step is reported as it happens. If the operator does not roll out, or a step does not become ready, within the per-resource timeout (configurable with `--timeout`), the command fails with the failing resource's status conditions; inspect them with `kubectl describe educatesclusterconfig cluster` or the equivalent for the other kinds.

Use `--verbose` to also stream Helm SDK debug output when troubleshooting.

Kubeconfig and context
----------------------

By default the CLI uses the standard kubeconfig (`$KUBECONFIG` or `$HOME/.kube/config`) and its current context. Use `--kubeconfig` to point at an alternate file and `--context` to select a context:

```shell
educates admin platform deploy --config config.yaml --kubeconfig kubeconfig.yaml --context educates-cluster
```

Updating configuration
----------------------

To amend the configuration of an existing installation, edit your configuration file and re-run the same deploy command:

```shell
educates admin platform deploy --config config.yaml
```

The Helm release is upgraded and the custom resources re-applied; the operator reconciles the differences in place. Note that configuration changes will not necessarily affect training portals or workshop environments which have already been created, and may only affect those created after that point.

How each cluster service is provided cannot be changed on an existing installation: the certificates provider, the ingress controller provider, the DNS provider and the policy enforcement engines and Kyverno provider are fixed when Educates is installed. A deploy that would change one of them, such as one that sets or clears `ingress.insecure` on the local configuration, fails with a message naming the setting that cannot change. To switch, delete the installation with `educates admin platform delete` and deploy again, or install into a new cluster. Settings within a bundled service, such as the options of the bundled external-dns, can still be changed by a deploy.

Deleting the installation
-------------------------

```shell
educates admin platform delete
```

This is the reverse of deploy: it deletes `SessionManager`, `LookupService`, `SecretsManager`, and `EducatesClusterConfig` in order, waiting for each to finish draining (the `EducatesClusterConfig` finalizer removes the operator-installed cluster services in reverse install order), and finally uninstalls the operator chart. No configuration file is needed — the resources are always the four singletons named `cluster`.

Each cluster service is removed only when Educates installed it: cert-manager when the certificates provider is `BundledCertManager`, Contour when the ingress controller provider is `BundledContour`, external-dns when the DNS provider is `BundledExternalDNS`, and Kyverno when the Kyverno provider is `Bundled` and a policy engine is `Kyverno`. A cluster service you installed yourself is left in place, even when it uses the release name and namespace of the bundled one, such as `cert-manager` or `contour`.

A confirmation prompt lists everything about to be deleted; pass `--yes` to skip it (required in CI and other non-interactive shells).

By default the four CRDs and the operator and `educates-secrets` namespaces are left in place so a subsequent deploy can reuse them. To remove those too:

```shell
educates admin platform delete --yes --purge
```

Neither form removes the custom resource definitions of the runtime components, such as `Workshop`, `TrainingPortal` and `SecretCopier`, which stay in place together with every resource of their kinds.
