---
orphan: true
---

Version 2.0.0
=============

In version 2.0.0 of Educates many major notable changes have been made. A summary of the most significant new features are listed below. Check subsequent sections for more details on new features, changed features and bug fixes.

* Using Carvel packaging for installation in place of `kustomize`.
* An updated local Educates environment for authoring of workshop content.
* A new set of workshop templates, including GitHub actions for publishing workshops.
* Better security through support of Kyverno for enforcing security policies.
* Support for OpenShift, with security context contraints being used to enforce security policies.
* Switch to `vendir` for downloading workshop content for each workshop session.
* A generic package mechanism for adding additional software and files to a workshop session.
* Support for creating virtual clusters for each workshop session.
* Integrated support for having a Git server for each workshop session.
* Support for embedding web sites which can interact with the workshop dashboard.
* Improved analytics for workshop events, including ability to track when actions are triggered.
* Ability to add ingresses to a workshop session which are not gated by session authentication.
* Builtin operator for copying secrets between namespaces and adding them to service accounts.
* Ability to block access to specific network address blocks from workshop/session namespaces.
* Ability to specify secrets that should be copied into workshop namespace for use by workshop.
* Ability to customize the finished workshop dialog box with custom content.
* Ability to forcibly recycle workshop environments from the training portal admin pages.
* Ability to disable access to the session namespace from a workshop container.
* A maximum time deadline can now be specified past which workshops cannot be extended.
* For slide presentations, multiple reveal.js versions are now provided, as well as impress.js.

New Features
------------

### Installation using Carvel

Version 1.X of Educates used `kustomize` to perform installations. Version 2.0.0 uses Carvel packaging and tools. This change has been made to provide more flexibility when installing Educates and allow new features which require additional operator components to be more easily added. Use of Carvel packaging also allows for installing Educates in disconnected Kubernetes environments.

For more information see:

* {any}`Installation Instructions <installation-instructions>`
* {any}`Configuration Settings <configuration-settings>`

Note that although Learning Center also uses Carvel packaging, configuration for the respective packages is not compatible.

### Local Educates environment

A set of scripts for deploying Educates locally to a Kind cluster previously existed. These scripts have been improved on and are now the recommended environment for both development of Educates, as well as for local workshop content creation. The manner in which workshop content is handled when using the local Educates environment is factored into workflows for workshop content creation, including compatibility with how GitHub actions are used to publish workshop content.

For more information see:

* [Quick Start Guide](quick-start-guide)
* [Local Environment](local-environment)

### Updated workshop templates

For Educates 1.X a couple of GitHub repositories were provided which could be used as GitHub repository templates when creating a GitHub repository for your own workshop.

In Educates 2.0.0 this has been replaced. The replacement can still be used as a GitHub repository template for simple workshops, but the recommended method is to download a copy of the workshop templates to your local machine and run the provided script to create your initial workshop content. When using this script parameters can be provided to customize the workshop content, including being able to apply additional overlays which add extra configuration into the workshop where you may require a virtual cluster, spring.io integration etc.

For more information see:

* [Creating a Workshop](creating-a-workshop)
* [Workshop Templates](workshop-templates)

### Security policy engine

Version 1.X of Educates relied in part on pod security policies to enforce restrictions on what workloads deployed from a workshop session could do. This included preventing workloads running as `root`, or using elevated privileges. Pod security policies in Kubernetes have however been deprecated and will be removed in Kubernetes 1.25. Even though based on older versions of Kubernetes, current Tanzu Community Edition (TCE) clusters do not support pod security policies at all. Enabling pod security policies in Tanzu Kubernetes Grid (TKG) was also optional. As a consequence when running Educates 1.X in many Kubernetes clusters, security couldn't be enforced properly and untrusted workshop users could run workloads using elevated privileges and could breach security of the cluster.

In Educates 2.0.0 multiple native policy engines are supported for enforcing cluster security. These are:

* Pod security policies (Kubernetes <= 1.25).
* Pod security standards (Kubernetes >= 1.22).
* Security context constraints (OpenShift)

After experimenting with pod security standards, the intended replacement for pod security policies, it was deemed as not flexible enough for the needs of Educates going forward. As a consequence support has also been added for the following separate policy engines:

* [Kyverno](https://kyverno.io/)

For Educates 2.0.0, Kyverno is now the recommended policy engine. In addition to using this as a replacement for pod security policies and pod security standards, Kyverno is used to enforce additional restrictions on workshops.

For more information see:

* [Cluster security enforcement](cluster-security-enforcement)
* [Workshop security enforcement](workshop-security-enforcement)

Note that there is no intention to support Open Policy Agent. If using OpenShift, at this time you still need to use security context contraints for cluster security enforcement.

### Support for OpenShift

Educates 1.X could not be deployed to OpenShift. This was principally due to OpenShift not supporting pod security policies and instead having it's own policy engine system using security context contraints.

Educates 2.0.0 can now be deployed to OpenShift. For this to work the policy engine support for security context constraints must be enabled.

For more information see:

* [Installing cluster essentials](installing-cluster-essentials)
* [Create the configuration](create-the-configuration)

Note though that bundled support for running the OpenShift web console is no longer provided, nor is the `oc` command line client provided in the workshop base image.

### Downloading of workshop content

Downloading of workshop content by specifying `spec.content.files` has been deprecated and is replaced with `spec.workshop.files`. This new mechanism uses `vendir` under the covers to download workshop content from one or more sources. Support includes being able to provide credentials for image repositories and Git repositories via Kubernetes secrets. Downloading of any workshop content when credentials are used will be done from a separate init container so that credentials are not exposed to a workshop user.

This avenue for downloading workshop content only applies to workshop content, including instructions, setup scripts, and files to be used in exercises. For a more general means of adding additional application software to a workshop session see the new packaging mechanism for installing extensions.

For more information see:

* [Downloading workshop content](downloading-workshop-content)
* [Hosting on an image repository](hosting-on-an-image-repository)
* [Hosting using a Git repository](hosting-using-a-git-repository)

### Installation of extension packages

When needing access to additional applications in a workshop it is possible to create a custom workshop base image. The problem with this solution is that the custom workshop base image is bound to a specific version of the standard workshop base image, meaning you could miss out on important bug fixes if newer versions are released. The alternative is to download and install the required applications in each workshop session using a setup script. The problem with this solution is that if it is a commonly used application, there end up being multiple versions of the setup script spread across different workshops and keeping them up to date is hard. Thus it is only a good idea to use this approach for one off cases.

To support easy installation of additional applications with each workshop session a new packaging and installation mechanism is supported based on `vendir`. This allows for additional applications to be installed into separate package directories under `/opt/packages`. Any package installed in this way can provide its own setup scripts or shell profile configuration which will be automatically used when a workshop session starts, enabling the workshop users environment to be automatically configured.

Educates 2.0.0 supplies a number of packages with commonly used tools. This allows for example a way to easily install additional applications such as the TCE variant of the `tanzu` command.

For more information see:

* [Adding extension packages](adding-extension-packages)

### Support for virtual clusters

By default for each workshop session a workshop user is given access to a single Kubernetes namespace to which they can deploy workloads as part of the workshop. The workshop user, with a few exceptions on what they could do, had admin access to that Kubernetes namespace. The workshop user did not have access to any privileges a cluster admin may have access to.

In Educates 2.0.0, for a specific workshop it is now possible to enable the provisioning of a virtual cluster in place of the session namespace. This provides the workshop user with the appearance of having their own complete Kubernetes cluster, including cluster admin access, except that it is a virtualized cluster running in the bounds of a single Kubernetes namespace of the underlying Kubernetes cluster. A workshop user then only has access to the virtual cluster, and has no access to the underlying host Kubernetes cluster.

Although a workshop user has access to a virtual Kubernetes cluster, they can still be bound by various restrictions. By default the security policy enforced will allow deployment of workloads running as `root`, but workloads will not be able to elevate privileges further. Quotas and default limit ranges will also be applied to the virtual cluster if enabled.

Workloads which use Carvel packaging can be deployed automatically to each virtual cluster if necessary. For example, deployment of `kapp-controller`. An in-built option is provided for deploying Contour ingress controller to the virtual cluster if more than the standard ingress resources are required.

For more information see:

* [Provisioning a virtual cluster](provisioning-a-virtual-cluster)

### Integrated Git server

An integrated Git server can be enabled, where each workshop session has its own Git server instance hosted from the workshop container. Access to the Git server is authenticated with unique credentials per workshop session. The Git server is exposed via an ingress and can be used in workshops which need access to hosted source code as part of a CI/CD pipeline.

The Git server supports any number of code repositories, with repositories being able to be initialized from workshop setup scripts, or by a user as part of workshop instructions. Changes can be made to a checkout of any source code, with changes pushed back to the Git server. Git hooks can be provided to emulate functionality such as webhook integration for triggering CI/CD pipelines. The Git server is automatically deleted when the workshop session terminates.

For more information see:

* [Enabling the local Git server](enabling-the-local-git-server)

### Workshop analytics events

A clickable action in the workshop instructions can be designated as an event source for analytics delivered by the analytics webhook defined for a training portal. Additional events are now also generated for a workshop session being created (distinct from a workshop session being started), as well as creation, termination and deletion of workshop environments, creation and deletion of anonymous user accounts, plus events for errors such as failure to download workshop content or run setup scripts. Events will also be generated for deletion of workshop sessions which had never been allocated to users.

For more information see:

* [Generating events for actions](generating-events-for-actions)
* [Collecting analytics on workshops](collecting-analytics-on-workshops)

### Clickable actions in Javascript

The functionality of a subset of clickable actions can be accessed from web pages or web sites embedded in a dashboard tab, using Javascript messages.

For more information see:

* [Triggering actions from Javascript](triggering-actions-from-javascript)

### Disabling ingress authentication

When specifying additional ingress points, by default access to the URL will be protected by the workshop session access controls. To disable the access controls on the URL it is now possible to override the authentication type.

For more information see:

* [Defining additional ingress points](defining-additional-ingress-points)

### Secret copying and injection

Educates now provides a companion operator which implements custom resources to set up rules for copying secrets between namespaces, as well as injecting secrets into service accounts. The secret copier and secret injector are used by Educates itself in it's implementation, but can also be of use in workshops. The implementation provides much more fine grained control than other solutions such as the Carvel Secretgen controller and would need to be used in place of the Carvel Secretgen controller as it's use is disabled in conjunction with Educates workshops due to security issues around it's model for blindly copying secrets to all namespaces.

Note that implementation of this mechanism is not yet finalized. More details will be provided later.

### Blocking network access

When installing Educates it is now possible to specify networks or specific IP addresses for which access should be blocked from workloads running in the workshop and session namespaces. As AWS would be a typical deployment target, by default the `169.254.169.254` and `fd00:ec2::254` IP addresses are blocked, which correspond to an internal host of AWS that can expose sensitive information about a users AWS account.

For more information see:

* [Restricting network access](restricting-network-access)

Note that this ability results in `NetworkPolicy` resources not being able to be created, edited or deleted by workshop users in session namespaces.

### Workshop environment secrets

It is now possible to provide a list of secrets, defined by namespace they are contained in, and the name of the secret, in the workshop definition and these will be copied into the workshop namespace.

The primary purpose for such secrets would be to hold credentials to access an image repository or Git repository when downloading workshop content. When the secret is then referenced in turn by name in `vendir` descriptions embedded in the workshop definition for downloading workshop files, or packages, an init container is created to perform such downloads so that any credentials are not visible to a workshop user in the main workshop container.

Such secrets might also be used by a workshop in other ways, such as mounting into the workshop container, or an extra init container to preform some special setup where a workshop user should not see the credentials.

For more information see:

* [Injecting workshop secrets](injecting-workshop-secrets)

### Finished workshop dialog

It is now possible to customize the description displayed in the finished workshop dialog. This could be just a change to the description, or an embedded form could be included to allow entering into a raffle where Educates is being used to host workshops at a conference booth. Alternatively, you might generate a QR code that people could scan on their own device so as to enter a raffle or fill out some other type of survey away from the booth and thus free up the booth laptop for other users.

Because this customization is only offered for the specific dialog shown when a workshop user fully finishes the workshop, and not if they exit the session early, in order to warn them of the required path they must take to get their reward, custom content for a new dialog to be shown when the workshop is started can also be provided. 

For more information see:

* [Overriding styling of the workshop](overriding-styling-of-the-workshop)

### Recycling workshop sessions

It is now possible from the training portal admin pages to select workshop environments and mark them for shutdown. This will result in a new workshop environment being created in its place, and with the old workshop environment being deleted when there are no more active workshops sessions against it. This can be used to force recycle workshop environments if necessary without needing to modify the training portal resource or delete and recreate the training portal.

### Disable REST API access

If a workshop doesn't actually need access to the Kubernetes cluster to deploy workloads, because it does everything in the workshop container or is only used as jumpoff box for another system, disabling REST API access for the local Kubernetes cluster previously involved providing a patch for the workshop pod template spec to disable auto mounting of the service account token. This can now be done by setting `session.namespaces.security.token.enabled` to `false` in the workshop definition.

For more information see:

* [Blocking access to Kubernetes](blocking-access-to-kubernetes)

### Maximum time deadline on workshops

In Educates 1.X a duration was specified for a workshop in the training portal definition, the workshop user could still extend the time provided to finish the workshop by clicking on the countdown timer in the workshop dashboard when it was displayed in red. Time could be extended in this way as many time as a workshop user wanted.

In Educates 2.0.0, by default a workshop session will always be terminated when the expiration time for the workshop has been reached. If you want to allow a user to be able to extend the time for the workshop you now need to configure the training portal to allow it. This is done by specifying a maximum time deadline up to which time extensions can be made. When the countdown timer is displayed as orange in the workshop session it can be clicked on to extend the time, but if displayed as red, it is in the final time period and cannot be extended further.

For more details see:

* [Expiring of workshop sessions](expiring-of-workshop-sessions)

### Slide presentation software

Both versions 3.X and 4.X of reveal.js are now supplied. Version 1.X of impress.js is now also supplied. If wishing to use either of the slide presentation packages, you must specify the version you need. It is also now necessary to enable the slide presentation feature.

For more details see:

* [Enabling presentation slides](enabling-presentation-slides)

Features Changed
----------------

### Educates API group changed

In Educates 1.X the API group used for custom resoures was `training.eduk8s.io`, with resource versions using `v1alpha1` or `v1alpha2`.

In Educates 2.X the API group used for the equivalent custom resources is `training.educates.dev`, with the resource version always being `v1beta1`.
### System profiles no longer exist

The concept of system profiles and the `SystemProfile` resource have been removed. Only a single global configuration now exists which is configured through the data values supplied when deploying Educates.

### Resource naming changes

The name of the namespace into which the operator for managing workshop sessions is deployed has changed from `eduk8s` to `educates`. The name of the operator deployment was in the process changed from `eduk8s-operator` to `session-manager`. The name of the deployment for a training portal was also changed from `eduk8s-portal` to `training-portal`.

Naming for any cluster scoped resources created by the operator have been changed so they all use an `educates` prefix. The name of the status key in custom resources managed by the operator has changed from `eduk8s` to `educates`. Names of service accounts and role bindings in workshop and session namespace have also changed.

So long as workshop definitions used the appropriate data variables in session and environment objects they should not be affected by these changes.

Note that the older `eduk8s` naming is still used inside of the workshop container image for the user home directory, user name etc.

### Hostnames for proxied ingresses

When configuring the workshop session to act as a proxy using `spec.session.ingresses` an ingress is automatically created. The format of the host name for the ingress is:

```
$(ingress_protocol)://$(session_namespace)-application.$(ingress_domain)
```

Instead of a suffix for the application name, a prefix is now also supported, and bundled applications such as the console and editor now use the prefix convention.

```
$(ingress_protocol)://application-$(session_namespace).$(ingress_domain)
```

Using a prefix is the recommended convention if you want to be able to create workshops that target deployment as a container using `docker` in conjunction with a `nip.io` style address.

### Stripped down workshop definition

Previously the complete `Workshop` definition was mounted into the workshop container. This is no longer the case and only a stripped down version is now provided to the workshop container. This consists of the `spec.session.applications`, `spec.session.ingresses` and `spec.session.dashboards` configuration only. This is being done to avoid sensitive information such as credentials defined in `spec.session.objects`, `spec.environment.objects` or `spec.session.patches` being visible to workshop users.

### Workshop base image name

The `spec.content.image` property is now deprecated and `spec.workshop.image` should be used instead.

Further, only the following short names for workshop images are now recognised in either property:

* ``base-environment:*`` - A tagged version of the ``base-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``jdk8-environment:*`` - A tagged version of the ``jdk8-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``jdk11-environment:*`` - A tagged version of the ``jdk11-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``jdk17-environment:*`` - A tagged version of the ``jdk17-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``jdk21-environment:*`` - A tagged version of the ``jdk21-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``conda-environment:*`` - A tagged version of the ``conda-environment`` workshop image which has been matched with the current version of the Educates operator.

Prior short names with `master` and `develop` tags are no longer recognised.

### Third party software upgrades

Versions of third party packages used by Educates were updated to latest available at the time. This includes Carvel tools, Kubernetes dashboard, `kustomize`, `helm`, `skaffold` etc. The `kubectl` versions currently provided are 1.20 through 1.23.

### Java runtime/framework upgrades

Versions of Java tools, including Maven and Gradle have been updated when using the Java workshop base images.
### Upgrade of VS Code version

Educates was updated to use the latest major update of VS Code package from [Coder](https://github.com/coder/code-server). This is version v4.x vs the previously used v3.x version.

Due to changes in how the newer version of VS Code handles extensions, Educates no longer installs any editor extensions by default. It is therefore up to a specific workshop to install the editor extensions they want to use.

Installation of editor extensions can be done from a `workshop/setup.d` file when the workshop container starts.

You can either directly install the extension from the [Open VSX Registry](https://open-vsx.org/), or bundle the `vsix` file with the workshop assets and install it from the local file system

If for example you were running a Java workshop, you might include an executable setup script file in `workshop/setup.d` containing:

```
#!/bin/bash

set +x

code-server --install-extension Pivotal.vscode-spring-boot@1.30.0
code-server --install-extension redhat.java@1.3.0
code-server --install-extension vscjava.vscode-java-debug@0.38.0
code-server --install-extension vscjava.vscode-java-dependency@0.19.0
code-server --install-extension vscjava.vscode-java-test@0.34.1
code-server --install-extension vscjava.vscode-maven@0.35.0
code-server --install-extension vscjava.vscode-spring-initializr@0.8.0
```

Note that the path to `code-server` also changed with this update to `/opt/editor/bin/code-server`. Previously it was `/opt/code-server/bin/code-server`. It is now however included in `PATH` and so you shouldn't need to use the absolute pathname.

If you didn't want to automate installation of extensions, you could just instruct a user to install the extension themselves via the VS Code interface as part of the workshop instructions.

Note that one of the reasons that editor extensions are no longer installed by default is because some extensions have significant memory resource requirements even when not being used. This is the case for the Java extension. You should therefore only install an editor extension if you absolutely need it and it is used by the workshop.

### OpenShift client and console

Client tools for working with OpenShift, including support for the OpenShift web console were removed. Workshops which are specific to OpenShift will need to add these tools themselves.

### Secretgen controller blocking

Because of the inherit security risks in how Carvel Secretgen controller works, it is now blocked from copying secrets into any Educates namespaces using a wildcard for the destination namespace. This is done to stop sensitive credentials held in image pull secrets being copied into session namespaces where an untrusted user can access them and use them to access services they shouldn't. If you legitimately need the ability to copy secrets into all session namespaces for a workshop, the builtin secret copier provided with Educates is a better option as it provides more flexibility around which namespaces a secret should be copied to.

### Location of Educates config/logs

In Educates 1.X the `$HOME/.eduk8s` directory was used to hold some Educates configuration files used during a workshop session, along with log files and marker files used to indicate failures when downloading workshop content or running setup scripts. This directory is no longer used and the `$HOME/.local/share/workshop` directory is used instead to better align with Linux standards for directory layouts and usage.
### Logging of workshop downloads

When workshop content is being download, the output of the download script is now saved in `$HOME/.local/share/workshop/download-workshop.log` so the reasons for failure can be worked out from the workshop terminal, rather than needing to look at the workshop container pod logs.

### Podman binary has been removed

The `podman` binary, along with any support for trying to use `podman` has been removed from the workshop image. This support didn't necessarily work properly and `docker` support was more reliable.

### Latest version of Octant

The latest version of Octant is now included in the workshop base image. Although included this isn't used when a workshop session is using a session namespace as newer Octant versions do not work properly when restrictive RBAC is being used. The newer version of Octant is only used when virtual cluster support for a workshop session is enabled and Octant is selected as the web console to use in place of the standard Kubernetes dashboard.

### Training portal deployment

If there is a failure when attempting to deploy the training portal, the Educates operator will now back off and after a delay will delete the training portal and reattempt the deployment of the training portal. This is just in case the reason for the failure is transient and it may work if tried again.

### Workshop environment creation

If there is a failure when attempting to create a workshop environment, the Educates operator will now back off and after a delay will delete the workshop environment and reattempt the creation of the workshop environment. This is just in case the reason for the failure is transient and it may work if tried again.

### Webhook URL for analytics

The webhook URL for reporting of events for analytics, from workshop sessions and the training portal can now be specified in the global Educates configuration as well as being overridden for a specific training portal instance.

### Browsing workshop files

It is no longer possible to get a browsable index page of files when using the `files` application in a workshop definition. Any file downloads must always use the exact URL they need to.

### Security policy changes

The names used to select different security policies have been changed. The equivalent names are:

* `anyuid` -> `baseline`
* `nonroot` -> `restricted`
* `custom` -> `privileged`

The old names are still accepted but are deprecated.

Further, the ``spec.session.security.policy`` setting no longer exists. This was previously for overriding the security policy for the workshop container but is no longer required. Only the security policy for session namespaces can be overridden using ``spec.session.namespaces.security.policy`` or more specific settings for secondary namespaces.

### Resorce quota memory limits

The default pod memory limit for all resource budgets, excluding `small` have been set to 512Mi, instead of ranging up to 2Gi. If any workload needs more than 512Mi they will need to provide a memory resource request in the deployment resources, or override the default resource limits in the workshop definition.

### Persistent API access tokens

Kubernetes 1.24 changes default behaviour of API access tokens mounted into workshop containers and will now refresh them every hour. For now it seems it doesn't completely invalidate the old token, but this may occur at some time in the future. To prevent this causing problems with a workshop session's access being invalidated, since it creates a `kubeconfig` file at the start of the workshop session and wouldn't refresh it, any Educates deployments now create a separate secret of type `kubernetes.io/service-account-token`, mounts that into containers and uses that in place of the standard in-cluster credentials. Such secrets are guaranteed to be valid for the life of the service account they are associated with.

### Install zlib-devel system package

The `zlib-devel` system package is now installed into the workshop base image as this can be a common requirement if a workshop compiles third party open source software.

### Labels identifying applications

Distinct labels are now added to the workshop and registry deployments so they can be more easily distinguished when needing to write automation, such as to run `kubectl exec` in workshop containers to determine if workshop content download or setup scripts failed when validating a large scale deployment of workshops before a scheduled workshop.

### Overriding ingress domain for portal

It is no longer possible to override the ingress domain and secret in a `TrainingPortal` resource. The global settings applied in the Educates configuration when it is deployed will always be used.

### Portal workshop defaults settings

The settings for workshop defaults in the `TrainingPortal` resource of Educates 1.X were `spec.portal.capacity`, `spec.portal.reserved`, `spec.portal.initial`, `spec.portal.expires` and `spec.portal.orphaned`. These have been deprecated and you should use similarly named settings under `spec.portal.workshop.defaults` instead.

```yaml
spec:
  portal:
    workshop:
      defaults:
        capacity: 6
        reserved: 2
        initial: 4
        expires: 60m
        orphaned: 5m
```

Bugs Fixed
----------

### Terminal reconnection failures

The workshop dashboard terminals will attempt to automatically reconnect when the web socket connection between the browser and the backend is lost. In some cases, especially when the browser window for the workshop dashboard was not visible for some time, the reconnection would fail. It is believed this issue has now been addressed and terminal connections should now be more reliable.

### Fixup of OCI artefact permissions

The `imgpkg` program from Carvel used to package up workshop content as an OCI artefact, allowing it to be used as the source of workshop content, does not preserve correctly the original file permissions for group and others. This can cause problems where workshop content contains files used as input to `docker` image builds and permissions on files are important. Loss of the permissions can for example result in files copied into an image not being accessible when the container image runs as a non `root` user. To workaround this limitation with `imgpkg`, Educates copies permissions from the user (except for write permission) to group and other in situations where `imgpkg`, including via `vendir`, is used.

Note that the underlying issue was in part addressed in `imgpkg` version 0.28.0, but the parent directory permissions were still not being fixed properly so the adjustment is still being performed for now.

### Downloading workshops from GitHub

Educates allows the workshop content files to be downloaded from GitHub by setting `spec.content.files` in the workshop definition to `github.com/organization/repository:branch`. If `branch` is left off then `master` branch would be tried first, and then `main`. GitHub however changed behaviour such that downloading the tar ball for `master` would redirect to that for `main` if `master` wasn't a valid branch. When unpacking this tar ball, the root directory would be `main` and not the expected `master`, causing a failure. To workaround this change in GitHub, Educates now tries `main` before `master` if no branch is explicitly provided.

Note that despite this fix specifying workshop content to download by specifying `spec.content.files` is now deprecated and you should use `spec.workshop.files` and the `vendir` based download mechanism instead.
### Binding of system service ports

A workshop specifying `anyuid` for the session namespace security policy was able to run pods as `root`, however they weren't able to bind low numbered system service ports. This restriction has been removed, allowing a pod running as `root` to bind port 80 for HTTP web servers.

Note that security policy name `anyuid` is now deprecated and you should use `baseline` instead.

### Custom namespace resource quotas

When using a `custom` resource budget for namespaces in order to define `ResourceQuotas` explicitly for the session namespaces, the code which verified that the resource quotas had been accepted by the API server and acknowledged by updating the hard quota in the status, was failing because code hadn't been updated correctly to the newer Kubernetes Python client API. Use of `custom` for the `budget` should now work correctly.

### Redirection to the web site root

If a direct link to a workshop session is saved away or shared with another user, and later used to access the training portal when there is no active login, the user would be redirected to the login page even if the training portal had anonymous access enabled. In this situation the user will instead now be redirected initially to the root of the web site. In this case if anonymous access is enabled they should then be redirected to the workshop catalog, or if an event code or login is necessary, they will be directed to the appropriate page to login.

Note that there may still be some corner cases where this redirection isn't occuring. The issue is still being monitored and investigated further.

### Missing session ID variable

The `session_id` variable could be used within the `Workshop` definition, but was not available for use as a data variable in workshop instructions or as an environment variable in the workshop container shell environment.

### Workshop updates in portal

Fixed a race condition in training portal where would try and check the workshop environment to see whether the workshop definition had changed, but the workshop environment hadn't actually been linked to the workshop environment yet. This resulted in an unhandled Python exception being logged, but the training portal process would recover and still proceed. The fix avoids the unnecessary noise from exceptions being logged.

### Initial sessions in portal

The ability to override the initial number of sessions created was not functioning correctly when a number was also specified for number of reserved sessions.

In the first instance, if the training portal definition specified that the number of initial sessions that should be created is `0`, but a non zero number was specified for reserved sessions, no reserved sessions were meant to be created initially, with the reserved sessions only being created for a specific workshop after the first request for that workshop was received. This was working at the outset when the training portal was configured, but the periodic task that ran to reconcile number of workshops sessions, was starting up the reserved sessions the first time it ran. The periodic task will now only create the required reserved sessions if at least one workshop session for that workshop has been created.

In the second instance, if the number of initial sessions was set higher than the number of reserved sessions, it was being capped at the latter, whereas the initial creation of more should have been allowed so long as the capacity wasn't exceeded. The limit on the number of reserved sessions should only have been applied after the number of initially available sessions dropped below the reserved number. The initial number of sessions can now be set higher than reserved and the periodic task which maintains the correct number will not delete these initial excess sessions.

### Update Ingress API group

Ingresses created were using deprecated `networking.k8s.io/v1beta1` API group. This has been updated to `networking.k8s.io/v1` so that Educates will work with Kubernetes 1.22+.

### Install shasum in workshop image

The `shasum` program is now installed into the workshop base image. This is required such that Carvel tools can be installed within a workshop session, even though the Carvel tools are already provided in the workshop base image.

### Setup script and shell environments

In Educates 1.X any `profile.d` files were only processed after all `setup.d` files were processed. This meant any environment set up by a `profile.d` file corresponding to a `setup.d` file wasn't available to `setup.d` files run in a subsequent phase. This meant that `setup.d` files may not work if they were dependent on configuration generated as a side effect of a prior initialization step. Further, if files were provided in `workshop/profile.d` directory, they were not being applied at all where workshop content was downloaded when the container started, where as they were if they had been built into a workshop image.

In Educates 2.0.0 the `profile.d` files corresponding to a certain phase of configuration are processed and incorporated into the shell environment before processing `setup.d` files for a subsequent phase. Handling of any `profile.d` files, including those in `workshop/profile.d` are now also only done after workshop content has been downloaded at runtime into the workshop container.

Note that a consequence of the changes required to fix this is that if during debugging of a workshop container you run `kubectl exec` to get access to the workshop container, the shell environment isn't setup unless you explicitly run `bash -l` as the command to `kubectl exec`. This is because the `profile.d` files will only be processed if a login shell is used.

### Expiring workshop sessions

The training portal admin pages provided a mechanism to select workshop sessions and expire them. In the case of a workshop session which hadn't yet been allocated this wasn't actually doing anything. This has been fixed now to immediately delete any unallocated workshop session.

### Mirror registry storage

When an image registry mirror was configured, if a storage user and storage group were both specified, indicating that the persistent volume file system permissions needed to be fixed using a special init container, this wasn't being done.
