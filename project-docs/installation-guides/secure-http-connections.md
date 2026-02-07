(secure-http-connections)=
Secure HTTP Connections
=======================

When installing Educates into a Kubernetes cluster, one of the key decisions you will need to make is how secure HTTP connections (HTTPS) are handled. Depending on your network environment, the answer can range from straightforward to quite involved. This is especially common in corporate environments where SSL certificates, DNS, and proxy infrastructure may be managed by separate operations teams.

This guide describes the most common scenarios you are likely to encounter, along with guidance on how to configure Educates for each. The goal is to help you understand what options exist so that you can work with whatever constraints your environment imposes. For the detailed configuration syntax used in each case, refer to the [configuration settings](configuration-settings) documentation.

Wildcard DNS as a prerequisite
------------------------------

Regardless of which approach you use for handling secure connections, all scenarios require that a wildcard DNS entry be configured for the ingress domain used by Educates. This is typically achieved by creating a wildcard CNAME record in your DNS provider, pointing at the IP address of whatever sits in front of your Kubernetes cluster, whether that is the cluster's own ingress router, a separate proxy server, or a CDN edge network.

For example, if your ingress domain is ``workshops.example.com``, you would need a DNS entry for ``*.workshops.example.com`` that resolves to the appropriate IP address.

When using one of the opinionated installers for infrastructure providers such as AWS (EKS), this DNS configuration may be handled for you automatically through services like ``external-dns``. Similarly, CDN providers like Cloudflare can manage DNS on your behalf. In other environments, you will need to arrange for this DNS entry to be created, which may require coordinating with your network or operations team.

No secure ingress
-----------------

The simplest case is where you do not have access to a wildcard TLS certificate for the ingress domain and cannot generate one. In this situation, Educates will operate using plain HTTP connections only.

To configure this, you need only set the ingress domain.

```yaml
clusterIngress:
  domain: "workshops.example.com"
```

If you do not have your own custom domain name, it is technically possible to use a ``nip.io`` address mapped to the IP address of the inbound ingress router for the Kubernetes cluster. Because it will not be possible to obtain a TLS certificate for such a domain, you will not be able to use secure ingress when using a ``nip.io`` address.

This approach is suitable for local development or testing environments. It is not recommended for production use or any environment where workshop users will be accessing Educates over the public internet, as all traffic including any credentials used to access training portals will be transmitted unencrypted.

Direct TLS termination at the ingress router
---------------------------------------------

If you have a wildcard TLS certificate that matches your ingress domain, you can configure Educates to use it directly. The Kubernetes ingress controller will terminate TLS connections and Educates will handle HTTPS natively.

The TLS certificate can be supplied in one of two ways. The preferred method is to create a Kubernetes secret containing the certificate and reference it from the Educates configuration.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificateRef:
    namespace: "default"
    name: "workshops.example.com-tls"
```

Alternatively, the certificate can be provided inline within the configuration.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificate:
    tls.crt: |
      ...
    tls.key: |
      ...
```

When a TLS certificate is provided, Educates will automatically understand that the protocol for ingress connections is HTTPS. There is no need to explicitly set the protocol.

If the TLS certificate was signed by a certificate authority that is not publicly trusted, you will also need to supply the CA certificate. Refer to the [configuration settings](configuration-settings) documentation for details on how to do this.

External proxy forwarding HTTP to the cluster
----------------------------------------------

In some environments, a separate proxy server sits in front of the Kubernetes cluster. This proxy is accessible to end users and holds the wildcard TLS certificate used for public HTTPS connections. DNS for the wildcard domain is configured to point at this proxy rather than at the Kubernetes cluster directly.

The proxy terminates the public TLS connection and then forwards traffic to the Kubernetes cluster's ingress router using plain HTTP. From the perspective of the Kubernetes cluster, all incoming traffic is HTTP, yet the users accessing Educates through the proxy are using HTTPS.

To tell Educates that the public-facing URL should use HTTPS even though the cluster itself is receiving HTTP, set the protocol explicitly.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  protocol: "https"
```

No TLS certificate needs to be provided in the Educates configuration because TLS is being handled entirely by the external proxy.

This arrangement is recommended only when the communication between the proxy and the Kubernetes cluster occurs over a private network, as the traffic on that internal hop is unencrypted.

External proxy re-encrypting with a private certificate
--------------------------------------------------------

A variation on the previous scenario is where the external proxy terminates the public TLS connection but then re-encrypts traffic using a private TLS certificate before forwarding it to the Kubernetes cluster. This provides encryption on the internal hop between the proxy and the cluster, which may be required by security policy even on a private network.

In this case, Educates needs to be configured with the private TLS certificate so that the Kubernetes ingress controller can terminate the re-encrypted connection.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificateRef:
    namespace: "default"
    name: "workshops.example.com-tls"
```

The ingress domain should still be set to the wildcard domain that DNS routes to the external proxy, not to anything specific to the internal hop.

If the private TLS certificate is self-signed or signed by an internal certificate authority that is not publicly trusted, the external proxy will need to be configured to trust that certificate when making connections to the Kubernetes cluster. You may also need to supply the CA certificate to Educates so that internal components can validate connections. Refer to the [configuration settings](configuration-settings) documentation for details.

Using Cloudflare proxy with origin certificates
------------------------------------------------

Cloudflare can act as the external proxy in front of your Kubernetes cluster. When Cloudflare is proxying traffic for your domain, it automatically manages the public TLS certificate presented to end users. DNS is managed through Cloudflare, with the wildcard domain pointing at Cloudflare's edge network. Cloudflare then forwards requests to your cluster based on the origin IP address you configure.

To secure the connection between Cloudflare and your Kubernetes cluster, Cloudflare provides what it calls an origin certificate. This is a TLS certificate issued by Cloudflare's own certificate authority, intended specifically for encrypting traffic on the hop between Cloudflare's edge and your origin server. The origin certificate can be generated and downloaded from the Cloudflare dashboard.

Because the origin certificate is signed by Cloudflare's CA rather than a publicly trusted authority, it will only be trusted by Cloudflare itself when making connections to your cluster. This is fine for this purpose since Cloudflare is the only entity connecting to your origin.

To configure Educates with a Cloudflare origin certificate, supply the certificate and optionally the Cloudflare origin CA certificate.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificateRef:
    namespace: "default"
    name: "workshops.example.com-tls"
  caCertificateRef:
    namespace: "default"
    name: "cloudflare-origin-ca"
```

When using Cloudflare proxy, the Cloudflare SSL mode should be set to "Full" or "Full (Strict)" so that Cloudflare connects to your origin over HTTPS using the origin certificate. The "Flexible" mode would cause Cloudflare to connect over plain HTTP, making the origin certificate unnecessary.

If the connection between Cloudflare and your Kubernetes cluster traverses a public network, you should consider configuring any inbound router or firewall in front of the cluster to only accept connections from Cloudflare's published IP address ranges. Cloudflare publishes the list of IP addresses used by its proxy network, and restricting inbound traffic to only those addresses helps ensure that external traffic to your cluster can only arrive via Cloudflare, where the public TLS certificate and any other edge protections are applied.

Using Cloudflare Tunnel
------------------------

Cloudflare Tunnel provides an alternative to the traditional proxy approach. Rather than exposing your Kubernetes cluster's ingress router to the internet, a ``cloudflared`` daemon running within or alongside your cluster creates an outbound connection to Cloudflare's edge network. Traffic from end users arrives at Cloudflare over HTTPS, travels through the tunnel, and is delivered to the Kubernetes cluster's ingress router as plain HTTP.

Because the tunnel is an outbound connection from your cluster, there is no need to expose any inbound ports or configure TLS certificates on the cluster side. Cloudflare manages the public TLS certificate and DNS automatically.

To configure Educates for use with Cloudflare Tunnel, set the protocol to HTTPS without providing a TLS certificate.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  protocol: "https"
```

This tells Educates that the public URL uses HTTPS so that all generated URLs will have the correct scheme, even though the Kubernetes cluster itself is receiving HTTP traffic from the tunnel.

Using AWS with ALB and ACM
--------------------------

When running Educates on Amazon EKS, the AWS infrastructure can manage TLS termination and DNS for you. An Application Load Balancer or Network Load Balancer sits in front of the Kubernetes cluster and terminates public TLS connections using a certificate managed by AWS Certificate Manager (ACM). The AWS Load Balancer Controller, running within the cluster, automatically configures the load balancer based on annotations on Kubernetes resources.

If you are using the Educates opinionated installer for EKS, much of this is handled automatically. The installer configures ``external-dns`` to manage the wildcard DNS entry in Route 53 and ``cert-manager`` to obtain certificates from Let's Encrypt. In this case you may not need to do anything beyond providing the required IAM role ARNs and ingress domain. Refer to the [infrastructure providers](infrastructure-providers) documentation for the specific EKS configuration.

For environments where you are managing the AWS load balancer configuration yourself, the typical pattern is that the ALB terminates TLS using an ACM certificate and forwards traffic to the Kubernetes ingress controller as plain HTTP. The load balancer adds an ``X-Forwarded-Proto`` header to tell backend services what protocol the original client used, but the actual connection between the load balancer and the cluster is unencrypted.

In this case the Educates configuration would set the protocol to HTTPS without providing a TLS certificate.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  protocol: "https"
```

ACM certificates are tightly integrated with AWS services. Historically the private key for an ACM certificate could not be exported for use outside of AWS, though AWS has since added export support for certificates created with that option enabled. For the typical EKS deployment pattern this does not matter, as the certificate is used directly by the load balancer and never needs to be provided to Educates.

Using cert-manager for certificate generation
----------------------------------------------

Rather than obtaining and managing TLS certificates yourself, you can use [cert-manager](https://cert-manager.io/) to automate the generation and renewal of certificates within the Kubernetes cluster. cert-manager is a Kubernetes operator that integrates with certificate authorities to issue and manage TLS certificates as Kubernetes secrets.

With cert-manager you can generate certificates from your own internal certificate authority, or use a public certificate authority such as [Let's Encrypt](https://letsencrypt.org/). If using your own CA, you would configure a cert-manager ``Issuer`` or ``ClusterIssuer`` that references your CA certificate and key, and cert-manager will issue certificates signed by that CA on demand.

When using Let's Encrypt, because Educates requires a wildcard TLS certificate to cover all hostnames under the ingress domain, you will need to use the DNS-01 challenge type. Let's Encrypt does not support issuing wildcard certificates using the HTTP-01 challenge. The DNS-01 challenge works by having cert-manager create a temporary TXT record in your DNS zone to prove domain ownership. This means cert-manager must be configured with credentials to access your DNS provider, whether that is Route 53, Cloud DNS, Cloudflare DNS, or another supported provider. Refer to the cert-manager documentation for the list of supported DNS providers and how to configure each.

Once cert-manager is issuing certificates, the generated TLS certificate will be stored as a Kubernetes secret in the cluster. Because cert-manager manages the secret directly, you must use the secret reference form of the Educates configuration rather than providing the certificate inline. This is because the certificate will be renewed automatically by cert-manager, updating the secret in place, and an inline copy in the Educates configuration would become stale.

For example, if you have configured a ``ClusterIssuer`` named ``letsencrypt-prod`` and created a cert-manager ``Certificate`` resource that stores the resulting certificate in a secret named ``workshops.example.com-tls`` in the ``default`` namespace, the Educates configuration would reference that secret.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificateRef:
    namespace: "default"
    name: "workshops.example.com-tls"
```

The corresponding cert-manager ``Certificate`` resource would look something like the following.

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: workshops.example.com
  namespace: default
spec:
  secretName: workshops.example.com-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - "*.workshops.example.com"
```

Note that when using the Educates opinionated installer for infrastructure providers such as EKS or GKE, cert-manager is installed and configured automatically, including the creation of a ``ClusterIssuer`` for Let's Encrypt. In those cases you do not need to set up cert-manager yourself. The information here is for environments where you are managing the cluster configuration independently and want to use cert-manager to handle certificate generation.

HTTP-to-HTTPS redirection and workshop services
------------------------------------------------

When using an external proxy or CDN in front of the Kubernetes cluster, there is a subtle issue to be aware of that can affect workshops which create their own Kubernetes ingress resources for plain HTTP services.

Many proxies and CDN providers offer the ability to force HTTP-to-HTTPS redirection at their edge. For example, Cloudflare has an "Always Use HTTPS" setting and AWS ALB supports listener rules that redirect HTTP requests to HTTPS. When this is enabled, any HTTP request from a user is redirected to HTTPS by the proxy before the request ever reaches the Kubernetes cluster.

This works well for the Educates training portal and other Educates services, which are designed to be accessed over HTTPS. However, individual workshops may deploy their own applications and create ingress resources for services that only handle HTTP. When the external proxy forces all traffic to HTTPS and then forwards requests to the cluster, it typically adds headers such as ``X-Forwarded-Proto: https`` to indicate the original client protocol. Depending on how the Kubernetes ingress controller is configured to handle these headers, and whether it has its own HTTP-to-HTTPS redirect logic enabled, this can lead to unexpected behaviour such as redirect loops or failed connections for these workshop services.

The details of how this manifests depend on which ingress controller is in use and how it is configured, as different ingress controllers handle forwarded protocol headers and redirect logic differently. The interaction between the external proxy's redirect behaviour and the ingress controller's own redirect settings can be difficult to debug when problems arise.

If your environment uses an external proxy, the recommended approach is to configure the proxy so that it does not force HTTP-to-HTTPS redirection. Instead, allow HTTP requests to be forwarded through to the Kubernetes cluster and let the applications running within the cluster handle redirection to HTTPS if and when they need to. This avoids conflicts between the proxy and the ingress controller and ensures that workshop services which only support HTTP can function correctly.

If forced HTTP-to-HTTPS redirection at the proxy cannot be avoided, you should verify that workshops which create their own ingress resources for HTTP services still work correctly in your environment. Some workshops may need to be customised to account for the redirect behaviour, or the ingress controller may need its handling of forwarded protocol headers and redirect logic reviewed to ensure it does not conflict with the external proxy.

This is an area where testing with your specific combination of proxy, ingress controller, and workshop is important, as the behaviour can vary depending on the exact configuration of each component.

Summary
-------

The following table provides a quick reference for the different scenarios described above.

```text
| Scenario                          | TLS Certificate in Educates | Protocol Setting |
|-----------------------------------|-----------------------------|------------------|
| No secure ingress (HTTP only)     | No                          | (default)        |
| Direct TLS at ingress router      | Yes                         | (automatic)      |
| External proxy forwarding HTTP    | No                          | "https"          |
| External proxy re-encrypting      | Yes (private certificate)   | (automatic)      |
| Cloudflare proxy with origin cert | Yes (origin certificate)    | (automatic)      |
| Cloudflare Tunnel                 | No                          | "https"          |
| AWS ALB with ACM                  | No                          | "https"          |
| cert-manager with own CA          | Yes (via secret reference)  | (automatic)      |
| cert-manager with Let's Encrypt   | Yes (via secret reference)  | (automatic)      |
```

In all cases, the ``clusterIngress.domain`` must be set to the wildcard domain for which DNS has been configured.

For the detailed syntax of all ingress-related configuration settings, including how to supply TLS certificates, CA certificates, and ingress class overrides, refer to the [configuration settings](configuration-settings) documentation.
