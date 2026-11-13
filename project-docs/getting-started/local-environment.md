(local-environment)=
Local Environment
=================

For local development of workshop content the Educates command line tool provides a means for creating a local Kubernetes cluster using Kind, running Educates. This includes an image registry for storing workshop content files and custom workshop base images. The out of the box configuration for the local environment uses a `nip.io` address for access, but it can be configured to use a custom ingress domain and corresponding TLS certficate.

Creating the cluster
--------------------

To create a local Kubernetes cluster using Kind and deploy Educates, run the command:

```
educates local cluster create
```

If no local configuration file exists yet, this command creates a default one for you, prints where it wrote it, and continues with the default settings, so this single command is all you need to get started. See [custom configuration](custom-configuration) below to view or change that configuration.

Deleting the cluster
--------------------

If you are done with the local environment and want to delete the Kubernetes cluster, run:

```
educates local cluster delete
```

You can then run `educates local cluster create` to recreate the Kubernetes cluster.

If you know you do not intend to recreate the Kubernetes cluster, and so want everything deleted, you can run:

```
educates local cluster delete --all
```

This will also delete the local image registry, the DNS resolver and the registry mirrors, if deployed. When you run [several local clusters](multiple-local-clusters), every cluster shares these, so `--all` keeps them while another local cluster remains, and names the clusters still using them.

(custom-configuration)=
Custom configuration
--------------------

If you want to provide overrides to the automatically generated configuration for Educates you can edit the local configuration file (an `EducatesLocalConfig`, validated against its schema on save) by running:

```
educates local config edit
```

and entering the configuration. See [configuration settings](configuration-settings) for the available fields. This configuration will be automatically used when running `educates local cluster create`.

You can view the configuration (validating it against the schema) by running:

```
educates local config view
```

This prints the effective configuration with the CLI defaults filled in, so the values supplied on your behalf are explicit. This includes the `<host-IP>.nip.io` ingress domain and the `ingress.insecure` default it implies when you have not set a domain of your own. To see the configuration file exactly as you wrote it instead, add `--raw`.

If you would rather have those defaults written into the file from the start, initialise it with `educates local config init --defaults`, which materialises the fully-defaulted configuration rather than the minimal file `educates local config init` writes by default.

To look up what a field means while editing, use `educates local config explain`, which describes a field and its sub-fields from the schema in the style of `kubectl explain`, for example `educates local config explain ingress`. See [configuration settings](configuration-settings) for the available fields and the other configuration kinds.

You can also supply a configuration file via the `--config` option when running the `educates local cluster create` command. An `EducatesLocalConfig` supplied this way is handled in the same way as the default configuration file. An `EducatesConfig` with `target.provider: kind` is also accepted: the kind cluster is created from its `target.cluster` block and its custom resources are deployed exactly as declared, with none of the local defaults, and secrets in the local secrets cache are not copied to the cluster. See the `EducatesConfig` section of [configuration settings](configuration-settings) for details.

Local image registry
--------------------

When you run the `educates local cluster create` command to create the local Kubernetes cluster, it will also deploy an image registry to your local docker environment. This is used for storing workshop content files and custom workshop base images. The Educates command line tool can be used to publish the workshop content files to this image registry.

If you want to use the registry to store other images, you should tag your images with the registry host/port of `localhost:5001`, then push the image to the registry.

If you want to pull images from the registry in Kubernetes deployments created in the Kubernetes cluster, you can also use `localhost:5001` and it will be automatically mapped to the local image registry, with Kubernetes also trusting the local image registry. This holds on every node of the cluster, including the worker nodes of a cluster that declares them in `cluster.nodes`.

Should you need to pull images from the local image registry from inside of the cluster using tools such as `docker`, `imgpkg`, `oras` or `skopeo`, you will need to use the registry host of `registry.default.svc.cluster.local` instead. This also can be used for Kubernetes deployments as well. Whether you need to tell those tools to trust the image registry will depend on whether they automatically support use of HTTP for access when using `.local` addresses.

Within a workshop definition the ``$(image_repository)`` variable reference will map to `registry.default.svc.cluster.local` and is the recommended way to refer to the local image registry. Workshop templates and the workshop publishing workflow are setup to use this variable rather than a hard coded reference.

If you wish to delete and reinstall the registry after the cluster has been created, you can run:

```
educates local registry delete
```

To deploy the registry again, you can then run:

```
educates local registry deploy
```

The local image registry, and any registry mirrors, are given fixed addresses at the top of the subnet of the `kind` Docker network, so that restarting Docker does not change the address `registry.default.svc.cluster.local` maps to. Docker Engine 29.0.0 to 29.0.3 refuse fixed addresses on the `kind` network, as do older versions until Docker is restarted for the first time after the network is created, in which case Docker picks the address instead, a warning is shown, and the address can still change when Docker restarts. If pulls from `registry.default.svc.cluster.local` inside the cluster fail, or reach a registry mirror, after Docker restarts, upgrade Docker or restart it, then run `educates local registry deploy` again. Running the command for a registry which already exists keeps its content, moves it to its fixed address, and points `registry.default.svc.cluster.local` at it. The command also configures every local cluster to pull from the registry: every one of its nodes pulls `localhost:5001` and `registry.default.svc.cluster.local` from the registry, and inside the cluster, through its own kube context, the `registry` Service and the `local-registry-hosting` ConfigMap point at it. It warns about a cluster which is stopped, and about any node which is not running, and so could not be configured. The `--kubeconfig` and `--context` options of the command are deprecated and ignored, since every local cluster is configured through its own kube context.

If you have successively pushed new builds of images to the local image registry using the same image version tag, the registry can blow out in size over time due to unreferenced image layers. To reclaim space consumed by these unreferenced image layers, you can run:

```
educates local registry prune
```

(custom-ingress-domain)=
Custom ingress domain
---------------------

By default, when deployed to the Kubernetes cluster of the local environment, Educates uses a `nip.io` address for the ingress domain, as a wildcard domain, and serves the cluster over plain HTTP. Because it is not possible to obtain an official trusted TLS certificate for a `nip.io` address, the default install sets `ingress.insecure` automatically, so no certificate authority is needed and the `educates local secrets add ca` step is skipped.

Serving over plain HTTP is convenient for trying Educates out, but because the ingress is not secured, some features of the workshop environment do not work. This includes the inability to use a per session image registry, which requires a secure ingress to be trusted by the Kubernetes cluster and other tools which work with registries.

Instead of relying on a `nip.io` address you can use your own domain that you control and for which you can generate yourself a wildcard TLS certificate. For example, you might own the domain `educates-local-dev.test`, in which case you could use a wildcard TLS certificate for `*.educates-local-dev.test`. You will also need to be able to configure DNS for the domain, or be able to set up a local DNS resolver on your local machine.

In using your own custom domain name, you could do it with a wildcard TLS certificate issued by a trusted registrar such as Lets Encrypt, or you could create a self signed certificate authority (CA) in order to create your own TLS certificate. If using a self signed CA, you will need to configure your operating system and Educates to use that CA.

If you are using a self signed CA, you could technically still use the `nip.io` domain, which would avoid needing to be able to configure DNS, but the domain name will be bound to the IP address of your machine, which can be an issue for laptops that are moved between networks as the IP address will change.

To use a custom ingress domain, set it in the local configuration before creating the cluster:

```
educates local config set ingress.domain educates-local-dev.test
```

Alternatively, you can run `educates local config edit` and add the configuration for the ingress domain as part of the global defaults.

```yaml
ingress:
  domain: educates-local-dev.test
```

When you set a custom ingress domain, the install serves workshop sessions over HTTPS: it installs cert-manager, which issues the wildcard TLS certificate for the ingress domain inside the cluster from a certificate authority (CA) minted for that domain from a root CA kept on your machine. Setting a custom domain turns off the insecure default, so before creating the cluster you provide the root CA, or instead a wildcard certificate as described further below. To generate a root CA:

```
educates local secrets add ca educates-local-ca
```

The command caches the root certificate as `educates-local-ca` and its private key as `educates-local-ca-root-key-pair`, and prints how to export the root certificate for your operating system trust store. To have your browser and operating system trust workshop URLs without warnings, configure them to trust this root certificate, as described in [Trusting the workshop certificates](trusting-the-workshop-certificates). You trust the root once, and it covers every ingress domain you use locally.

If you already use `mkcert`, whose root CA is trusted on your machine, register that root with its private key instead, so that you do not trust a second root:

```
educates local secrets add ca mkcert-ca \
 --cert "$(mkcert -CAROOT)/rootCA.pem" \
 --key "$(mkcert -CAROOT)/rootCA-key.pem"
```

The command checks that the certificate is a CA and that the key matches it. The local secrets cache holds one root CA, so `educates local secrets add ca` refuses a second root and names the existing one. To replace the root, first remove it with `educates local secrets remove NAME`, which also removes its private key and every CA minted from it.

The root's private key never leaves your machine. When a deploy, such as `educates local cluster create` or `educates admin platform deploy --local-config`, needs a CA for the ingress domain, it mints an intermediate CA from the root, caches it as `<domain>-ca-key-pair` and prints a line saying so. The intermediate is limited by an X.509 name constraint to the ingress domain and its subdomains, with every IP address excluded, and it cannot sign further CAs. Only its key pair is copied into the cluster, so a key taken from the cluster cannot sign a certificate your browser accepts for any other site. Later deploys for the same domain reuse the intermediate, and one that has expired, or that was minted from a root you have since replaced, is minted again. Switching to a new ingress domain needs no new trust import, since its intermediate chains to the root you already trust. A root CA that carries name constraints of its own must permit the ingress domain, or the deploy fails saying so. `educates admin platform render` shows the configuration a deploy would use without writing to the cache.

If you would rather not run cert-manager in the cluster at all, serve a wildcard certificate you already have instead. This installs no cert-manager. With `mkcert`, create a certificate for the wildcard of the ingress domain, cache it, and cache mkcert's root CA certificate so that Educates components trust it:

```
mkcert -cert-file educates-local-dev.test.pem \
 -key-file educates-local-dev.test-key.pem "*.educates-local-dev.test"
educates local secrets add tls educates-local-dev.test-tls \
 --cert educates-local-dev.test.pem \
 --key educates-local-dev.test-key.pem
educates local secrets add ca mkcert-ca --cert "$(mkcert -CAROOT)/rootCA.pem"
```

`educates local secrets add tls` checks that the certificate is not a CA, that the key matches it, and that it has at least one wildcard DNS name of the form `*.<domain>`. It records no domain: a cached TLS certificate serves every domain it has a wildcard DNS name for, so one certificate created for several wildcard domains serves whichever of them your local configuration selects. `educates local secrets add ca` with `--cert` alone caches only mkcert's root certificate, never its private key.

When exactly one cached TLS certificate that has not expired covers the ingress domain, a deploy serves it with the `StaticCertificate` certificates provider and installs no cert-manager, even when a root CA is also cached. The deploy says so, names the certificate and any other domains it covers, and gives the `educates local secrets remove` command that removes it, should you want cert-manager to issue the certificates from your root CA instead. It finds the CA that Educates components trust by verifying the certificate's chain against the cached secrets that `educates local secrets list` shows as a `CA certificate`, and warns when none of them verifies it, since the training portal and workshop sessions would not trust the ingress. An expired TLS certificate is skipped with a notice, and the deploy fails, naming the certificates, when more than one that has not expired covers the domain. The certificate is not renewed for you: before it expires, cache the new certificate under the same name with `educates local secrets add tls` and run `educates local secrets sync`, and the cluster serves it without a new install.

If you would rather not manage a CA, you can run the local cluster over plain HTTP by setting `ingress.insecure` in the configuration:

```yaml
ingress:
  domain: educates-local-dev.test
  insecure: true
```

With `ingress.insecure` set, no certificate authority is needed, the `educates local secrets add ca` step is skipped, and `educates local cluster create` serves workshop sessions over HTTP. Because the ingress is not secured, some features that require a trusted secure ingress do not work, including the per session image registry, so an insecure cluster is best suited to trying Educates out rather than developing workshops that rely on those features.

Choose between plain HTTP and HTTPS before creating the cluster. Switching an existing cluster between the two, by setting or clearing `ingress.insecure` or by setting a custom ingress domain on a cluster created with the insecure default, changes how certificates are provided, which an existing installation does not allow, and the deploy fails saying so. The same holds for switching between a root CA and a wildcard certificate, by caching or removing a wildcard certificate for the ingress domain of an existing cluster. To switch, run `educates admin platform delete` and deploy again, or recreate the cluster with `educates local cluster delete` followed by `educates local cluster create`.

Cached secrets, such as the root's CA certificate, the CA minted for the ingress domain, a cached wildcard certificate, and any docker-registry secrets referenced from `secretPropagation.imagePullSecretNames`, are automatically copied into the `educates-secrets` namespace of the local Kubernetes cluster when running `educates local cluster create`, or `educates admin platform deploy` with a local configuration. The root's private key and the CAs minted for other ingress domains are never copied. See [Local secrets cache](local-secrets-cache) for the commands that manage these secrets.

Note that DNS still needs to be configured to map using a CNAME the wildcard domain to the IP address of your local host machine where the Kubernetes cluster is running. This could be done by modifying your actual DNS registry, or you can run a local DNS resolver. If doing this in your global DNS registry, it doesn't matter that the IP address is a local network address which is not accessible to the internet, although depending on what internet router you use for a home network, you may need to disable DNS rebinding protection in your router for the domain.

(local-dns-resolver)=
Local DNS resolver
------------------

The alternative to setting up a global DNS registry to map the wildcard domain to the IP address of your local host machine, is to run a local DNS resolver for the ingress domain. This requires being able to run `dnsmasq` or equivalent locally and configuring the local DNS resolution to forward lookups for the ingress domain to the local DNS resolver.

If you are on macOS, the Educates command line tool provides the means to run `dnsmasq` for you with the required configuration. To start this run:

```
educates local resolver deploy
```

This will run the `dnsmasq` instance in the local docker daemon instance.

Next ensure that the directory `/etc/resolver` exists:

```
sudo mkdir /etc/resolver
```

In that directory create a file with name the same as the ingress domain, with `nameserver` entry pointing at `127.0.0.1`.

```
sudo sh -c "cat > /etc/resolver/${INGRESS_DOMAIN} << EOF
nameserver 127.0.0.1
EOF"
```

To test that the DNS resolver is working correctly run:

```
scutil --dns www.${INGRESS_DOMAIN}
```

You should see an entry like the following for your ingress domain:

```
resolver #8
  domain   : educates-local-dev.test
  nameserver[0] : 127.0.0.1
  flags    : Request A records, Request AAAA records
  reach    : 0x00030002 (Reachable,Local Address,Directly Reachable Address)
```

Note that tools like `nslookup` and `dig` do not use the local DNS resolver so don't expect them to show a result. You can instead use `curl` to test a host within the ingress domain. You should get a HTTP 404 response, which will be the default response from the Kubernetes ingress controller, since there will not be any ingress configured to respond to the request.

```
$ curl -v www.educates-local-dev.test
*   Trying 192.168.168.1:80...
* Connected to www.educates-local-dev.test (192.168.168.1) port 80 (#0)
> GET / HTTP/1.1
> Host: www.educates-local-dev.test
> User-Agent: curl/7.79.1
> Accept: */*
> 
* Mark bundle as not supporting multiuse
< HTTP/1.1 404 Not Found
< vary: Accept-Encoding
< date: Fri, 25 Mar 2022 03:07:19 GMT
< server: envoy
< content-length: 0
< 
* Connection #0 to host www.educates-local-dev.test left intact
```

The IP address used as the target for addresses resolved by the local DNS resolver will be the IP address of the primary active network interface for the local host. If you want to override the IP address and change it to use an alternate IP, such as that used by an alias applied to a network interface using the ``ifconfig INTERFACE alias`` command, you can edit the local Educates config using the command ``educates local config edit`` and add configuration in the following form before deploying the local DNS resolver.

```yaml
resolver:
  targetAddress: 192.168.168.1
```

By default the local DNS resolver will only resolve the domain for the Educates ingress domain. If you want it to resolve other domains and also have them directed to the IP address used for accessing the local Educates cluster, you can also add them in the local Educates config.

```yaml
resolver:
  extraDomains:
  - example.com
```

When you run several local clusters, as described in [Running several local clusters](multiple-local-clusters), the one local DNS resolver serves the domains of all of them, each pointing at its own cluster's address. Creating or deleting a local cluster refreshes a running resolver. After changing the `ingress` or `resolver` configuration of a cluster in any other way, run `educates local resolver update` to have the resolver serve it.

Linux systems may already provide a local DNS resolver in which case you can configure it, however how that is done will depend on the Linux distributions.

If the Linux distribution uses a `dnsmasq` server as the local DNS resolver, it should be configured with an entry like the following:

```
address=/educates-local-dev.test/192.168.168.1
```

where your ingress domain is mapped to the IP address of your local machine.

When done with the local environment and you want to delete the local DNS resolver if started on macOS, you can run:

```
educates local resolver delete
```

You will need to manually remove the file you created under `/etc/resolver`.

Note that when using the local DNS resolver on macOS, for it to work relies on DNS resolution within the local Kind Kubernetes cluster being passed through to the macOS system DNS resolution framework. Although this appears to work for Kubernetes Kind clusters running on macOS Intel architecture, it appears not to currently work for macOS ARM architecture. It is believed this may be because Go application binaries compiled using cross compilation do not link and use the macOS DNS frameworks and instead have a native Go implementation which bypasses the macOS `/etc/resolver` based system. If you are using macOS ARM based machines, you will need to have access to a public DNS registry or server where you can instead configure your domains. Using a DNS server on your local network or embedded in your internet router may also not work as it is also suspected that for macOS ARM acrhitecture, a Kubernetes Kind cluster bypasses local DNS servers and connects direct to a public DNS. The only known way around this at present is to override CoreDNS configuration in the Kubernetes cluster.

(local-secrets-cache)=
Local secrets cache
-------------------

The `educates local secrets` commands manage a cache of Kubernetes secrets kept in the `secrets` directory of the CLI data home. A deploy of a local configuration copies them into the `educates-secrets` namespace of the cluster, where the certificates and registry credentials of the install are read from.

The cache holds private keys and registry credentials, so it is readable only by you: the CLI creates the `secrets` directory with mode `0700` and writes each file in it with mode `0600`. A directory that an earlier release created with looser permissions is changed to `0700` the next time the CLI writes to the cache.

Certificate authorities are cached in three roles:

* A CA certificate `NAME` holds a CA certificate as `ca.crt`, and no private key. It is copied to the cluster so components can trust the CA.
* A CA root key pair `NAME-root-key-pair` holds the private key of the root CA whose certificate is cached as `NAME`. It is never copied to a cluster, and the cache holds at most one.
* A CA key pair `<domain>-ca-key-pair` holds an intermediate CA minted from the root for one ingress domain: the intermediate followed by the root as `tls.crt`, the intermediate's private key as `tls.key`, and the root as `ca.crt`. It is annotated with its domain and the name of its root, and it is copied to a cluster only when that cluster's ingress domain selects it.

A TLS certificate is a `kubernetes.io/tls` secret holding a certificate that is not a CA, optionally followed by its intermediate certificates, as `tls.crt`, and its private key as `tls.key`. The domains it serves are those of its `*.<domain>` DNS names; any domain annotation it carries, as caches from version 3 do, is ignored.

The commands are:

* `educates local secrets add ca NAME` generates a root CA. With `--cert` and `--key` it registers an existing root CA with its private key instead, after checking that the certificate is a CA and that the key matches it. Either way it caches the CA certificate `NAME` and the CA root key pair `NAME-root-key-pair`, and it refuses a second root, naming the existing one. With `--cert` alone it caches only the CA certificate `NAME`, for trust.
* `educates local secrets add tls NAME --cert FILE --key FILE` caches a wildcard TLS certificate, with any intermediate certificates that follow it in the `--cert` file, and its private key. Both flags are required. It refuses a CA certificate, a key that does not match the certificate, and a certificate with no `*.<domain>` DNS name. It has no `--domain` flag: the certificate serves every domain it has a wildcard DNS name for, and a deploy whose ingress domain it covers serves it with the `StaticCertificate` provider and installs no cert-manager.
* `educates local secrets add docker-registry NAME` caches credentials for a container registry. There is no `add` command for a generic secret: add it with `educates local secrets import --file FILE` from a YAML file holding the Kubernetes secret, such as one written by `kubectl create secret generic NAME --from-literal=KEY=VALUE --dry-run=client -o yaml`.
* `educates local secrets list` lists the cached secrets as a table, so you can see what each one is and what a deploy uses it for without reading its YAML. The `TYPE` column is the role of the secret, decided the same way a deploy decides it: `CA certificate`, `CA root key pair`, `CA intermediate key pair` for a CA key pair, `TLS certificate`, `Registry credentials` or `Generic`. A secret to remove from the cache is shown as `invalid`, and the rest of the list still prints. A file that cannot be read or decoded is `invalid`, and `educates local secrets view NAME` says what is wrong with it. So is a CA certificate with its private key that is neither a CA root key pair nor a CA key pair, such as a CA cached by a 4.0.0 alpha release, which deploys neither use nor copy to a cluster; [CAs cached by alpha releases](local-secrets-cache-alpha-cas) says how to keep using such a CA. The `DOMAINS` column is what the secret is valid for: every wildcard domain of a TLS certificate, the ingress domain of a CA key pair, and the permitted DNS name constraints of a CA certificate or CA root key pair, or `any` when it has none. The `EXPIRES` column is the date the certificate expires, in UTC, or `expired`. For example, with a generated root CA, the CA key pair minted from it for `workshops.test`, a `mkcert` wildcard certificate for two other domains with the `mkcert` root CA certificate, and registry credentials:

  ```text
  NAME                         TYPE                       DOMAINS                EXPIRES
  educates-ca                  CA certificate             any                    2036-10-15
  educates-ca-root-key-pair    CA root key pair           any                    2036-10-15
  labs-tls                     TLS certificate            demo.test, labs.test   2029-01-15
  mkcert-ca                    CA certificate             any                    2035-10-15
  registry-credentials         Registry credentials
  workshops.test-ca-key-pair   CA intermediate key pair   workshops.test         2036-10-15
  ```
* `educates local secrets view NAME` shows the details of one cached secret that do not fit the table of `educates local secrets list`, so you can inspect it without decoding its YAML. It never prints key material. It shows the type and domains of the secret as `educates local secrets list` shows them, the file it is cached in and its Kubernetes secret type, and for a CA key pair the ingress domain it was minted for and its root CA. For each certificate the secret holds, those of `tls.crt` and then those of `ca.crt`, it shows the common name, the subject, the issuer, the subject alternative names, the times it is valid from and expires, in UTC and followed by `(expired)` once it has expired, whether it is a CA and its path length, the name constraints of a CA, or `any` when it has none, the serial number and the SHA-256 fingerprint. It shows whether the secret holds a private key, never the key itself, only the registries of registry credentials, never their usernames or passwords, and only the names of the keys of a generic secret. For a secret that `educates local secrets list` shows as `invalid` it shows why, such as a file that cannot be decoded or a CA cached by a 4.0.0 alpha release, and the `educates local secrets remove` command that removes it. For a CA cached by an alpha release it first says to register the CA as the root CA under another name with `educates local secrets add ca ROOT --cert FILE --key FILE`, as [CAs cached by alpha releases](local-secrets-cache-alpha-cas) describes, since removing the secret removes the CA's private key. Naming a secret the cache does not hold is an error. For example, for the CA key pair of the list above:

  ```text
  Name:                  workshops.test-ca-key-pair
  Type:                  CA intermediate key pair
  Location:              /Users/dev/Library/Application Support/educates/secrets/workshops.test-ca-key-pair.yaml
  Secret type:           kubernetes.io/tls
  Domains:               workshops.test
  Ingress domain:        workshops.test
  Root CA:               educates-ca

  Certificate (tls.crt, 1 of 2):
    Common name:         workshops.test
    Subject:             CN=workshops.test,O=Educates
    Issuer:              CN=educates-dev-ca (educates-ca),O=Educates
    Valid from:          2026-10-15 12:44:58 UTC
    Expires:             2036-10-15 13:44:58 UTC
    CA:                  yes, path length 0
    Name constraints:    permitted: workshops.test; excluded: 0.0.0.0/0, ::/0
    Serial:              E4:B1:7D:82:B5:1F:FD:51:FB:12:76:24:DA:53:54:5A
    SHA-256 fingerprint: 1A:BB:05:B8:CE:B2:03:2E:9A:85:70:A1:49:45:E1:FB:AD:17:F1:DD:15:83:52:22:A3:78:EF:08:8C:93:98:CA

  Certificate (tls.crt, 2 of 2):
    Common name:         educates-dev-ca (educates-ca)
    Subject:             CN=educates-dev-ca (educates-ca),O=Educates
    Issuer:              CN=educates-dev-ca (educates-ca),O=Educates
    Valid from:          2026-10-15 12:44:58 UTC
    Expires:             2036-10-15 13:44:58 UTC
    CA:                  yes, no path length limit
    Name constraints:    any
    Serial:              60:F2:29:3C:7D:3B:5B:A4:E3:16:F6:C2:13:0E:66:E7
    SHA-256 fingerprint: FA:23:02:9D:7C:33:95:E4:BF:5B:49:E2:AB:A7:59:42:52:41:2F:5D:DD:57:E7:53:A5:23:A8:9B:39:88:44:6B

  Certificate (ca.crt):
    Common name:         educates-dev-ca (educates-ca)
    Subject:             CN=educates-dev-ca (educates-ca),O=Educates
    Issuer:              CN=educates-dev-ca (educates-ca),O=Educates
    Valid from:          2026-10-15 12:44:58 UTC
    Expires:             2036-10-15 13:44:58 UTC
    CA:                  yes, no path length limit
    Name constraints:    any
    Serial:              60:F2:29:3C:7D:3B:5B:A4:E3:16:F6:C2:13:0E:66:E7
    SHA-256 fingerprint: FA:23:02:9D:7C:33:95:E4:BF:5B:49:E2:AB:A7:59:42:52:41:2F:5D:DD:57:E7:53:A5:23:A8:9B:39:88:44:6B

  Private key:           present
  ```
* `educates local secrets remove NAME` removes a secret from the cache and prints a line for each secret it removes. Removing a root CA, named by its CA certificate `NAME` or by its CA root key pair `NAME-root-key-pair`, removes both together with every CA key pair minted from the root, so no CA material of that root stays in the cache. This is how you replace the root: remove it, then register the new one with `educates local secrets add ca`. Remove the old root certificate from your operating system trust store separately.
* `educates local secrets export` prints the cached secrets, or the one named, as YAML, including any private keys they hold. With `--pem` it prints only the certificates of the named secret as PEM and never a private key, whatever the secret holds. For a CA it prints the root certificate, which is what you import into a trust store: the CA's own certificate for a CA certificate, a CA root key pair or an `invalid` CA cached by a 4.0.0 alpha release, and the root for a CA key pair. For a TLS certificate it prints the certificate and its chain. It goes by the type `educates local secrets list` shows for the secret, so a secret of any other type, such as registry credentials or a generic secret, has no certificate to print.
* `educates local secrets import --file FILE` adds the secrets in a YAML file, such as the output of `educates local secrets export`, to the cache, replacing cached secrets of the same names. Like `add ca`, it keeps the cache to one root CA: it refuses a file holding a CA root key pair when the cache already holds a different root, naming the existing one, and a file holding more than one root. Importing the root the cache already holds is allowed. The file is checked before anything is written, so a refused import leaves the cache unchanged.
* `educates local secrets sync` copies the cached secrets into the `educates-secrets` namespace of the cluster, the same way a deploy does. It never copies a CA root key pair, and it copies a CA key pair only for the `ingress.domain` of the local configuration, minting it first when it is missing or stale. Without a local configuration it copies no CA key pair. When the certificates of the local configuration cannot be decided, such as a secure `ingress.domain` with neither a root CA nor a wildcard certificate for it cached, it warns why and still copies every secret that holds no CA private key, such as registry credentials, while a deploy of that configuration fails.

(local-secrets-cache-alpha-cas)=
### CAs cached by alpha releases

The 4.0.0 alpha releases cached a CA as a `kubernetes.io/tls` secret holding the CA certificate and its private key, under a name that is neither a CA root key pair nor a CA key pair. Such a secret is not migrated automatically: `educates local secrets list` shows it as `invalid`, and deploys neither use it nor copy it to a cluster. To keep using the CA, for example because you already imported it into your operating system trust store, register it as the root CA, then remove the old entry. With `NAME` the name of the old entry and `ROOT` a name for the root CA other than `NAME`, such as `educates-ca`, export the certificate and key of the old entry, register them as the root CA, delete the exported key and remove the old entry:

```
educates local secrets export NAME --pem > NAME.crt
educates local secrets export NAME | awk '$1 == "tls.key:" {print $2}' | base64 --decode > NAME.key
educates local secrets add ca ROOT --cert NAME.crt --key NAME.key
rm NAME.key
educates local secrets remove NAME
```

If the CA was the root CA of `mkcert`, register it straight from `mkcert` instead, then remove the old entry:

```
educates local secrets add ca ROOT --cert "$(mkcert -CAROOT)/rootCA.pem" --key "$(mkcert -CAROOT)/rootCA-key.pem"
educates local secrets remove NAME
```

The cache holds one root CA, so if you cached a CA for each of several domains, register only one of them and remove the others with `educates local secrets remove`: deploys for their domains mint their CAs from the root. Remove those CAs from your trust store too. A local cluster created by an alpha release received the CA's private key and still holds it, so delete and recreate it with `educates local cluster delete` and `educates local cluster create`.

Local image mirrors
-------------------
A local image mirror acts as a proxy for a remote container registry. When you configure a mirror, images pulled from the remote registry are cached locally, so subsequent pulls are faster and do not consume external bandwidth. This is especially useful for large images, repeated CI/CD runs, or when working in environments with limited or expensive internet access.

Using mirrors when creating a cluster
-------------------------------------

You can specify registry mirrors in your cluster configuration YAML. When you create a cluster with mirrors configured, Educates will automatically deploy the mirrors and link them to every node of your local cluster. This ensures that all image pulls for the mirrored registries are routed through your local mirror, improving speed and reliability.

```yaml
cluster:
  listenAddress: 0.0.0.0
  registryMirrors:
    - mirror: ghcr.io
    - mirror: docker.io
      url: registry-1.docker.io
```

In this example, we're mirroring GitHub Container Registry as well as Docker Hub. Note that Docker Hub has an alternate
url for the remote registry mirror. We could have provided credentials for Docker Hub to prevent image pull throttling.

```
educates local cluster create
```

This will deploy the cluster and all defined mirrors automatically. When you run [several local clusters](multiple-local-clusters), they share the mirror containers, and a cluster's configuration has to declare a mirror with the same URL and username as the container another cluster already created.

Deploying a local registry mirror
---------------------------------

To deploy a local mirror for a remote registry, use the following command:

```
educates local mirror deploy ghcr.io
```

In this example we are creating a mirror to GitHub Container Registry (ghcr.io). The mirror name will also used as the remote registry URL, but you can specify an alternate `--url` remote registry URL to mirror. The mirror is linked to every node of every local cluster, as described in [running several local clusters](multiple-local-clusters), and a warning names any node which is not running and so could not be linked.

Optionally, you can also provide credentials, via `--username` and `--password`, for authenticating to the remote registry. Use these if you are mirroring a private registry, or to provide DockerHub credentials to avoid request throttling.

It is important to note that, when done this way, this mirror's configuration is not persisted in the cluster's configuration, so if you were to recreate the cluster, the mirror would not be configured in the cluster, even if the mirror container still exists.

This command is most useful in case you want to clean your existing mirror and delete the mirror, and hence it's contents, and then recreate the mirror.

Deleting a local registry mirror
--------------------------------

To delete a local registry mirror, run:

```
educates local mirror delete ghcr.io
```

This will stop and remove the mirror container and clean up the configuration from every running node of every local cluster.

As pointed out earlier, if the mirror configuration exists in the local cluster's configuration, if the Educates local cluster is recreated then the mirror will be recreated as well.

(multiple-local-clusters)=
Running several local clusters
------------------------------

The cluster `educates local cluster create` makes is the default local cluster. Next to it you can run any number of named local clusters, for example to test two builds of Educates side by side, or one workshop against both. Each named cluster is a Kind cluster of its own, `educates-<name>`, with the kube context `kind-educates-<name>`, its own configuration, and a host IP address of its own on which it publishes ports 80 and 443. Every cluster shares the local image registry on `localhost:5001`, so an image you push there once can be pulled by any of them, and the registry mirrors a cluster's configuration declares share their containers with any other cluster that declares them.

To create a named cluster called `test2` on the host IP address `10.10.10.2`, run:

```
educates local cluster create --local-cluster test2 --listen-address 10.10.10.2
```

When the named cluster has no configuration yet, this writes one to `clusters/test2/config.yaml` in the data home, declaring `cluster.listenAddress` and, when you add `--domain`, `ingress.domain`. It then creates the Kind cluster `educates-test2` and installs Educates into it, while the default cluster keeps running. The install always targets the cluster just created, through its own kube context, whatever `KUBECONFIG` or the current kube context say. Creating a named cluster with neither a configuration nor `--listen-address` fails, and so does a `--listen-address` or `--domain` that disagrees with an existing configuration: the options only write a missing configuration, and never change one. You can also supply the configuration with `--config`, in which case the file supplies the configuration and `--local-cluster` the name of the cluster. The command then makes the cluster's folder `clusters/test2/` in the data home, so the cluster counts as a local cluster, without copying the file into it.

The `educates local cluster create`, `delete`, `start`, `stop` and `status` commands act on the cluster `--local-cluster` names. To act on one cluster for every command in a terminal, set the `EDUCATES_LOCAL_CLUSTER` environment variable instead:

```
export EDUCATES_LOCAL_CLUSTER=test2
educates local cluster status
```

The `--local-cluster` option takes precedence over the environment variable, and an empty value, as in `--local-cluster=`, selects the default cluster even when the variable is set. With neither, the commands act on the default cluster, as they always have. A cluster name is a lowercase DNS label of at most 31 characters.

Deleting a named cluster with `educates local cluster delete --local-cluster test2` deletes its Kind cluster and keeps its configuration, so you can create it again with just `--local-cluster test2`. The `--all` option also deletes the local image registry, the DNS resolver and the registry mirrors that every cluster shares, but only once no other local cluster remains, running or stopped. While another one does, `--all` deletes the selected cluster, keeps the shared containers and names the clusters still using them, so the containers go with the last cluster you delete with `--all`.

Selecting a cluster also decides which cluster the `educates cluster` and `educates admin` commands act on, as [commands that follow the selected cluster](local-cluster-kube-context) describes.

### The listen address

A named cluster's `cluster.listenAddress` must be a specific IP address of your host. It cannot be left out, and it cannot be `0.0.0.0`, which would claim ports 80 and 443 on every host address and leave none for another cluster. It cannot be a loopback address either, such as `127.0.0.2` or `::1`. Workshop sessions resolve the cluster's ingress domain to the listen address, and inside a pod a loopback address reaches the pod itself rather than your host, so the cluster could not reach its own ingress. For the same reason, an alias added to the loopback interface does not work as a listen address.

The address has to exist on your host before you create the cluster, and you add it once:

* On macOS, add it as an alias on a network interface, such as the interface which already holds the address of your default cluster, or your primary interface `en0`:

  ```
  sudo ifconfig en0 alias 10.10.10.2 netmask 255.255.255.255
  ```

* On Linux, add it to a network interface:

  ```
  sudo ip address add 10.10.10.2/32 dev eth0
  ```

Neither command persists the address across a restart of your host, so add it again after one, or configure it permanently through your operating system's network settings.

Docker Desktop 4.97 and later serves published ports below 1024 on loopback addresses only, unless the setting "Allow privileged port forwarding to external machines", under **Settings > Advanced**, is turned on. A named cluster publishes ports 80 and 443 on a non-loopback address, so turn this setting on before creating one.

### Listen addresses that clash

Before `educates local cluster create` creates a cluster, and before `educates local cluster start` starts one, the command line tool checks the cluster's listen address against every other local cluster, running or stopped, as its Kind node containers are configured. It refuses, naming the other cluster, when:

* another local cluster publishes ports 80 and 443 on the same address, since every cluster needs an address of its own;
* either cluster publishes them on `0.0.0.0`, every host address, which leaves no address for any other cluster;
* the address is on no network interface of your host, as when an address you added was lost on a restart.

A stopped cluster counts, since it takes its address back when it starts. A default cluster created on `0.0.0.0` therefore stops any named cluster from being created next to it, and the error tells you to re-create the default cluster on a specific IP address: delete it, set `cluster.listenAddress` in its configuration to an address of your host, such as `10.10.10.1`, and create it again.

Once those checks pass, `educates local cluster create` still tries to bind ports 80 and 443 on the address before it creates the Kind cluster, which catches a port held by something other than a local cluster, such as another ingress controller.

### The ingress domain

With no `ingress.domain` in its configuration, a named cluster uses the `nip.io` name of its listen address, with dashes, such as `10-10-10-2.nip.io` for `10.10.10.2`, and `ingress.insecure` defaults to true, so the cluster is served over plain HTTP with no certificate authority to manage. This mirrors the default cluster, which falls back to the `nip.io` name of your host's IP address. As `nip.io` names only IPv4 addresses, a cluster listening on an IPv6 address needs an `ingress.domain` of its own.

To use a domain of your own, pass `--domain` when you create the cluster, or set `ingress.domain` in its configuration. As for the default cluster, a domain of your own is served over HTTPS and needs a CA in the local secrets cache, as described in [custom ingress domain](custom-ingress-domain).

An `EducatesConfig` with `target.provider: kind` can also serve as a named cluster's configuration. Its `target.cluster.listenAddress` must be a specific IP address of your host, by the same rules, and it gets no `nip.io` fallback: its domain is the one its custom resources declare.

### The DNS resolver

One local DNS resolver serves every local cluster. It points the ingress domain and the `resolver.extraDomains` of each cluster at that cluster's own address, its `resolver.targetAddress`, which defaults to your host's IP address for the default cluster, as described in [Local DNS resolver](local-dns-resolver), and to the listen address for a named cluster. The `educates local resolver deploy` and `educates local resolver update` commands always configure it for every cluster, and take no `--local-cluster` option. The `--domain` option of `educates local resolver deploy` overrides the domain of the default cluster only.

A named cluster is served while its Kind cluster exists. Creating or deleting a cluster refreshes a resolver that is already running, adding or removing that cluster's domains, so you don't need to run `educates local resolver update` yourself. When the resolver can't be refreshed, the command warns and still succeeds, since the cluster has been created or deleted by then: fix the cause the warning names and run `educates local resolver update`.

Another cluster's configuration that does not load, such as a named cluster's configuration you edited to a loopback listen address, or that the resolver can't use, is left out with a warning naming its file. It does not stop `educates local cluster create` from creating a different cluster, nor the resolver commands and the refreshes from serving every other cluster. The configuration of the cluster being created, and the default cluster's configuration given to the resolver commands, still fail the command as before.

The resolver answers a name from the most specific domain it serves, so a named cluster can use a subdomain of the default cluster's domain. With the default cluster on `educates.test` and the named cluster `test2` created with `--domain test2.educates.test`, names under `test2.educates.test` resolve to `10.10.10.2`, and every other name under `educates.test` still resolves to the default cluster. Two clusters claiming the same domain is an error that names both. The resolver commands report it, and so does `educates local cluster create`, before it creates anything.

On macOS, the file `/etc/resolver/educates.test` you created for the default cluster's domain also covers its subdomains, so a named cluster on `test2.educates.test` needs no new file. A named cluster on a domain outside the ones you already resolve, such as `workshops.example.test`, needs a file of its own, `/etc/resolver/workshops.example.test`, with the same `nameserver 127.0.0.1` entry. A cluster on its `nip.io` domain needs neither, since `nip.io` names resolve through public DNS.

### The shared registry and mirrors

Every local cluster pulls from the one local image registry and the one set of registry mirror containers, so the commands which manage them take no `--local-cluster` option and act on every local cluster. `educates local registry deploy` configures every node of every cluster to pull from the registry, and inside each cluster, through its own kube context, points the `registry` Service and the `local-registry-hosting` ConfigMap at it. `educates local mirror deploy` links the mirror into every node of every local cluster, and `educates local mirror delete` unlinks it from every one of them. A node which is not running can't be configured, and the deploy commands warn about it, so run the command again once its cluster runs.

When a cluster's configuration declares a mirror in `cluster.registryMirrors` whose container another cluster already created, `educates local cluster create` reuses that container and links it into every node of the new cluster. The container keeps the URL and username it was created with, so creating a cluster whose configuration declares the mirror with another `url` or `username` fails before the Kind cluster is created. The error names the mirror, the values its container has, and the values the configuration declares. Either declare the mirror as its container has it, or delete the mirror with `educates local mirror delete`, which unlinks it from every cluster, and create the cluster again, which creates the mirror with the new values. Every cluster which declares the mirror then needs those values too. A different `password` can't be seen on the container, so it is not detected, and the mirror keeps the password it was created with until you delete it.

### Listing the clusters

To see every local cluster, run:

```
educates local cluster list
```

It lists the default cluster, shown as `(default)`, and every named cluster, whatever `--local-cluster` or `EDUCATES_LOCAL_CLUSTER` select. Each row has the cluster's state, the host IP address it publishes ports 80 and 443 on, its ingress domain and its kube context. The state is `running`, `stopped`, or `partly running` when only some nodes of a multi-node cluster run. The domain is the one the DNS resolver serves for the cluster: the domain its configuration declares, or else the `nip.io` name of its listen address for a named cluster and of your host's IP address for the default cluster. It shows as `-` when the cluster has none, as for an `EducatesConfig` whose custom resources declare no domain.

Only the Kind clusters the command line tool manages are local clusters: the Kind cluster `educates`, and a Kind cluster `educates-<name>` whose folder `clusters/<name>/` exists in the data home. A Kind cluster you created yourself is left out of the list and ignored by every check, even when its name starts with `educates-`. The `educates local cluster delete`, `start`, `stop` and `status` commands also leave it alone: given the name of a cluster whose folder the data home does not hold, such as `--local-cluster foo` beside a Kind cluster `educates-foo` you created, they refuse with an error saying so.

### A named cluster's configuration

To write a named cluster's configuration without creating the cluster, for example to change it before you first create the cluster, run `educates local config init` with the same options:

```
educates local config init --local-cluster test2 --listen-address 10.10.10.2
```

A named cluster needs `--listen-address`, and `--domain` is optional. As for the default cluster, `educates local config init` refuses to replace an existing configuration unless you add `--force`. The `--defaults` option writes only the default cluster's configuration, while `educates local config view --local-cluster test2` shows the defaults a named cluster takes.

The `educates local config get`, `set`, `view` and `edit` commands also select a cluster with `--local-cluster` or the `EDUCATES_LOCAL_CLUSTER` environment variable, and act on that cluster's configuration, or on the default cluster's with neither. For example, to give the named cluster `test2` a domain of its own:

```
educates local config set --local-cluster test2 ingress.domain test2.educates.test
```

For a named cluster, `educates local config set` and `edit` refuse a change that breaks the rules for its listen address, such as a loopback address, and `educates local config view` shows the `nip.io` name of the listen address as the domain when the configuration sets none.

To remove a named cluster's configuration once you no longer need the cluster, delete the cluster first and then its configuration:

```
educates local cluster delete --local-cluster test2
educates local config delete --local-cluster test2
```

`educates local config delete` removes the folder `clusters/test2` of the data home, which holds the cluster's configuration and its Kind configuration file. It refuses while the Kind cluster `educates-test2` exists, and it never deletes the default cluster's configuration.

(local-cluster-kube-context)=
### Commands that follow the selected cluster

Kind makes the cluster it created last the current context of your kubeconfig. Without a cluster selected, the commands that act on a cluster through a kube context use the current context, as they always have, so they follow whichever cluster was created last, or whichever context you last switched to with `kubectl config use-context`.

When you select a cluster with `--local-cluster` or `EDUCATES_LOCAL_CLUSTER`, these commands act on that cluster's kube context instead, `kind-educates` for the default cluster and `kind-educates-<name>` for a named one:

* the `educates cluster workshop`, `educates cluster portal` and `educates cluster session` commands, and their aliases such as `educates deploy-workshop` and `educates serve-workshop`;
* `educates admin platform deploy`, `educates admin platform delete`, `educates admin diagnostics collect` and `educates admin lookup kubeconfig`;
* `educates local secrets sync`.

So a terminal set to one cluster stays on it, whichever cluster Kind created last. In a terminal where you ran:

```
export EDUCATES_LOCAL_CLUSTER=test2
educates cluster workshop deploy
```

the workshop is deployed to the cluster `educates-test2`, while in a terminal without the variable the same command deploys it to the current context.

The `--context` option takes precedence over both `--local-cluster` and `EDUCATES_LOCAL_CLUSTER`: a command given `--context` acts on the context it names. Selecting a cluster never changes the current context of your kubeconfig, which `kubectl` keeps using.

The context of a selected cluster is looked up in the kubeconfig file `educates local cluster create` writes it into: the file `--kubeconfig` names, and otherwise `~/.kube/config`, whatever the `KUBECONFIG` environment variable says. A terminal whose `KUBECONFIG` leaves out `~/.kube/config` therefore still reaches a selected local cluster. Without a cluster selected, the commands read `KUBECONFIG` as before.

With `--local-config`, `educates admin platform deploy` and `educates admin platform render` read the configuration of the selected cluster: `config.yaml` in the data home for the default cluster, and `clusters/<name>/config.yaml` for a named one. So you can redeploy one cluster from its configuration without touching the others:

```
educates admin platform deploy --local-config --local-cluster test2
```

With `--config`, `educates admin platform deploy` installs the file you name into the selected cluster. `educates admin platform render` talks to no cluster, so `--local-cluster` only selects the configuration it renders, and cannot be combined with `--config`. `educates admin platform delete` reads no configuration, so for it `--local-cluster` only selects the cluster to remove Educates from.

`educates local secrets sync` copies into the selected cluster the cached secrets its configuration needs: the CA key pair for its ingress domain when the cluster is served over HTTPS, and no CA key pair for a named cluster on the `nip.io` name of its listen address, which is served over plain HTTP. Without a cluster selected, it reads the default cluster's configuration and copies into the current context, as before.

### Serving a workshop to several clusters

`educates cluster workshop serve` serves a workshop's instructions from your local system to the workshop sessions of a cluster, so that an edit shows in the sessions as you make it. To serve one workshop to two clusters at once, run the command in two terminals, selecting a cluster in each, the default cluster with an empty `--local-cluster=`:

```
educates cluster workshop serve --patch-workshop --local-cluster=
```

```
educates cluster workshop serve --patch-workshop --local-cluster test2
```

With a cluster selected, two defaults of the command change, so that the two do not compete for the same ports:

* The proxy that workshop sessions connect to listens on the cluster's listen address, such as `10.10.10.2`, instead of on `0.0.0.0`, every host address, when the listen address is a specific IP address: neither `0.0.0.0` nor a loopback address. It keeps port 10081, since a cluster's sessions reach your host through a loopback service under the cluster's ingress domain, which resolves to the cluster's own listen address.
* The Hugo server, which renders the instructions, listens on a free port the operating system picks, instead of on port 1313.

The `--local-host`, `--local-port` and `--hugo-port` options still take precedence when you give them. With `--patch-workshop`, both commands use the access token the command line tool keeps in the data home. Without a cluster selected, the command listens on `0.0.0.0` and port 1313, as it always has.

The default cluster on a loopback listen address, such as its default `127.0.0.1`, has no address of its own for the proxy, so its proxy keeps listening on `0.0.0.0` even when you select the cluster. On macOS, a proxy listening on `0.0.0.0` can share port 10081 with one listening on a named cluster's address, which receives the connections to that address. On Linux, two proxies cannot share the port that way, so to serve a workshop to two clusters at once there, give each cluster a specific listen address, such as `10.10.10.1` for the default cluster, and select each cluster when you serve it.

### A second data home is not a second cluster

The `EDUCATES_CLI_DATA_HOME` environment variable moves the data home, where the command line tool keeps its configuration and local secrets cache, but it is not a way to run several local clusters. A second data home still creates the Kind cluster `educates`, binds the same host ports, and manages the same registry, resolver and mirror containers as the first, so the two collide. Use named clusters instead, which share one data home.

Customize local pod and service CIDRs
-------------------------------------

If you need to modify the pod and service CIDRs for the local Kind cluster, you can do so by editing the local cluster configuration using the command `educates local config edit` and adding the following configuration:

```yaml
cluster:
  networking:
    podSubnet: 10.244.0.0/16
    serviceSubnet: 10.96.0.0/12
```

