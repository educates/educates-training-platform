(installation-instructions)=
Installation Instructions
=========================

The installation instructions given here are only needed if you are installing into a dedicated Kubernetes cluster and not using the [local Educates environment](quick-start-guide). Ensure you have read the general documentation about [cluster requirements](cluster-requirements) before proceeding with trying to install Educates into an existing Kubernetes cluster.

CLI vs kapp-controller
----------------------

To install Educates into an existing Kubernetes cluster you have two choices.

The first is to use the `educates` CLI. This is a self contained solution and does not require any special operators to be installed into the Kubernetes cluster, nor any special third party packaging tools to be available on the machine from which you are performing the install, beyond having the CLI itself.

The second relies on having the `kapp-controller` operator from the [Carvel](https://carvel.dev/) project pre-installed into the Kubernetes cluster. You will only need to have `kubectl` available on the machine from which you are performing the install.

The `educates` CLI provides a more convenient experience for installing Educates into an existing Kubernetes cluster, however using the Carvel packages with `kapp-controller` may work better when using a GitOps approach to managing Kubernetes clusters.

Opinionated cluster install
---------------------------

Whether using the CLI or `kapp-controller` to facilitate installation of Educates, the Educates installation mechanism provides for an opinionated configuration and installation.

What this means is that it is possible to simply specify the infrastructure provider for the Kubernetes cluster being used and Educates will use a pre-canned configuration suitable for that provider, to install not just the Educates training platform, but other services and Kubernetes operators required by Educates, or which are beneficial when working with that infrastructure provider.

Support is currently provided for the following infrastructure providers.

* `eks` - Amazon Elastic Kubernetes Service (EKS)
* `gke` - Google Kubernetes Engine (GKE)
* `kind` - Kubernetes in Docker (Kind)
* `minikube` - Minikube
* `openshift` - OpenShift (RedHat)
* `vcluster` - Virtual Kubernetes Cluster (Loft)

Although using a pre-canned configuration, you can still provide customizations on top to modify what is installed and how.

If your infrastructure provider is not supported and you have a generic Kubernetes cluster available which has an ingress controller pre-installed, but nothing else, you can use the `generic` provider.

If you would rather roll your own configuration from scratch, the `custom` provider should be used but you would then need to provide a complete configuration for Educates along with enabling what other services you want installed.

Additional installed services
-----------------------------

As noted above, when installing Educates, not just the Educates training platform will be installed, but also other services and Kubernetes operators required by Educates, or which are beneficial when working with a specific infrastructure provider.

The list of additional services that configuration is provided for and that can be automatically installed are:

* `cert-manager` - Certificate manager for Kubernetes.
* `contour` - Ingress controller for Kubernetes.
* `external-dns` - External DNS manager for Kubernetes.
* `kapp-controller` - Carvel package installation operator.
* `kyverno` - Policy enforcement engine for Kubernetes.

Typically Kyverno will always be installed as it is used for security policy enforcement for cluster and workshop security.

The `kapp-controller` operator, although it may not be required for installation, may be required if intending to host workshops that make use of it.

Other services may be automatically installed depending on which infrastructure provider is used.

Package configuration file
--------------------------

When performing an installation a package configuration file must be supplied with values to configure Educates.

The format of the configuration file is YAML. The minimal configuration which is required will depend on the infrastructure provider in which the Kubernetes cluster is running, with more detailed configuration being required if specifying a `custom` configuration.

In the case of targeting a Kubernetes cluster which was previously created using Kind, the minimum required configuration would be:

```yaml
# Specify the infrastructure provider hosting the Kubernetes cluster.

clusterInfrastructure:
  provider: kind

# Specify the ingress domain to be used to access the workshops hosted by
# the Educates installation.

clusterIngress:
  domain: educates-local-dev.test
```

The `clusterInfrastructure.provider` property specifies the identifier for the infrastructure provider to which Educates is being installed.

The `clusterIngress.domain` property needs to be set to the parent domain under which Educates is to be hosted.

Where additional configuration is provided, these will override global defaults, or those for a specific infrastructure provider.

See the general documentation on [Configuration Settings](configuration-settings) for customizing the Educates installation.

For more details on configuration requirements for specific infrastructure providers see the documentation on [Infrastructure Providers](infrastructure-providers).

Customizing the installation via installer images
------------------------------------------------

When installing with the Educates CLI, you can customize the installation in two ways using the ``installerImages`` configuration:

* **Custom bundle**: Replace the default installer bundle with your own customized imgpkg bundle by setting ``installerImages.bundle`` to an OCI image reference. This allows you to use a completely customized installation bundle.

* **Overlays**: Add one or more *installer overlay* bundles by setting ``installerImages.overlays`` to a list of OCI image references. Each overlay is an imgpkg bundle that contains ytt templates or YAML; the CLI fetches these images and runs their content together with the default installer templates. Use this to add resources, patch existing ones, or adjust configuration without forking the installer. Overlay bundles can use any directory layout, as the entire bundle root is passed to ytt.

Both options can be used independently or together. Configure these in your package configuration file under the top-level key ``installerImages``. See [Configuration Settings](configuration-settings) for the ``installerImages`` setting. This option is only available when using the [CLI Based Installation](cli-based-installation); it is not used when installing via Carvel or kapp-controller.

Performing the installation
---------------------------

To perform the installation see the documentation on the process you intend using.

* [CLI Based Installation](cli-based-installation) - Installing Educates using the Educates CLI.
* [Carvel Based Installation](carvel-based-installation) - Installing Educates using pre-installed `kapp-controller` operator.

Note that both of these relate to installing Educates into an existing Kubernetes cluster. If you are trying Educates for the first time it is recommended not to use an existing Kubernetes cluster, but use the Educates CLI to create a local Educates environment, including a Kubernetes cluster, for you.

* [Quick Start Guide](quick-start-guide) - Quick start guide for installing Educates and deploying a workshop.
* [Local Environment](local-environment) - More detailed guide for installing a local Educates environment.
