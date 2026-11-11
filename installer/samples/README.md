# EducatesClusterConfig samples

Reference `EducatesClusterConfig` resources for the three verified
Managed-mode scenarios, plus full-field references covering the entire
supported v1alpha1 spec surface.

| File | Scenario | Certificates | DNS | Policy |
|---|---|---|---|---|
| `01-local-kind-customca.yaml` | Local kind / developer machine (Managed) | BundledCertManager + CustomCA | — | — |
| `02-gke-clouddns-acme.yaml` | GKE production with Workload Identity (Managed) | BundledCertManager + ACME-DNS01 (CloudDNS) | BundledExternalDNS (CloudDNS) | Bundled Kyverno |
| `03-eks-route53-acme.yaml` | EKS production with IRSA (Managed) | BundledCertManager + ACME-DNS01 (Route53) | BundledExternalDNS (Route53) | Bundled Kyverno |
| `04-openshift-inline.yaml` | OpenShift / BYO cluster services (Inline) | pre-existing wildcard TLS Secret | — (cluster-managed) | OpenShiftSCC |
| `05-managed-full.yaml` | Field reference: every supported Managed-mode field in one CR (GKE-flavoured) | BundledCertManager + ACME-DNS01 (CloudDNS) | BundledExternalDNS (CloudDNS) | Bundled Kyverno |
| `06-inline-full.yaml` | Field reference: every Inline-mode field in one CR (generic BYO NGINX + cert-manager) | pre-existing wildcard TLS Secret + CA + ClusterIssuer | — (cluster-managed) | Kyverno (pre-existing) |

The `-full` samples are field references, not starting points: each
populates every field the v1alpha1 operator supports and lists the
reserved-but-rejected surface in its comment header. For real installs
start from the scenario samples and add only what you need.

Platform-component CRs (apply *after* `EducatesClusterConfig` is Ready):

| File | Component |
|---|---|
| `secretsmanager.yaml` | SecretsManager — installs the secrets-manager runtime |
| `lookupservice.yaml` | LookupService — installs the lookup-service runtime (prefix + cluster domain → full hostname) |
| `sessionmanager.yaml` | SessionManager — installs the session-manager runtime (requires SecretsManager to be Ready first) |
| `secretsmanager-full.yaml` | SecretsManager field reference — every spec field populated |
| `lookupservice-full.yaml` | LookupService field reference — every spec field populated |
| `sessionmanager-full.yaml` | SessionManager field reference — every supported spec field populated; rejected blocks (`defaultAccessCredentials`, `registryMirrors`) kept as comments |

Apply order:

```bash
helm install educates-installer ./installer/charts/educates-installer \
  --namespace educates-installer --create-namespace
kubectl apply -f installer/samples/<scenario>.yaml
kubectl apply -f installer/samples/secretsmanager.yaml
kubectl apply -f installer/samples/lookupservice.yaml      # optional
kubectl apply -f installer/samples/sessionmanager.yaml
```

Each file's comment header lists the prerequisites (Secrets to create,
IAM/Workload Identity bindings to set up before applying the CR).

## Deletion order

Delete in **reverse** order:

```bash
kubectl delete sessionmanager cluster
kubectl delete lookupservice cluster      # if applied
kubectl delete secretsmanager cluster
kubectl delete educatesclusterconfig cluster
```

Deleting `EducatesClusterConfig` first is safe but blocks: in either mode
its finalizer refuses to finish while any platform CR still exists,
reporting `Ready=False reason=PlatformCRsPresent` and naming the
offenders, and requeues until you remove them. In Managed mode that keeps
the cluster services (cert-manager, contour, kyverno, external-dns) in
place, so the platform-component finalizers don't fail to clean up
resources whose CRDs would otherwise already be gone. In both modes it
keeps the Secrets the config owns, `educates-ingress-ca` and the image
pull Secret copies in `educates`, which the running components use.
Following the reverse order above avoids the wait.
