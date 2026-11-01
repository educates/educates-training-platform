(infrastructure-providers)=
Infrastructure Providers
========================

Educates provides purpose-built configuration kinds for common infrastructure providers, and an Inline mode for clusters whose ingress and certificate management already exist. The provider-specific prerequisites and configuration are described below; the field-by-field reference is in [configuration settings](configuration-settings).

Installation to Amazon EKS
--------------------------

Use the `EducatesEKSConfig` kind. The operator installs the Educates training platform, Contour as the ingress controller (exposed via a LoadBalancer service), Kyverno for cluster and workshop security policy enforcement, [cert-manager](https://cert-manager.io/) issuing a wildcard certificate from [Let's Encrypt](https://letsencrypt.org) via a Route53 DNS01 challenge, and [external-dns](https://github.com/kubernetes-sigs/external-dns) maintaining the wildcard DNS record in your Route53 hosted zone.

Prerequisites you must create up front:

* A Route53 hosted zone containing your wildcard ingress domain.
* Two IAM roles configured for [IAM Roles for Service Accounts (IRSA)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html): one for [cert-manager](https://cert-manager.io/docs/configuration/acme/dns01/route53/#eks-iam-role-for-service-accounts-irsa) and one for [external-dns](https://github.com/kubernetes-sigs/external-dns/blob/master/docs/tutorials/aws.md#iam-roles-for-service-accounts), both granting access to the hosted zone. By default the roles are expected at `arn:aws:iam::{accountId}:role/educates-cert-manager` and `.../educates-external-dns`; override with `aws.certManagerRoleARN` / `aws.externalDNSRoleARN` if yours are named differently.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesEKSConfig
aws:
  accountId: "123456789012"
  region: eu-west-1
  route53HostedZoneId: Z0123456789ABCDEF
domain: educates.example.com
acme:
  email: admin@example.com
```

Then `educates admin platform deploy --config config.yaml` against the EKS cluster's context.

Installation to Google GKE
--------------------------

Use the `EducatesGKEConfig` kind. The installed stack is the same as for EKS, with certificates and DNS handled through Google CloudDNS and authentication through [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity).

Prerequisites you must create up front:

* A CloudDNS zone containing your wildcard ingress domain.
* Two Google service accounts bound for Workload Identity: one for [cert-manager](https://cert-manager.io/docs/configuration/acme/dns01/google/#gke-workload-identity) and one for [external-dns](https://github.com/kubernetes-sigs/external-dns/blob/master/docs/tutorials/gke.md), both with DNS admin rights on the zone. By default they are expected at `cert-manager@{project}.iam.gserviceaccount.com` and `external-dns@{project}.iam.gserviceaccount.com`; override with `gcp.certManagerServiceAccount` / `gcp.externalDNSServiceAccount`.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesGKEConfig
gcp:
  project: my-gcp-project
domain: educates.example.com
acme:
  email: admin@example.com
```

Installation to local Kind cluster
----------------------------------

For laptop use, prefer `educates local cluster create`, which creates the kind cluster, local image registry and Educates install in one command — see the [quick start guide](quick-start-guide) and [local environment](local-environment). The configuration kind behind it is `EducatesLocalConfig`.

To install Educates onto a kind cluster you created yourself, run `educates local cluster create --cluster-only` first (kind cluster + registry, no platform), or use `educates admin platform deploy` against any existing kind cluster with an `EducatesLocalConfig` file.

Installation to OpenShift
-------------------------

OpenShift clusters come with their own ingress router and security model, so they are installed in Inline mode using the `EducatesInlineConfig` kind: you declare the existing IngressClass, a wildcard TLS certificate Secret, and OpenShift's security context constraints as the cluster policy engine.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesInlineConfig
domain: educates.apps.example.com
ingressClassName: openshift-default
wildcardCertificateSecret: wildcard-tls
policyEnforcement:
  clusterEngine: OpenShiftSCC
  workshopEngine: Kyverno
```

Workshop-level policy enforcement still uses Kyverno, which you must install yourself in Inline mode. The operator checks that it is installed, and the installation does not become ready until it is. Alternatively set `workshopEngine: None`, which is only acceptable for trusted users (see [cluster requirements](cluster-requirements)).

Other Kubernetes clusters
-------------------------

Any conformant Kubernetes cluster with an existing ingress controller can be targeted the same way with `EducatesInlineConfig`: provide the IngressClass name, a wildcard TLS Secret for your domain, and your policy engine choices.

For a fresh, dedicated cluster on an unlisted provider where you want Educates to install the cluster services itself (Managed mode with choices the narrow kinds don't expose), use the `EducatesConfig` escape hatch and author the `EducatesClusterConfig` spec directly — see [configuration settings](configuration-settings) and the [sample scenarios](https://github.com/educates/educates-training-platform/tree/develop/installer/samples).
