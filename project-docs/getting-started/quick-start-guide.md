(quick-start-guide)=
Quick Start Guide
=================

The quickest way to install and start experimenting with Educates is to install it on your local machine using a Kubernetes cluster created using Kind. To make this process easier, Educates provides a command line tool called `educates` you can use to create a cluster and deploy Educates, as well as deploy and manage workshops under Educates.

This local Educates environment is also the recommended setup for working on your own workshop content, as it provides you with a local image registry which can be used to hold both custom workshop base images and your published workshops. Together this provides a quick local workflow for iterating on changes to your workshop content, without needing to publish anything to third party sites.

A detailed description on how to install Educates into any Kubernetes cluster is included later in the documentation.

Host system requirements
------------------------

To deploy Educates on your local machine using the Educates command line tool the following are required:

* You need to be running macOS or Linux. If using Windows you will need WSL (Windows subsystem for Linux). The Educates command line tool has primarily been tested on macOS.

* You need to have a working `docker` environment. The Educates command line tool has primarily been tested with [Docker Desktop](https://www.docker.com/products/docker-desktop/) but you can use [Colima](https://github.com/abiosoft/colima) if you are running on macOS.

* You need to have sufficient memory and disk resources allocated to the `docker` environment to run Kubernetes, Educates etc.

* You cannot be running an existing Kubernetes cluster created using Kind.

* You cannot be using port 80 (HTTP) and 443 (HTTPS) on the local machine as these will be required by the Kubernetes ingress controller.

* You need to have port 53 (DNS) available on the local machine when using macOS if you want to enable a local DNS resolver.

* You need to have port 5001 available on the local machine as this will be used for a local image registry.

If you are using [Docker Desktop](https://www.docker.com/products/docker-desktop/), you will need to enable the following:

* Allow the default Docker socket to be used (Settings->Advanced).

* Allow privileged port mapping (Settings->Advanced).

Depending on the Docker Desktop version you are running, you may also need to enable/disable:

* Use kernel networking for UDP (Settings->Resources->Network).

In case you are using [Colima](https://github.com/abiosoft/colima), you need to add the following lines to the educates configuration file:

```
$ educates local config edit
localKindCluster:
  listenAddress: 0.0.0.0
```

Downloading the CLI
-------------------

To download the Educates CLI visit the releases page at:

* [https://github.com/educates/educates-training-platform/releases](https://github.com/educates/educates-training-platform/releases)

Find the most recent released version and download the `educates` CLI program for your platform.

* `educates-linux-amd64` - Linux (amd64)
* `educates-linux-arm64` - Linux (arm64)
* `educates-darwin-amd64` - macOS (amd64)
* `educates-darwin-arm64` - macOS (arm64)

Rename the downloaded program to `educates`, make it executable (`chmod +x educates`), and place it somewhere in your application search path.

To download the latest version using `curl` and mark it executable, you can use the appropriate command for your operating system and architecture below.

::::{tab-set}

:::{tab-item} Linux (amd64)
```
curl -o educates -sL https://github.com/educates/educates-training-platform/releases/latest/download/educates-linux-amd64 && chmod +x educates
```
:::

:::{tab-item} Linux (arm64)
```
curl -o educates -sL https://github.com/educates/educates-training-platform/releases/latest/download/educates-linux-arm64 && chmod +x educates
```
:::

:::{tab-item} macOS (amd64)
```
curl -o educates -sL https://github.com/educates/educates-training-platform/releases/latest/download/educates-darwin-amd64 && chmod +x educates
```
:::

:::{tab-item} macOS (arm64)
```
curl -o educates -sL https://github.com/educates/educates-training-platform/releases/latest/download/educates-darwin-arm64 && chmod +x educates
```
:::

::::

If you are running macOS with Apple silicon (arm64), the Intel 64 (amd64) binary will still work and be run under Rosetta emulation, however, by using it you will be able to use both `amd64` and `arm64` images in the Kubernetes cluster. If you use the Apple silicon (arm64) binary you will only be able to use `amd64` images in the Kubernetes cluster. Neither of the macOS binaries are signed so you will need to tell macOS to trust it before you can run it.

The `educates` CLI can also be downloaded from the `educates/educates-training-platform` GitHub repository packaged as an OCI image using the command:

```
imgpkg pull -i ghcr.io/educates/educates-client-programs:X.Y.Z -o educates-client-programs
```

Replace `X.Y.Z` with the version of Educates you want to use. Use the appropriate binary found in the `educates-client-programs` sub directory which is created.

The `imgpkg` command if you do not have it can be downloaded as part of the [Carvel](https://carvel.dev/) toolset.

Note that the `imgpkg` command pulls down an OCI image artefact from GitHub container registry. That image is public, if however you get an authentication failure make sure you haven't previously logged into GitHub container registry with a GitHub personal access token which has since expired as that will cause a failure even though the image is public.

The OCI image containing the `educates` CLI can also be used in a `Dockerfile` if needing to embed the `educates` CLI in a container image:

```
FROM ghcr.io/educates/educates-client-programs:X.Y.Z AS client-programs

FROM fedora:42

ARG TARGETARCH

COPY --from=client-programs educates-linux-${TARGETARCH} /educates
```

Default ingress domain
----------------------

Educates requires a valid fully qualified domain name (FQDN) to use with Kubernetes ingresses which it creates.

By default, the `educates` CLI when creating a cluster will automatically use a `nip.io` address which consists of the IP address of your local machine as the ingress domain. For example `192-168-1-1.nip.io`.

If a `nip.io` address is relied upon, some features of Educates may not be able to be used. This is because those features require that you also have access to a wildcard TLS certificate for the ingress domain. Since you don't control the `nip.io` domain, there is no way for you to generate the required TLS certificate using a service such as LetsEncrypt. You could however using your own self signed certificate authority (CA) create a wildcard TLS certificate for the `nip.io` domain but you will need to configure macOS to use the CA, as well as configure Educates to know about the CA.

Also be aware that some home internet routers may block `nip.io` addresses from working. This is because of what is called [DNS rebinding protection](https://en.wikipedia.org/wiki/DNS_rebinding#Protection). You may have to re-configure your router to disable DNS rebinding protection. Alternatively, you can set up your host DNS resolver to use a public DNS provider such as Google (8.8.8.8) or Cloudflare (1.1.1.1).

For the initial deployment we will rely on a `nip.io` address. How to use an alternate ingress domain and a TLS certificate will be covered later.

Local Kubernetes cluster
------------------------

To create a local Kubernetes cluster using Kind and deploy Educates, run the command:

```
educates create-cluster
```

This command will perform the following steps:

* Create the Kubernetes cluster using Kind.

* Enable a security policy engine in the Kubernetes cluster.

* Install Contour into the Kubernetes cluster and expose it via ports 80/443 on the local machine.

* Deploy an image registry running accessible via port 5001 on the local machine.

* Configure the Kubernetes cluster to trust the container image registry.

* Deploy Educates to the Kubernetes cluster.

Creation of the Kubernetes cluster, including the deployment of any required services and Educates, can take up to 5 minutes depending on your network speed.

Once the Kubernetes cluster has been created, you should be able to access it immediately using `kubectl` as the configuration will be added to your local Kube configuration. The name of the Kube config context for the cluster is `kind-educates`.

Deploying a workshop
--------------------

The Educates CLI is intended primarily for people who need to create workshop content. Before we get to how you can create your own workshop, let's start by deploying an existing workshop. In this case we will use an existing workshop which teaches about the fundamentals of using a Kubernetes cluster to deploy an application.

To deploy this workshop run:

```
educates deploy-workshop -f https://github.com/educates/lab-k8s-fundamentals/releases/latest/download/workshop.yaml
```

This will load the workshop resource definition into the Kubernetes cluster. If a training portal instance is not already running one will be deployed. A workshop environment for this specific workshop will then be created and registered with the training portal.

Accessing the workshop
----------------------

To access the workshop you just deployed, run:

```
educates browse-workshops
```

This should open your web browser on the URL for the training portal dashboard.

Note that the training portal will have a password and you will need to be logged in, however the `educates browse-workshops` command will automatically log you in.

If you want to share the URL for accessing the training portal, or enter it manually in the web browser, you can run:

```bash
educates list-portals
```

to get the details.

If the training portal was being accessed by a different user, or you were doing it from a different browser, you will be prompted to enter the training portal password.

To view the password you can run:

```
educates view-credentials
```

Enter the password if prompted. You should then be shown the list of workshops registered with the training portal and can start a workshop.

Note that the first time you run a workshop it may be slow to startup as the container image for the workshop environment will need to be pulled down to the local Kubernetes cluster. So be a bit patient if you have a slow internet connection.

When you have completed the workshop and you exit it, the workshop session will be shutdown and you will be returned to the training portal dashboard.

Deleting the workshop
---------------------

When you no longer require this workshop and wish to delete the workshop environment, run:

```
educates delete-workshop -f https://github.com/educates/lab-k8s-fundamentals/releases/latest/download/workshop.yaml
```

This requires you to provide the same URL for the location of the workshop definition you used when you deployed the workshop. If you do not remember the URL, you can view it by running:

```
educates list-workshops
```

Instead of using the URL, you can also use the name of the workshop as displayed when listing the workshops. For example:

```
educates delete-workshop -n educates-cli--lab-k8s-fundamentals-0129afe
```
