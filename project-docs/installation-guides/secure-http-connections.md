(secure-http-connections)=
Secure HTTP Connections
=======================

When installing Educates into a Kubernetes cluster, one of the key decisions you will need to make is how secure HTTP connections (HTTPS) are handled. Depending on your network environment, the answer can range from straightforward to quite involved. This is especially common in corporate environments where SSL certificates, DNS, and proxy infrastructure may be managed by separate operations teams.

This guide describes the most common scenarios along with how each maps onto the Educates configuration. For the configuration file syntax, refer to the [configuration settings](configuration-settings) documentation.

Wildcard DNS as a prerequisite
------------------------------

Regardless of which approach you use for handling secure connections, all scenarios require that a wildcard DNS entry be configured for the ingress domain used by Educates. This is typically achieved by creating a wildcard CNAME record in your DNS provider, pointing at the IP address of whatever sits in front of your Kubernetes cluster, whether that is the cluster's own ingress router, a separate proxy server, or a CDN edge network.

For example, if your ingress domain is ``workshops.example.com``, you would need a DNS entry for ``*.workshops.example.com`` that resolves to the appropriate IP address.

When using the GKE or EKS configuration kinds, this DNS configuration is handled for you automatically through ``external-dns``. In other environments, you will need to arrange for this DNS entry to be created, which may require coordinating with your network or operations team.

How certificates are configured in v4
-------------------------------------

The `EducatesClusterConfig` resource always carries a certificates configuration; how the wildcard certificate comes to exist depends on the provider you select:

* **ACME, fully managed** (`BundledCertManager` with an ACME issuer) — the operator installs cert-manager and obtains a wildcard certificate from [Let's Encrypt](https://letsencrypt.org/) using a DNS01 challenge against your cloud DNS zone. This is what the `EducatesGKEConfig` (CloudDNS) and `EducatesEKSConfig` (Route53) kinds configure, and renewal is automatic. Wildcard certificates require the DNS01 challenge — Let's Encrypt does not issue wildcards via HTTP01 — which is why these scenarios need DNS provider credentials (Workload Identity / IRSA).
* **Custom CA, fully managed** (`BundledCertManager` with a CustomCA issuer) — the operator installs cert-manager and issues the wildcard certificate from a CA you supply as a Secret. This is what a local laptop install with a custom ingress domain uses: `educates local secrets add ca <name> --domain <domain>` generates a self-signed CA, and the deploy pushes it into the cluster. A local install with no ingress domain set instead defaults to the `None` provider below.
* **Your cert-manager** (`ExternalCertManager`) — cert-manager already runs in your cluster; you point Educates at your `ClusterIssuer` and it requests the wildcard certificate from it.
* **Static certificate** (`StaticCertificate`, or Inline mode's `wildcardCertificateSecret`) — you already have a wildcard certificate (bought, issued by a corporate CA, a Cloudflare origin certificate, or generated with `certbot`). Store it as a `kubernetes.io/tls` Secret in the operator's namespace and reference it by name. If the certificate is signed by a CA that is not publicly trusted, also supply the CA certificate Secret (`caCertificateRef` / `caCertificateSecret`), with the CA certificate under the `ca.crt` key, so workshop components can validate connections. Renewals are your responsibility — update the Secret in place.
* **No in-cluster TLS** (`None`) — the operator installs no cert-manager and issues no certificate, and Educates serves the cluster over plain HTTP. Select this with `ingress.insecure` on the local configuration for a laptop cluster, or with the `None` certificates provider directly. Set `ingress.protocol: https` alongside the `None` provider when TLS is terminated outside the cluster (see External proxies and CDNs below), so generated URLs use the right scheme even though the cluster presents no certificate.

In all cases other than the `None` provider, TLS is terminated by the cluster's ingress controller, and Educates handles HTTPS natively. With the `None` provider the cluster serves plain HTTP, either for a local insecure install or behind an external proxy that terminates TLS for it.

External proxies and CDNs
-------------------------

In some environments, a separate proxy server or CDN (Cloudflare, an AWS ALB with an ACM certificate, Cloudflare Tunnel) sits in front of the Kubernetes cluster, terminates the public TLS connection, and forwards traffic inward.

If the proxy **re-encrypts** traffic toward the cluster using a private certificate (for example a Cloudflare origin certificate, with the Cloudflare SSL mode set to "Full"), this is just the static-certificate scenario from the cluster's point of view: supply the private certificate (and its CA) as the static wildcard certificate.

If the proxy forwards **plain HTTP** to the cluster (Cloudflare "Flexible", Cloudflare Tunnel, the typical ALB+ACM listener), the cluster itself terminates no public TLS, but Educates must still generate `https://` URLs for the public-facing domain. Assert this with `externalTLSTermination: true` in the `EducatesGKEConfig`, `EducatesEKSConfig` or `EducatesInlineConfig` configuration kinds. Underneath, this selects the `None` certificates provider with `ingress.protocol: https`, so the operator issues no in-cluster certificate while the generated URLs stay `https`. The same combination is reachable directly via `EducatesConfig` or when applying the custom resources yourself.

Because the `None` provider issues no certificate, the GKE and EKS kinds no longer require `acme` settings, and Inline mode no longer requires a `wildcardCertificateSecret`, when `externalTLSTermination` is set.

When fronting the cluster with a proxy that traverses public networks, restrict inbound traffic to the proxy's published IP ranges so traffic cannot bypass the proxy's TLS and protections.

HTTP-to-HTTPS redirection and workshop services
------------------------------------------------

Many proxies and CDN providers offer the ability to force HTTP-to-HTTPS redirection at their edge (Cloudflare's "Always Use HTTPS", ALB redirect listener rules). This works well for the Educates training portal and other Educates services, which are designed to be accessed over HTTPS. However, individual workshops may deploy their own applications and create ingress resources for services that only handle HTTP. When the external proxy forces all traffic to HTTPS and forwards requests with headers such as ``X-Forwarded-Proto: https``, the interaction with the ingress controller's own redirect logic can lead to unexpected behaviour such as redirect loops or failed connections for these workshop services.

If your environment uses an external proxy, the recommended approach is to configure the proxy so that it does not force HTTP-to-HTTPS redirection — let applications within the cluster handle redirection themselves. If forced redirection cannot be avoided, verify that workshops which create their own ingress resources for HTTP services still work correctly in your environment; behaviour varies with the exact proxy and ingress controller combination, so test with your specific stack.

Summary
-------

```text
| Scenario                          | v4 configuration                                          |
|-----------------------------------|-----------------------------------------------------------|
| Managed ACME (Let's Encrypt)      | EducatesGKEConfig / EducatesEKSConfig (BundledCertManager) |
| Managed with your own CA          | BundledCertManager + CustomCA (local: secrets add ca)     |
| Existing cert-manager in cluster  | ExternalCertManager + your ClusterIssuer                  |
| Wildcard certificate in hand      | StaticCertificate / Inline wildcardCertificateSecret      |
| Proxy re-encrypting to cluster    | StaticCertificate with the private certificate            |
| Proxy forwarding plain HTTP       | externalTLSTermination: true (None provider, protocol     |
|                                   | https; no in-cluster certificate issued)                  |
| Plain HTTP, no TLS at all         | ingress.insecure: true (Local) / None provider, protocol  |
|                                   | http                                                       |
```

In all cases, the ingress domain must be set to the wildcard domain for which DNS has been configured.
