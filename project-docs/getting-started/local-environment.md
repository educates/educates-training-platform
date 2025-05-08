(local-environment)=
Local Environment
=================

For local development of workshop content the Educates command line tool provides a means for creating a local Kubernetes cluster using Kind, running Educates. This includes an image registry for storing workshop content files and custom workshop base images. The out of the box configuration for the local environment uses a `nip.io` address for access, but it can be configured to use a custom ingress domain and corresponding TLS certficate.

Creating the cluster
--------------------

To create a local Kubernetes cluster using Kind and deploy Educates, run the command:

```
educates create-cluster
```

Deleting the cluster
--------------------

If you are done with the local environment and want to delete the Kubernetes cluster, run:

```
educates delete-cluster
```

You can then run `educates create-cluster` to recreate the Kubernetes cluster.

If you know you do not intend to recreate the Kubernetes cluster, and so want everything deleted, you can run:

```
educates delete-cluster --all
```

This will also delete the local image registry and DNS resolver if deployed.

Custom configuration
--------------------

If you want to provide overrides to the automatically generated configuration for Educates you can provide a global set of defaults for the YAML data values by running:

```
educates local config edit
```

and entering the YAML data values. This configuration will be automatically used when running `educates create-cluster`.

You can view what actual YAML data values will be used for this configuration when doing a deployment of Educates by running:

```
educates local config view
```

You can also supply a YAML data values file via the `--config` option when running the `educates create-cluster` command, however by doing so any secrets in the local secrets cache will not be automatically copied to the cluster.

Local image registry
--------------------

When you run the `educates create-cluster` command to create the local Kubernetes cluster, it will also deploy an image registry to your local docker environment. This is used for storing workshop content files and custom workshop base images. The Educates command line tool can be used to publish the workshop content files to this image registry.

If you want to use the registry to store other images, you should tag your images with the registry host/port of `localhost:5001`, then push the image to the registry.

If you want to pull images from the registry in Kubernetes deployments created in the Kubernetes cluster, you can also use `localhost:5001` and it will be automatically mapped to the local image registry, with Kubernetes also trusting the local image registry.

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

If you have successively pushed new builds of images to the local image registry using the same image version tag, the registry can blow out in size over time due to unreferenced image layers. To reclaim space consumed by these unreferenced image layers, you can run:

```
educates local registry prune
```

Custom ingress domain
---------------------

By default when deployed to the Kubernetes cluster of the local environment, Educates will be configured to use a `nip.io` address for the ingress domain, as a wildcard domain.

This works but because it is not possible to obtain an official trusted TLS certificate for a `nip.io` address, and as such it is not possible to use secure ingresses, some features of the workshop environment may not work. This includes the inability to use a per session image registry, which requires a secure ingress to be trusted by the Kubernetes cluster and other tools which work with registries.

Instead of relying on a `nip.io` address you can use your own domain that you control and for which you can generate yourself a wildcard TLS certificate. For example, you might own the domain `educates-local-dev.test`, in which case you could use a wildcard TLS certificate for `*.educates-local-dev.test`. You will also need to be able to configure DNS for the domain, or be able to set up a local DNS resolver on your local machine.

In using your own custom domain name, you could do it with a wildcard TLS certificate issued by a trusted registrar such as Lets Encrypt, or you could create a self signed certificate authority (CA) in order to create your own TLS certificate. If using a self signed CA, you will need to configure your operating system and Educates to use that CA.

If you are using a self signed CA, you could technically still use the `nip.io` domain, which would avoid needing to be able to configure DNS, but the domain name will be bound to the IP address of your machine, which can be an issue for laptops that are moved between networks as the IP address will change.

To use a custom ingress domain, when running `educates create-cluster` you can supply the `--domain` option to pass the domain name.

```
educates create-cluster --domain educates-local-dev.test
```

Alternatively, you can run `educates local config edit` and add the configuration for the ingress domain as part of the global defaults.

```yaml
clusterIngress:
  domain: educates-local-dev.test
```

This will still only allow HTTP as is and will not use a secure ingress. If you want to use secure ingress you need to provide the corresponding wildcard TLS certificate.

If you had used certbot and Lets Encrypt to create a wildcard TLS certificate using a DNS challenge, you could then configure Educates to know about it and use it by running:

```
educates local secrets add tls ${INGRESS_DOMAIN}-tls \
 --cert $HOME/.letsencrypt/config/live/${INGRESS_DOMAIN}/fullchain.pem \
 --key $HOME/.letsencrypt/config/live/${INGRESS_DOMAIN}/privkey.pem \
 --domain ${INGRESS_DOMAIN}
```

The `--domain` option must be used to indicate the domain the wildcard TLS certificate is for, as the name of the secret is not significant. You can if necessary add multiple wildcard TLS certificates for different domains under different names. The TLS certificate annotated with the domain name which matches the `clusterIngress.domain` setting will be used.

If the wildcard TLS certificate is self signed using your own certificate authority (CA) certificate, you would still use the above command to add the TLS certificate for Educates to use, but supply the location of where you had saved the corresponding files. You can then provide the CA certificate for Educates to use by running:

```
educates local secrets add ca ${INGRESS_DOMAIN}-ca \
 --cert "`mkcert -CAROOT`/rootCA.pem" \
 --domain ${INGRESS_DOMAIN}
```

In this example, it was assumed that ``mkcert`` had been used to create the CA certificate and wildcard TLS certificate and thus we run ``mkcert`` to determine where the CA certificate was stored.

These secrets will be automatically copied to the local Kubernetes cluster when running `educates create-cluster` provided that the `--config` is not being used.

Note that DNS still needs to be configured to map using a CNAME the wildcard domain to the IP address of your local host machine where the Kubernetes cluster is running. This could be done by modifying your actual DNS registry, or you can run a local DNS resolver. If doing this in your global DNS registry, it doesn't matter that the IP address is a local network address which is not accessible to the internet, although depending on what internet router you use for a home network, you may need to disable DNS rebinding protection in your router for the domain.

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
localDNSResolver:
  targetAddress: 192.168.168.1
```

By default the local DNS resolver will only resolve the domain for the Educates ingress domain. If you want it to resolve other domains and also have them directed to the IP address used for accessing the local Educates cluster, you can also add them in the local Educates config.

```yaml
localDNSResolver:
  extraDomains:
  - example.com
```

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

Local image mirrors
-------------------
A local image mirror acts as a proxy for a remote container registry. When you configure a mirror, images pulled from the remote registry are cached locally, so subsequent pulls are faster and do not consume external bandwidth. This is especially useful for large images, repeated CI/CD runs, or when working in environments with limited or expensive internet access.

Deploying a local registry mirror
---------------------------------

To deploy a local mirror for a remote registry, use the following command:

```
educates local mirror deploy ghcr.io
```

In this example we are creating a mirror to GitHub Container Registry (ghcr.io). The mirror name will also used as the remote registry URL, but you can specify an alternate `--url` remote registry URL to mirror.

Optionally, you can also provide credentials, via `--username` and `--password`, for authenticating to the remote registry. Use these if you are mirroring a private registry, or to provide DockerHub credentials to avoid request throttling.

Deleting a local registry mirror
--------------------------------

To delete a local registry mirror, run:

```
educates local mirror delete ghcr.io
```

This will stop and remove the mirror container and clean up the configuration from the cluster.

Using mirrors when creating a cluster
-------------------------------------

You can specify registry mirrors in your cluster configuration YAML. When you create a cluster with mirrors configured, Educates will automatically deploy and link the mirrors to your local cluster. This ensures that all image pulls for the mirrored registries are routed through your local mirror, improving speed and reliability.

```yaml
localKindCluster:
  listenAddress: 0.0.0.0
  registryMirrors:
    - mirror: ghcr.io
    - mirror: docker.io
      url: registry-1.docker.io
```

In this example, we're mirroring GitHub Container Registry as well as Docker Hub. Note that Docker Hub has an alternate
url for the remote registry mirror. We could have provided credentials for Docker Hub to prevent image pull throttling.

```
educates create-cluster
```

This will deploy the cluster and all defined mirrors automatically.




