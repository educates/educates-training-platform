(migrating-from-v3)=
Migrating from Educates v3
==========================

Educates v4 replaces the installation mechanism. v3 installed the platform as Carvel packages (driven by the `educates` CLI or a pre-installed `kapp-controller`); v4 installs a Helm chart containing a Kubernetes operator, driven by four custom resources — see [installation instructions](installation-instructions) for the new model.

Two facts shape the migration:

* **The Educates runtime is unchanged.** Workshop definitions, workshop content, published workshop OCI artifacts, and training portal configurations carry over as they are — the `training.educates.dev` custom resource APIs did not change in v4.
* **There is no in-place migration.** A v3 and a v4 installation cannot coexist on one cluster, and v4 does not upgrade a v3 install. You delete the v3 installation and install v4.

Migrating a cluster installation
--------------------------------

1. **Export your workshop resources.** Deleting the v3 installation removes the `training.educates.dev` CRDs and with them every `Workshop` and `TrainingPortal` resource. Save them first:

   ```shell
   kubectl get workshops -o yaml > workshops-backup.yaml
   kubectl get trainingportals -o yaml > trainingportals-backup.yaml
   ```

   If your definitions already live in files or Git (recommended), skip this.

2. **Delete the v3 installation**, using whichever mechanism installed it: `educates delete-platform` with your v3 CLI, or delete the `kapp-controller` `App`/`PackageInstall` resources if you installed via GitOps. Verify the educates namespaces and CRDs are gone before proceeding.

3. **Write a v4 configuration file** for your scenario (see the translation tables below and [configuration settings](configuration-settings)).

4. **Install v4** following [CLI-based installation](cli-based-installation) or [Helm-based installation](helm-based-installation).

5. **Re-apply your workshop resources.** Strip the exported resources of `status`, `metadata.uid`, `metadata.resourceVersion` and similar server-side fields (unnecessary if applying from your own files), then `kubectl apply` them. Training portals will recreate their workshop environments from scratch; any in-flight workshop sessions from the v3 install are not preserved.

Translating your v3 configuration
---------------------------------

The v3 `clusterInfrastructure.provider` value points at the v4 configuration kind to use:

```text
| v3 provider           | v4 configuration kind                                    |
|-----------------------|----------------------------------------------------------|
| kind                  | EducatesLocalConfig (migrated automatically — see below) |
| gke                   | EducatesGKEConfig                                        |
| eks                   | EducatesEKSConfig                                        |
| openshift             | EducatesInlineConfig                                     |
| generic               | EducatesInlineConfig                                     |
| minikube, vcluster    | EducatesConfig (escape hatch)                            |
| custom                | EducatesConfig (escape hatch)                            |
```

Common v3 values map to v4 fields as follows:

```text
| v3 values.yaml                          | v4 equivalent                                              |
|-----------------------------------------|------------------------------------------------------------|
| clusterIngress.domain                   | domain (GKE/EKS/Inline kinds) / ingress.domain (Local)     |
| clusterIngress.tlsCertificateRef        | wildcardCertificateSecret (Inline), a Secret in the        |
|                                         | operator namespace / local secrets add tls (Local)         |
| clusterIngress.caCertificateRef         | caCertificateSecret (Inline) / local secrets add ca (Local)|
| clusterIngress.protocol                 | ingress.insecure: true (Local, plain HTTP) /               |
|                                         | externalTLSTermination: true (GKE/EKS/Inline kinds)        |
| aws.region / aws.irsaRoles.*            | aws.region / aws.certManagerRoleARN /                      |
|                                         | aws.externalDNSRoleARN (EKS kind)                          |
| gcp.project / gcp.workloadIdentity.*    | gcp.project / gcp.certManagerServiceAccount /              |
|                                         | gcp.externalDNSServiceAccount (GKE kind)                   |
| clusterSecurity / policy engines        | policyEnforcement.clusterEngine / workshopEngine (Inline)  |
| imageRegistry (mirror)                  | imageRegistry.prefix (Inline / EducatesClusterConfig)      |
| imageVersions                           | imageVersions (same shape)                                 |
| imagePuller.enabled                     | imagePrePuller.enabled                                     |
| imagePuller.prePullImages               | imagePrePuller.images and includeDefaultImages, see below  |
| lookupService.enabled                   | lookupService (boolean toggle)                             |
```

Version 3 pre-pulled the training portal image and then each name in
``imagePuller.prePullImages``, which defaulted to ``base-environment``. Version
4 pre-pulls its default images, the training portal and the workshop base
environment, and then each entry of ``imagePrePuller.images``, an image's short
``name`` or a full ``image`` reference. To pre-pull the same images as before:

* When ``prePullImages`` includes ``base-environment``, or is not set, keep the
  default images and add each of its other names as a ``name`` entry.
* Otherwise, an empty list included, set ``includeDefaultImages`` to ``false``
  and list ``training-portal`` followed by its names as ``name`` entries.

The version 4 image inventory does not hold the names
``loftsh-kubernetes-v1.31``, ``loftsh-kubernetes-v1.32``, ``session-manager``,
``secrets-manager``, ``lookup-service``, ``node-ca-injector`` and
``pause-container``, and a ``name`` entry the inventory does not hold fails the
installation of the session manager. Leave these names out, or give the image's
full reference as an ``image`` entry. A ``loftsh-kubernetes-v1.31`` or
``loftsh-kubernetes-v1.32`` name that your ``imageVersions`` defines still
resolves, since an image override adds its name to the inventory. Drop an
``imageVersions`` entry named ``pause-container``: the pre-puller no longer
runs a pause container, and its own helper image is overridden by the name
``image-puller``. See [pre-pulling workshop images](pre-pulling-workshop-images)
for the ``imagePrePuller`` fields.

Version 3's ``imageRegistry`` replaced the registry host and namespace of the
Educates images, which it pulled from ``<host>/<namespace>/educates-<name>``.
Version 4's ``imageRegistry.prefix`` keeps each image's upstream repository
path under the prefix instead, as it does for the cluster services, so
``ghcr.io/educates/educates-session-manager`` is pulled from
``<prefix>/educates/educates-session-manager``. A version 3 mirror whose
namespace ends in ``educates`` is reached with the host and the rest of the
namespace as the prefix: the host alone for the namespace ``educates``, and
``registry.internal/mirror`` for the namespace ``mirror/educates`` on
``registry.internal``. Any other mirror needs the images copied into the
layout the [air-gapped installation](airgapped-installation) guide describes.

Anything not expressible in a narrow kind is reachable through `EducatesConfig`, which carries the four custom resource specs verbatim. Keep your v3 `values.yaml` open beside the [configuration settings](configuration-settings) reference while re-declaring; the JSON schemas give completion and validation in your editor as you go.

(laptop-installs-migrate-automatically)=
Laptop installs migrate automatically
-------------------------------------

For local kind-cluster users, the v4 CLI migrates your configuration on first use. When a v4 command needs the local config (`educates local cluster create`, `educates admin platform deploy --local-config`, ...) and finds a v3 `values.yaml` in the CLI data home with `clusterInfrastructure.provider` empty or `kind`, it:

* translates it to a v4 ``config.yaml`` (``EducatesLocalConfig``), carrying over
  the ingress domain, kind cluster settings (listen address, API server,
  subnets, volume mounts, registry mirrors), local DNS resolver settings, image
  version overrides, image pre-puller settings, website styling references,
  and image pull secret propagation;
* renames the original to `values.yaml.v3-backup`; and
* prints a notice naming both files, followed by a line for each setting it
  carried over in a changed form or dropped. There is no prompt and no flag:
  you run the same command you would have run on v3.

The image pre-puller settings are carried over as the mapping above describes,
so the pre-puller keeps its switch and its list of names. The migration drops
the seven version 3 names listed above that the version 4 image inventory does
not hold, keeping a ``loftsh-kubernetes-v1.31`` or ``loftsh-kubernetes-v1.32``
name only when your ``imageVersions`` defines it, and drops an
``imageVersions`` entry named ``pause-container``. Version 3 also pulled a name
its inventory did not hold, as ``<registry>/educates-<name>:<version>``. The
migration carries such a name over as a ``name`` entry, and since the version
4 inventory does not hold it either, the first deployment fails, the
``SessionManager`` reporting ``InstallFailed`` with a message naming the entry.
Replace that entry with the image's full reference as an ``image`` entry, or
remove it. A version 3 file without ``imagePuller`` leaves
``imagePrePuller`` unset, so the pre-puller is on with its default images. The
notice says what was carried over and what was dropped, with lines such as:

```
dropped imageVersions entry pause-container: the image pre-puller now runs its own image-puller helper, which a pause container image cannot replace
carried imagePuller over to imagePrePuller: enabled: true, images: jdk17-environment
dropped imagePuller.prePullImages names the v4 image inventory does not hold: session-manager; to pull one, add its full reference to imagePrePuller.images as an image entry
```

If the v3 file's provider is anything other than kind or empty, the CLI refuses with instructions instead: cloud and BYO configurations carry intent the CLI cannot infer (DNS, ACME, identity wiring), so they must be re-declared against a v4 kind as described above. The `values.yaml` is left untouched for reference.

One caveat concerns the local secrets cache. v3 cached its local CA as an `Opaque` Secret holding only the CA certificate as `ca.crt`, which v4 uses unchanged as a CA trust Secret. If the cache also holds the wildcard certificate for your ingress domain, as the v3 `mkcert` setup left it, v4 deploys serve that certificate with the `StaticCertificate` certificates provider, trust the cached CA certificate its chain verifies against, and install no cert-manager, so nothing needs to be registered again. The domain annotation v3 put on both Secrets is ignored: a wildcard certificate serves the domains of its `*.<domain>` DNS names. The migration prints a note about this. Without a cached wildcard certificate, the v3 CA certificate alone cannot sign, since v4 mints a CA for the ingress domain from a root CA whose key is cached on your machine. Generate a root CA with the following command, or register the root CA you used with v3 together with its key by adding `--cert` and `--key`:

```shell
educates local secrets add ca <name>
```

Command changes
---------------

The local-environment commands moved under grouped names:

```text
| v3 command                  | v4 command                       |
|-----------------------------|----------------------------------|
| educates create-cluster     | educates local cluster create    |
| educates delete-cluster     | educates local cluster delete    |
| educates deploy-platform    | educates admin platform deploy   |
| educates delete-platform    | educates admin platform delete   |
```

The workshop tooling (`educates publish-workshop`, `deploy-workshop`,
`browse-workshops`, ...) keeps its commands and options, but publishing a
workshop changed in ways which can need action:

* `spec.publish.files` in a workshop definition accepts only `directory`
  entries. The remote source types, such as `git`, `http` and `image`, were
  removed, so files fetched from a remote source need to be placed in the
  workshop directory before publishing. In its patterns, `[!x]` now negates a
  character class and `dir/**` also matches a file named `dir`. See
  [publishing of workshop content](publishing-of-workshop-content).
* The `IMGPKG_*` environment variables are no longer read. Registry
  credentials come from the `--registry-*` options, or from the Docker
  configuration that `docker login` writes and the credential helpers it
  names.
* Workshop images are published as OCI images, so the first publish gives a
  different image digest from a v3 publish of the same content.
  `.DS_Store` and `._*` files are left out, and a symbolic link or a
  hard-linked file in the published files fails the publish.

Removed in v4
-------------

* The Carvel/`kapp-controller` installation path. GitOps installs now point at the published Helm chart — see [Helm-based installation](helm-based-installation).
* Pre-canned provider configurations for `minikube` and `vcluster`. Equivalent installs are possible via `EducatesConfig` or `EducatesInlineConfig`.

Certificate-less, plain-HTTP installs are still supported. A v3 `clusterIngress.protocol: http` install maps to `ingress.insecure: true` on `EducatesLocalConfig`, which the laptop migration applies automatically. On other kinds, `externalTLSTermination` now installs no in-cluster certificate, and at the cluster-configuration level the `None` certificates provider serves the same role. See [secure HTTP connections](secure-http-connections).
