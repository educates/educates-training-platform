(workshop-definition)=
Workshop Definition
===================

The ``Workshop`` custom resource defines a workshop.

The raw custom resource definition for the ``Workshop`` custom resource can be viewed by running:

```
kubectl get crd/workshops.training.educates.dev -o yaml
```

Workshop title and description
------------------------------

Each workshop is required to provide the ``title`` and ``description`` fields. If the fields are not supplied, the ``Workshop`` resource will be rejected when you attempt to load it into the Kubernetes cluster.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-markdown-sample
spec:
  title: Markdown Sample
  description: A sample workshop using Markdown
```

The ``title`` field should be a single line value giving the subject of the workshop.

The ``description`` field should be a longer description of the workshop.

The following optional information can also be supplied for the workshop.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-markdown-sample
spec:
  title: Markdown Sample
  description: A sample workshop using Markdown
  url: https://github.com/educates/lab-markdown-sample
  difficulty: beginner
  duration: 15m
  vendor: educates.dev
  authors:
  - John Smith
  tags:
  - template
  labels:
  - name: id
    value: educates.dev/lab-markdown-sample
  logo: data:image/png;base64,....
```

The ``url`` field should be a URL you can go to for more information about the workshop.

The ``difficulty`` field should give an indication of who the workshop is targeting. The value must be one of ``beginner``, ``intermediate``, ``advanced`` and ``extreme``.

The ``duration`` field gives the expected maximum amount of time the workshop would take to complete. The format of the field is an integer number with ``s``, ``m``, or ``h`` suffix. This field only provides informational value and is not used directly to police how long a workshop instance will last. The `educates` CLI if used to deploy a workshop, will however copy this value into the `expires` field for the workshop when configuring the training portal definition, thus setting the actual expiration time value for the workshop. For more control over the workshop duration and the ability to extend it, it needs to be configured in the training portal, or using the command line options of the `educates` CLI when deploying the workshop.

The ``vendor`` field should be a value which identifies the company or organization which the authors are affiliated with. This could be a company or organization name, or a DNS hostname under the control of whoever has created the workshop.

The ``authors`` field should list the people who worked on creating the workshop.

The ``tags`` and ``labels`` fields can be used to help identify what the workshop is about. This might be used in a searchable catalog of workshops. Tags can consist of literal string values for categorization, and labels mappings between a key and value which might be used for searching based on arbitrary properties or for identification.

The ``logo`` field should be a graphical image provided in embedded data URI format which depicts the topic of the workshop. The image should be 400 by 400 pixels. This might be used in a searchable catalog of workshops.

Note that when referring to a workshop definition after it has been loaded into a Kubernetes cluster, the value of ``name`` field given in the metadata is used. If you want to play around with slightly different variations of a workshop, copy the original workshop definition YAML file and change the value of ``name``. Then make your changes and load it into the Kubernetes cluster.

A note about use of data variables
----------------------------------

Beyond the basic meta data for a workshop there are many different sections for configuring the workshop environment and individual sessions. This includes how and where to download workshop content from.

In many of the sections which define the workshop, special data variables can be referenced in configuration values and various examples will be shown throughout this documentation. These will appear in the form:

```
$(variable_name)
```

The value of the data variable will be automatically expanded when applying the configuration.

These data variables fall into the following categories.

Session data variables are the set of data variables which can be used in configuration sections which customize a specific workshop session. The core data variables in this category are:

* ``assets_repository`` - The host name of the workshop environment assets repository when enabled.
* ``cluster_domain`` - The internal domain used by the Kubernetes cluster, usually ``cluster.local``.
* ``config_password`` - A unique random password value for use when accessing the workshop session configuration.
* ``environment_name`` - The name of the workshop environment.
* ``image_repository`` - The host name of the image repository associated with the cluster or training portal for image storage.
* ``ingress_class`` - The ingress class which Educates is configured to use for all ingress.
* ``ingress_domain`` - The domain which should be used in the any generated hostname of ingress routes for exposing applications.
* ``ingress_port`` - The port number for the workshop session ingress, usually port 80 or 443, but for docker deployment can be different.
* ``ingress_port_suffix`` - The port number (with colon prefix) for the workshop session ingress. Will however be empty when standard ports of 80 or 443.
* ``ingress_protocol`` - The protocol (http/https) that is used for ingress routes which are created for workshops.
* ``ingress_secret`` - The name of the Kubernetes secret containing the wildcard TLS certificate for use with ingresses.
* ``oci_image_cache`` - The hostname of the workshop environment OCI image cache when enabled.
* ``platform_arch`` - The CPU architecture the workshop container is running on, ``amd64`` or ``arm64``.
* ``service_account`` - The name of the service account in the workshop namespace that the workshop session pod is deployed as.
* ``services_password`` - A unique random password value for use with arbitrary services deployed with a workshop.
* ``session_hostname`` - The host name of the workshop session instance.
* ``session_id`` - The short identifier for the workshop session. Is only unique in the context of the associated workshop environment.
* ``session_name`` - The name of the workshop session. Is unique within the context of the Kubernetes cluster the workshop session is hosted in.
* ``session_namespace`` - When session has access to a shared Kubernetes cluster, the name of the namespace the workshop instance is linked to and into which any deployed applications will run.
* ``session_url`` - The full URL for accessing the workshop session instance dashboard.
* ``ssh_keys_secret`` - The name of the Kubernetes secret containing the SSH keys for the workshop session.
* ``ssh_private_key`` - The private part of a unique SSH key pair generated for the workshop session.
* ``ssh_public_key`` - The public part of a unique SSH key pair generated for the workshop session.
* ``storage_class`` - The storage class which Educates is configured to use for all storage.
* ``training_portal`` - The name of the training portal the workshop is being hosted by.
* ``workshop_environment_uid`` - The resource ``uid`` for the ``WorkshopEnvironment`` resource for a workshop instance.
* ``workshop_image`` - The image used to deploy the workshop container.
* ``workshop_image_pull_policy`` - The image pull policy of the image used to deploy the workshop container.
* ``workshop_name`` - The name of the workshop.
* ``workshop_namespace`` - The name of the namespace used for the workshop environment.
* ``workshop_session_uid`` - The resource ``uid`` for the ``WorkshopSession`` resource for a workshop session.
* ``workshop_version`` - The version tag from the workshop image when a published workshop is being deployed. If not known will be set to ``latest``.

Note that ``session_name`` was only added in Educates version 2.6.0. In prior versions ``session_namespace`` was used as a general identifier for the name of the session when in practice it identified the name of the namespace the workshop instance had access to when it was able to make use of the same Kubernetes cluster the workshop instance was deployed to. Since Educates supports configurations where there is no access to a Kubernetes cluster, or a distinct Kubernetes cluster was used with full admin access, the naming made no sense so ``session_name`` was added. As such, if needing an identifier for the name of the session, use ``session_name``. Only use ``session_namespace`` when needing to refer to the actual namespace in a Kubernetes cluster which the session may be associated with. Although the values of each are currently the same, in the future ``session_namespace`` will at some point start to be set to an empty string when there is no associated Kubernetes namespace.

When specific application features are enabled, additional data variables may be available for use. These will be listed in sections dealing with the specific application features.

Environment data variables are the set of data variables which can be used in configuration sections which customize a specific workshop environment. The subset of data variables from above which are in this category are:

* ``assets_repository``
* ``cluster_domain``
* ``environment_name``
* ``image_repository``
* ``ingress_class``
* ``ingress_domain``
* ``ingress_port``
* ``ingress_port_suffix``
* ``ingress_protocol``
* ``ingress_secret``
* ``oci_image_cache``
* ``platform_arch``
* ``service_account``
* ``storage_class``
* ``training_portal``
* ``workshop_image``
* ``workshop_image_pull_policy``
* ``workshop_name``
* ``workshop_namespace``
* ``workshop_version``

A further subset of these will be available in the specific case where configuration is for downloads done as part of the workshop environment setup. These are:

* ``assets_repository``
* ``cluster_domain``
* ``environment_name``
* ``image_repository``
* ``ingress_domain``
* ``ingress_port_suffix``
* ``ingress_protocol``
* ``oci_image_cache``
* ``platform_arch``
* ``training_portal``
* ``workshop_name``
* ``workshop_namespace``
* ``workshop_version``

Request data variables are the set of data variables which can be used in configuration sections which are used to create resources at the time of a workshop session being allocated to a workshop user. These consist of the session data variables, as well as additional custom variables derived from the request parameters.

(downloading-workshop-content)=
Downloading workshop content
----------------------------

Workshop content can be downloaded at the time the workshop instance is created with it being overlayed on a selected workshop base image, or the workshop content can be added into a container image built from a workshop base image.

To download workshop content when a workshop instance starts up, the ``vendir`` tool from Carvel is used. The configuration for ``vendir`` should be included under ``spec.workshop.files``. The format of configuration supplied needs to match the [configuration](https://carvel.dev/vendir/docs/v0.25.0/vendir-spec/) that can be supplied under ``directories.contents`` of the ``Config`` resource used by ``vendir``, with the exception that if ``path`` is not supplied it will default to ``.``.

The ``vendir`` tool supports a range of sources for downloading content, including:

* OCI image artefacts stored in an image repository.
* A hosted Git source repository, such as GitHub and Gitlab.
* Inline file definitions and files sourced from Kubernetes secrets.
* Files associated with a GitHub repository release.
* Files downloading from a HTTP web server.

If bundling workshop content into a container image built from a workshop base image, the location of the image can be specified by setting ``spec.workshop.image``.

(hosting-on-an-image-repository)=
Hosting on an image repository
------------------------------

The preferred method for hosting workshop content is to create an OCI image artefact containing the workshop content and host it on an image repository. If this method is used, it will make it possible later to bundle up workshops, relocate them, and use them in disconnected environments.

If you use the workshop templates provided by the Educates project and are using GitHub to store your workshop files, the GitHub action created by the workshop template will automatically create the required OCI image artefact each time you tag the GitHub repository with a specific version, and publish it to the GitHub container registry. The GitHub action will also automatically create a GitHub release with the workshop definition attached, which has been rewritten so that the published OCI image artefact is used. If using the ``educates`` CLI you can also use it to publish the workshop image.

The initial format of ``spec.workshop.files`` created from the workshop templates will be:

```yaml
spec:
  workshop:
    files:
    - image:
        url: $(image_repository)/{name}-files:$(workshop_version)
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
```

The ``$(image_repository)`` and ``$(workshop_version)`` data variables reference in the ``workshop.files.image.url`` property is special to the workflow for working on workshop content using the local Educates environment discussed in the getting started section of the documentation. This will be rewritten by the GitHub action or ``educates`` CLI when a workshop is published, with it replaced with an explicit reference to the GitHub container registry organization used to publish the OCI image artefact containing the workshop content.

The ``{name}`` reference in the same property, in the case of using GitHub and relying on the supplied GitHub actions to publish the workshop content, must be the name of the Git repository. If creating the initial workshop content using the workshop templates, this will be set for you. For the GitHub action to work the ``-files`` suffix to the name must also be used, with it distinguishing the OCI image artefact as being for the workshop content files, as distinct from a custom workshop image for the same workshop.

If not using the workshop templates, local Educates environment, or relying on the GitHub actions workshop to publish the workshop content, but want to use an OCI image artefact to publish the workshop content, set the ``workshop.files.image.url`` property to the location of where you have published the OCI image artefact.

As ``vendir`` is used to download and unpack the OCI image artefact, under ``workshop.files`` for the ``image`` source type you can also supply additional options, including:

* ``includePaths`` - Specify what paths should be included from the OCI image artefact when unpacking.
* ``excludePaths`` - Specify what paths should be excluded from the OCI image artefact when unpacking.
* ``newRootPath`` - Specify the directory path within the OCI image artefact that should be used as the root for the workshop files.

If credentials are required to access the image repository, these can be supplied via a Kubernetes secret in your cluster. The ``environment.secrets`` property list should designate the source for the secret. The secret will then be copied into the workshop namespace and automatically injected into the container and passed to ``vendir`` when it is run, so long as the configuration for ``vendir`` has an appropriate ``secretRef`` property with the name of the secret.

```yaml
spec:
  workshop:
    files:
    - image:
        url: $(image_repository)/{name}-files:$(workshop_version)
        secretRef:
          name: pull-secret
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
  environment:
    secrets:
    - name: pull-secret
      namespace: default
```

For more details and other options see the ``vendir`` [documentation](https://carvel.dev/vendir/docs/latest/vendir-spec/).

(publishing-of-workshop-content)=
Publishing of workshop content
------------------------------

When using an OCI image artefact to hold workshop content it is expected that ``imgpkg`` from the Carvel project is used to create it. This can be done indirectly using the Educates GitHub action for publishing workshops, or the ``educates`` CLI using the ``educates publish-workshop`` command.

When using the ``educates publish-workshop`` command it is possible to customize what is included in the OCI artefact image by providing a specification of what to include in the workshop definition. At the minimum this must include ``publish.image``, which defines where the image is to be published.

```yaml
spec:
  publish:
    image: $(image_repository)/lab-hugo-workshop-files:$(workshop_version)
```

In this case the ``$(image_repository)`` and ``$(workshop_version)`` data variables references are special to the workflow for working on and publishing workshop content using the ``educates`` CLI. However, even if not using the local image registry this format should still be used. The values can be overridden using command line options when using ``educates publish-workshop``.

To customize what is included in the OCI artefact image, a ``publish.files`` section should be provided. Like with ``workshop.files`` this is a ``vendir`` configuration snippet but instead of downloading files from a remote location, it is used to construct the content to be included from the local file system. The equivalent for the default behaviour of packaging up the whole workshop directory would be:

```yaml
spec:
  publish:
    image: $(image_repository)/lab-hugo-workshop-files:$(workshop_version)
    files:
    - directory:
        path: .
```

To include only select directories or files you can use ``includePaths``.

```yaml
spec:
  publish:
    image: $(image_repository)/lab-hugo-workshop-files:$(workshop_version)
    files:
    - directory:
        path: .
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
```

The ``vendir`` program will be run with this configuration to make a copy of the files into a temporary area and that will then be packaged up using ``imgpkg``.

Because ``vendir`` is used, if desired, files could also be downloaded from remote sources to be incorporated into the workshop OCI image artefact.

When running ``educates publish-workshop``, if the ``--export-workshop`` option is provided along with an output file name, a modified version of the workshop definition be output which has been rewritten to use the location to which the workshop has been published. This modified version of the workshop definition can then be used to deploy the workshop from its published location.

(hosting-using-a-git-repository)=
Hosting using a Git repository
------------------------------

If not using GitHub, don't want to rely on the GitHub actions for publishing workshop content as an OCI image artefact, or just don't want to deal with OCI image artefacts as a publishing mechanism for workshop content, you can instead configure workshop content to be downloaded from a hosted Git repository.

The format of ``spec.workshop.files`` for downloading workshop content in this case would be:

```yaml
spec:
  workshop:
    files:
    - git:
        url: https://github.com/{organization}/{repository}
        ref: origin/main
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
```

If not using GitHub, but another Git repository service such as GitLab or a self hosted enterprise version of a Git hosting service, replace ``github.com`` with the hostname of your Git server.

The ``{organization}`` reference in the ``workshop.files.git.url`` property should be replaced with the name of your account or the organization being used. The ``{repository}`` reference should be replaced with the name of Git repository. You must specify ``workshop.files.git.ref`` with an appropriate Git reference. This can describe a branch, commit, or tag.

As ``vendir`` is used to download files from the Git repository, under ``workshop.files`` for the ``git`` source type you can also supply additional options, including:

* ``includePaths`` - Specify what paths should be included from the Git repository when unpacking.
* ``excludePaths`` - Specify what paths should be excluded from the Git repository when unpacking.
* ``newRootPath`` - Specify the directory path within the Git repository that should be used as the root for the workshop files.

If credentials are required to access the Git repository, these can be supplied via a Kubernetes secret in your cluster. The ``environment.secrets`` property list should designate the source for the secret, with the secret then being copied into the workshop namespace and automatically injected into the container and passed to ``vendir`` when it is run.

For more details and other options see the ``vendir`` [documentation](https://carvel.dev/vendir/docs/latest/vendir-spec/).

(hosting-using-a-http-server)=
Hosting using a HTTP server
---------------------------

In addition to hosting workshop files as an OCI image artifact on an image registry, or in a hosted Git repository, ``vendir`` supports sourcing files from a range of other services. The other main source for files is downloading them from a HTTP server.

```yaml
spec:
  workshop:
    files:
    - http:
        url: https://example.com/workshop.tar.gz
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
```

Although ``vendir`` will automatically unpack any archive file by default, it is currently limited in that it will not restore execute permissions on files extracted from a tar/zip archive. Educates will restore execute permissions on ``setup.d`` scripts, but if you have other files which have execute permissions, you will need to supply a ``setup.d`` to restore those execute permissions.

For more details and other options see the ``vendir`` [documentation](https://carvel.dev/vendir/docs/latest/vendir-spec/).

Content download (deprecated)
-----------------------------

Downloading workshop content using ``workshop.files`` is a new mechanism which replaces a now deprecated older mechanism using ``content.files``. Use of ``vendir`` provides greater flexibility and more options for where workshop content can be downloaded. There are a still a few corner cases where ``vendir`` can't be used to replace the older way of downloading content. Issues have been raised against ``vendir`` to have these shortcomings fixed, but in the interm you can still use ``content.files`` if necessary.

To download workshop content using ``content.files`` set the field to the location of the workshop content.

```yaml
spec:
  content:
    files: github.com/educates/lab-markdown-sample
```

The location can be a GitHub or GitLab repository reference, a URL to a tarball hosted on a HTTP server, or a reference to an OCI image artifact on a registry.

In the case of a GitHub or GitLab repository, do not prefix the location with ``https://`` as a symbolic reference is being used and not an actual URL.

The format of the reference to a GitHub or GitLab repository is similar to that used with kustomize when referencing remote repositories. For example:

* ``github.com/organization/project`` - Use the workshop content hosted at the root of the GitHub repository. The ``master`` or ``main`` branch is used.
* ``github.com/organization/project/subdir?ref=develop`` - Use the workshop content hosted at ``subdir`` of the GitHub repository. The ``develop`` branch is used.
* ``gitlab.com/organization/project`` - Use the workshop content hosted at the root of the GitLab repository. The ``master`` branch is used.
* ``gitlab.com/organization/project/subdir?ref=develop`` - Use the workshop content hosted at ``subdir`` of the GitLab repository. The ``develop`` branch is used.

In the case of a URL to a tarball hosted on a HTTP server, the URL can be in the following formats:

* ``https://example.com/workshop.tar`` - Use the workshop content from the top level directory of the unpacked tarball.
* ``https://example.com/workshop.tar.gz`` - Use the workshop content from the top level directory of the unpacked tarball.
* ``https://example.com/workshop.tar?path=subdir`` - Use the workshop content from the specified sub directory path of the unpacked tarball.
* ``https://example.com/workshop.tar.gz?path=subdir`` - Use the workshop content from the specified sub directory path of the unpacked tarball.

The tarball referenced by the URL can be uncompressed or compressed.

If using GitHub, instead of using the earlier form for referencing the Git repository containing the workshop content, you can instead use a URL to refer directly to the downloadable tarball for a specific version of the Git repository.

* ``https://github.com/organization/project/archive/develop.tar.gz?path=project-develop``

When using this form you must reference the ``.tar.gz`` download and cannot use the ``.zip`` file. The base name of the tarball file is the branch or commit name. You must specify the ``path`` query string parameter where the argument is the name of the project and branch or commit. The path needs to be supplied as the contents of the repository is not returned at the root of the archive.

If using GitLab, it also provides a means of download a package as a tarball.

* ``https://gitlab.com/organization/project/-/archive/develop/project-develop.tar.gz?path=project-develop``

For HTTP servers which need HTTP basic authentication, credentials can be provided in the URL:

* ``https://username@token:example.com/workshop.tar.gz``

Be aware that these credentials will be visible to a workshop user. When working with sources of workshop content that require authentication you should use ``content.downloads`` instead.

The last case is a reference to an OCI image artifact stored on a registry. This is not a full container image with operating system, but an image containing just the files making up the workshop content. The URI formats for this is:

* ``imgpkg+https://harbor.example.com/organization/project:version`` - Use the workshop content from the top level directory of the unpacked OCI artifact. The registry in this case must support ``https``.
* ``imgpkg+https://harbor.example.com/organization/project:version?path=subdir`` - Use the workshop content from the specified sub directory path of the unpacked OCI artifact. The registry in this case must support ``https``.
* ``imgpkg+http://harbor.example.com/organization/project:version`` - Use the workshop content from the top level directory of the unpacked OCI artifact. The registry in this case can support only ``http``.
* ``imgpkg+http://harbor.example.com/organization/project:version?path=subdir`` - Use the workshop content from the specified sub directory path of the unpacked OCI artifact. The registry in this case can support only ``http``.

Instead of the prefix ``imgpkg+https://``, you can instead use just ``imgpkg://``. The registry in this case must still support ``https``.

For any of the formats, credentials can be supplied as part of the URI.

* ``imgpkg+https://username:password@harbor.example.com/organization/project:version``

Access to the registry using a secure connection using ``https`` must have a valid certificate, except for the case where the registry uses a ``.local`` address.

As with supplying credentials to HTTP servers, these credentials will be visible to the workshop users.

In all cases for downloading workshop content using ``content.files`` if you want files to be ignored and not included in what the user can see, you can supply a ``.eduk8signore`` file in your repository or tarball and list patterns for the files in it.

Note that the contents of the ``.eduk8signore`` file is processed as a list of patterns and each will be applied recursively to subdirectories. To ensure that a file is only ignored if it resides in the root directory, you need to prefix it with ``./``.

```text
./.dockerignore
./.gitignore
./Dockerfile
./LICENSE
./README.md
./kustomization.yaml
./resources
```

Container image for the workshop
--------------------------------

When using a custom workshop base image, the ``workshop.image`` field should specify the image reference identifying the location of the container image to be deployed for the workshop instance.

If you are making use of the local Educates environment when creating workshop content, and subsequently using GitHub to host your workshop content, and GitHub actions to publish the workshop, this should be specified in the form:

```yaml
spec:
  workshop:
    image: $(image_repository)/{name}-image:$(workshop_version)
    files:
    - image:
        url: $(image_repository)/{name}-files:$(workshop_version)
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
```

As for ``workshop.files.image.url``, the ``$(image_repository)`` and ``$(workshop_version)`` data variables reference in the ``workshop.image`` property is special to the workflow for working on workshop content using the local Educates environment discussed in the getting started section of the documentation. This will be rewritten by the GitHub action or ``educates`` CLI when a workshop is published, with it replaced with an explicit reference to the GitHub container registry organization used to publish the custom workshop image.

The ``{name}`` reference in the same property, in the case of using GitHub and relying on the supplied GitHub actions to publish the workshop content, must be the name of the Git repository. For the GitHub action to work the ``-image`` suffix to the name must also be used, with it distinguishing the custom workshop base image, as being distinct from an OCI image artefact containing just the workshop content.

If not using the workflow provided by using the local Educates environment and the GitHub actions when hosting workshop content on GitHub, ``workshop.images`` should be set to wherever you are hosting the custom workshop base image.

```yaml
spec:
  workshop:
    image: ghcr.io/{organization}/{image}:latest
```

Note that if you use any of the version tags ``:main``, ``:master``, ``:develop`` and ``:latest``, the Educates operator will set the image pull policy to ``Always`` to ensure that a newer version is always pulled if available, otherwise the image will be cached on the Kubernetes nodes and only pulled when it is initially not present. Any other version tags will always be assumed to be unique and never updated. Do though be aware of image registries which use a CDN as front end. When using these image tags the CDN can still always regard them as unique and they will not do pull through requests to update an image even if it uses a tag of ``:latest``.

Where special custom workshop base images are available as part of the Educates project, instead of specifying the full location for the image, including the image registry, you can specify a short name. The Educates operator will then fill in the rest of the details.

```yaml
spec:
  workshop:
    image: jdk11-environment:*
    files:
    - image:
        url: $(image_repository)/{name}-files:$(workshop_version)
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
```

The short versions of the names which are recognised are:

* ``base-environment:*`` - A tagged version of the ``base-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``jdk8-environment:*`` - A tagged version of the ``jdk8-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``jdk11-environment:*`` - A tagged version of the ``jdk11-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``jdk17-environment:*`` - A tagged version of the ``jdk17-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``jdk21-environment:*`` - A tagged version of the ``jdk121-environment`` workshop image which has been matched with the current version of the Educates operator.
* ``conda-environment:*`` - A tagged version of the ``conda-environment`` workshop image which has been matched with the current version of the Educates operator.

The JDK workshop images are intended for workshops requiring Java. The Conda workshop image is intended for workshops which would benefit from the Anaconda distribution of Python, rather than the standard Python distribution supplied with the operating system image.

Note that in older versions of Educates the location of the custom workshop base image could be specified using ``content.image``. This is now deprecated and ``workshop.image`` should always be used.

(adding-extension-packages)=
Adding extension packages
-------------------------

Creating a custom workshop base image is one way of making additional applications available for a workshop. The drawback of a custom workshop base image is that it needs to build from a specific version of the standard workshop base image. As new releases of Educates are made available, if the custom workshop image is not continually rebuilt against the newest workshop base image, it may stop working if changes are made in Educates that require the newest workshop image be used. Use of custom workshop base images is therefore discouraged as a general rule, although if the size of the applications required is large, can be the only choice as downloading applications at the start of every session could take too long.

If using the standard workshop base image, rather than a custom workshop base image, and you want to download additional applications or files required for a workshop when the workshop session is created, you can use a workshop ``setup.d`` script. Having installed the additional applications, a workshop ``profile.d`` script file can be used to set up the shell environment so the applications can be found, or to set any special environment variables which may be required when using the applications.

Where a set of applications are common and used in more than one workshop, having to have a separate copy of the ``setup.d`` and ``profile.d`` scripts in every workshop is not ideal. This is because the multiple copies makes it hard to ensure they are kept up to date, if how applications are downloaded needs to change, or if the versions of any applications need to be updated.

To try and simplify the process of adding additional applications, an extension package feature is available. This relies on ``vendir`` and can be used to download applications as pre-created bundles from an image repository, with the package also containing any ``setup.d`` and ``profile.d`` scripts which may still be required to configure the package when the workshop session starts.

As an example, installation of extension package which adds additional command line tools to a workshop session might be configured using:

```yaml
spec:
  workshop:
    packages:
    - name: tce
      files:
      - image:
          url: ghcr.io/educates/educates-extension-packages/tce-0.12:sha-5f9081f
```

When a package is installed it is placed under a sub directory of ``/opt/packages`` with name corresponding to the ``name`` field in the ``packages`` configuration. Any setup scripts contained in the ``setup.d`` directory of the installed package will be run when the workshop session starts, with the shell environment being configured using any scripts in the ``profile.d`` of the installed package.

In this example ``vendir`` was being used to download an OCI image artefact, but other mechanisms ``vendir`` provides can also be used when downloading remote files. This includes from Git repositories and HTTP web servers. Any configuration for ``vendir`` should be included under ``spec.packages.files``. The format of configuration supplied needs to match the [configuration](https://carvel.dev/vendir/docs/v0.25.0/vendir-spec/) that can be supplied under ``directories.contents`` of the ``Config`` resource used by ``vendir``.

Note that although ``vendir`` will automatically unpack any archive file by default, it is currently limited in that it will not restore execute permissions on files extracted from a tar/zip archive. Educates will restore execute permissions on ``setup.d`` scripts, but if you have other files which have execute permissions, you will need to supply a ``setup.d`` to restore those execute permissions.

If credentials are required to access any remote server, these can be supplied via a Kubernetes secret in your cluster. The ``environment.secrets`` property list should designate the source for the secret, with the secret then being copied into the workshop namespace and automatically injected into the container and passed to ``vendir`` when it is run.

For a number of extension packages being maintained by the Educates team see:

* [https://github.com/educates/educates-extension-packages](https://github.com/educates/educates-extension-packages)

Setting environment variables
-----------------------------

If you want to set or override environment variables for the workshop instance, you can supply the ``session.env`` field.

```yaml
spec:
  session:
    env:
    - name: REPOSITORY_URL
      value: https://github.com/educates/lab-markdown-sample
```

The ``session.env`` field should be a list of dictionaries with a ``name`` field giving the name of the environment variable.

For the value of the environment variable, an inline value can be supplied using the ``value`` field.

Values of fields in the list of resource objects can reference any session data variables.

In place of ``value``, one can also supply a ``valueFrom`` field. This can be used to reference a specific data value from a Kubernetes secret or config map. The ``valueFrom`` definition uses the same structure as used for setting environment variables using this mechanism in a Kubernetes pod.

```yaml
spec:
  session:
    env:
    - name: SSO_USERNAME
      valueFrom:
        secretKeyRef:
          name: $(session_name)-request
          key: username
```

As with a Kubernetes pod, one can also use ``valueFrom`` to set the value of the environment variable with values sourced using the Kubernetes downward API.

In the case where you want to inject all data values from a secret or config map and there is no requirement to override the name of the environment variable created, instead of using ``env`` and ``valueFrom``, you can use ``envFrom``.

```yaml
spec:
  session:
    envFrom:
      secretKeyRef:
        name: $(session_name)-request
```

Note that the ability to override environment variables using this field should be limited to cases where they are required for the workshop. If you want to set or override an environment for a specific workshop environment, use the ability to set environment variables in the ``WorkshopEnvironment`` custom resource for the workshop environment instead.

(overriding-the-memory-available)=
Overriding the memory available
-------------------------------

By default the container the workshop environment is running in is allocated 512Mi. If the editor is enabled a total of 1Gi is allocated.

Where the purpose of the workshop is mainly aimed at deploying workloads into the Kubernetes cluster, this would generally be sufficient. If you are running workloads in the workshop environment container itself and need more memory, the default can be overridden by setting ``memory`` under ``session.resources``.

```yaml
spec:
  session:
    resources:
      memory: 2Gi
```

(mounting-a-persistent-volume)=
Mounting a persistent volume
----------------------------

In circumstances where a workshop needs persistent storage to ensure no loss of work if the workshop environment container were killed and restarted, you can request a persistent volume be mounted into the workshop container.

```yaml
spec:
  session:
    resources:
      storage: 5Gi
```

If instead of declaring the size for storage, you want to create the persistent volume claim yourself, for example as part of ``session.objects``, you can specify the name of the separate persistent volume claim. This may include a ``subPath`` within the persistent volume you want to use.

```yaml
spec:
  session:
    resources:
      volume:
        name: $(session_name)-workshop
        subPath: storage
```

The persistent volume will be mounted on top of the ``/home/eduk8s`` directory. Because this would hide any workshop content bundled with the image, an init container is automatically configured and run, which will copy the contents of the home directory to the persistent volume, before the persistent volume is then mounted on top of the home directory.

Mounting arbitrary volume types
-------------------------------

Where secrets or configmaps are injected into the workshop environment, or for specific workshops sessions, these can be mounted into the workshop container by declaring standard Kubernetes ``volumes`` and ``volumeMounts`` definitions.

```yaml
spec:
  session:
    volumes:
    - name: request-secret
      secret:
        secretName: $(session_name)-request
    volumeMounts:
    - name: request-secret
      mountPath: /opt/request-secret
```

Care should be taken in naming volumes and where they are mounted to avoid clashes with names used internally by Educates.

In addition to secrets and configmaps these can be used to mount different types of persistent storage as well.

Note that ``volumeMounts`` are only added to the main workshop container. If mounting of a volume into a side car container was necessary for some purpose, then ``patches`` would need to be used to apply a patch against the complete workshop pod spec.

(resource-budget-for-namespaces)=
Resource budget for namespaces
------------------------------

In conjunction with each workshop instance, a namespace will be created for use during the workshop. That is, from the terminal of the workshop dashboard applications can be deployed into the namespace via the Kubernetes REST API using tools such as ``kubectl``.

By default there are no limits or quotas. To control how much resources can be used you can set a resource budget for any namespaces created for the workshop instance.

To set the resource budget, set the ``session.namespaces.budget`` field.

```yaml
spec:
  session:
    namespaces:
      budget: small
```

The resource budget sizings and quotas for CPU and memory are:

```text
| Budget    | CPU   | Memory |
|-----------|-------|--------|
| small     | 1000m | 1Gi    |
| medium    | 2000m | 2Gi    |
| large     | 4000m | 4Gi    |
| x-large   | 8000m | 8Gi    |
| xx-large  | 8000m | 12Gi   |
| xxx-large | 8000m | 16Gi   |
```

A value of 1000m is equivalent to 1 CPU.

Separate resource quotas for CPU and memory are applied for terminating and non terminating workloads.

Only the CPU and memory quotas are listed above, but limits are also in place on the number of resource objects that can be created of certain types, including persistent volume claims, replication controllers, services and secrets.

For each budget type, a limit range is created with fixed defaults. The limit ranges for CPU usage on a container are as follows.

```text
| Budget    | Min | Max   | Request | Limit |
|-----------|-----|-------|---------|-------|
| small     | 50m | 1000m | 50m     | 250m  |
| medium    | 50m | 2000m | 50m     | 500m  |
| large     | 50m | 4000m | 50m     | 500m  |
| x-large   | 50m | 8000m | 50m     | 500m  |
| xx-large  | 50m | 8000m | 50m     | 500m  |
| xxx-large | 50m | 8000m | 50m     | 500m  |
```

Those for memory are:

```text
| Budget    | Min  | Max  | Request | Limit |
|-----------|------|------|---------|-------|
| small     | 1M   | 1Gi  | 128Mi   | 256Mi |
| medium    | 1M   | 2Gi  | 128Mi   | 512Mi |
| large     | 1M   | 4Gi  | 128Mi   | 512Mi |
| x-large   | 1M   | 8Gi  | 128Mi   | 512Mi |
| xx-large  | 1M   | 12Gi | 128Mi   | 512Mi |
| xxx-large | 1M   | 16Gi | 128Mi   | 512Mi |
```

The request and limit values are the defaults applied to a container when no resources specification is given in a pod specification.

If a budget sizing for CPU and memory is sufficient, but you need to override the limit ranges and defaults for request and limit values when none is given in a pod specification, you can supply overrides in ``session.namespaces.limits``.

```yaml
spec:
  session:
    namespaces:
      budget: medium
      limits:
        min:
          cpu: 50m
          memory: 32Mi
        max:
          cpu: 1
          memory: 1Gi
        defaultRequest:
          cpu: 50m
          memory: 128Mi
        default:
          cpu: 500m
          memory: 1Gi
```

Although all possible properties that can be set are listed in this example, you only need to supply the property for the value you want to override.

If you need more control over limit ranges and resource quotas, you should set the resource budget to ``custom``. This will remove any default limit ranges and resource quota which might be applied to the namespace. You can then specify your own ``LimitRange`` and ``ResourceQuota`` resources as part of the list of resources created for each session.

Before disabling the quota and limit ranges, or contemplating any switch to using a custom set of ``LimitRange`` and ``ResourceQuota`` resources, consider if that is what is really required. The default requests defined by these for memory and CPU are fallbacks only. In most cases instead of changing the defaults, you should specify memory and CPU resources in the pod template specification of your deployment resources used in the workshop, to indicate what the application actually requires. This will allow you to control exactly what the application is able to use and so fit into the minimum quota required for the task.

Note that this budget setting and the memory values are distinct from the amount of memory the container the workshop environment runs in. If you need to change how much memory is available to the workshop container, set the ``memory`` setting under ``session.resources``.

Creation of session resources
-----------------------------

When a workshop instance is created, the deployment running the workshop dashboard is created in the namespace for the workshop environment. When more than one workshop instance is created under that workshop environment, all those deployments are in the same namespace.

For each workshop instance, a separate empty namespace is created with name corresponding to the workshop session. The workshop instance is configured so that the service account that the workshop instance runs under can access and create resources in the namespace created for that workshop instance. Each separate workshop instance has its own corresponding namespace and they can't see the namespace for another instance.

If you want to pre-create additional resources within the namespace for a workshop instance, you can supply a list of the resources against the ``session.objects`` field within the workshop definition. You might use this to add additional custom roles to the service account for the workshop instance when working in that namespace, or to deploy a distinct instance of an application for just that workshop instance, such as a private image registry.

```yaml
spec:
  session:
    objects:
    - apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: registry
      spec:
        replicas: 1
        selector:
          matchLabels:
            deployment: registry
        strategy:
          type: Recreate
        template:
          metadata:
            labels:
              deployment: registry
          spec:
            containers:
            - name: registry
              image: registry.hub.docker.com/library/registry:2.6.1
              imagePullPolicy: IfNotPresent
              ports:
              - containerPort: 5000
                protocol: TCP
              env:
              - name: REGISTRY_STORAGE_DELETE_ENABLED
                value: "true"
    - apiVersion: v1
      kind: Service
      metadata:
        name: registry
      spec:
        type: ClusterIP
        ports:
        - port: 80
          targetPort: 5000
        selector:
          deployment: registry
```

Note that for namespaced resources, it is not necessary to specify the ``namespace`` field of the resource ``metadata``. When the ``namespace`` field is not present the resource will automatically be created within the session namespace for that workshop instance.

When resources are created, owner references are added making the ``WorkshopSession`` custom resource corresponding to the workshop instance the owner. This means that when the workshop instance is deleted, any resources will be automatically deleted.

Values of fields in the list of resource objects can reference any of the session data variables which may be appropriate.

In the case of cluster scoped resources, it is important that you set the name of the created resource so that it embeds the value of ``$(session_name)``. This way the resource name is unique to the workshop instance and you will not get a clash with a resource for a different workshop instance.

Resources you include can also be new resources types defined by a custom resource definition (CRD), but the corresponding operator implementing those CRDs will need to be deployed to the cluster that Educates is deployed to. For example, if KubeVirt were available, you could use ``session.objects`` to trigger the creation of a virtual machine for a workshop session.

```yaml
spec:
  session:
    namespaces:
      budget: x-large
      limits:
        max:
          memory: 2.5Gi
    objects:
    - apiVersion: secrets.educates.dev/v1beta1
      kind: SecretCopier
      metadata:
        name: $(session_name)-ssh-keys
      spec:
        rules:
        - sourceSecret:
            name: $(ssh_keys_secret)
            namespace: $(workshop_namespace)
          targetNamespaces:
            nameSelector:
              matchNames:
              - $(session_namespace)
          targetSecret:
            name: ssh-keys
    - apiVersion: kubevirt.io/v1
      kind: VirtualMachine
      metadata:
        name: testing
        labels:
          kubevirt.io/vm: testing
      spec:
        running: true
        template:
          metadata:
            labels:
              kubevirt.io/vm: testing
          spec:
            terminationGracePeriodSeconds: 30
            accessCredentials:
            - sshPublicKey:
                source:
                  secret:
                    secretName: ssh-keys
                propagationMethod:
                  configDrive: {}
            domain:
              cpu:
                cores: 2
              resources:
                limits:
                  memory: 2Gi
                requests:
                  memory: 2Gi
              devices:
                disks:
                - name: disk1
                  disk:
                    bus: virtio
                - disk:
                    bus: virtio
                  name: cloudinitdisk
                interfaces:
                - name: default
                  masquerade: {}
            networks:
            - name: default
              pod: {}
            volumes:
            - name: disk1
              containerDisk:
                image: $(oci_image_cache)/containerdisks/fedora:42
            - name: cloudinitdisk
              cloudInitConfigDrive:
                userData: |-
                  #cloud-config
                  password: $(services_password)
                  chpasswd: { expire: False }
    - apiVersion: v1
      kind: Service
      metadata:
        name: testing
      spec:
        selector:
          kubevirt.io/vm: testing
        ports:
          - name: ssh
            protocol: TCP
            port: 22
            targetPort: 22
          - name: http
            protocol: TCP
            port: 80
            targetPort: 80
          - name: https
            protocol: TCP
            port: 443
            targetPort: 443
  environment:
    images:
      ingress:
        enabled: true
      storage: 1Gi
      registries:
      - urls:
        - https://quay.io/containerdisks
        onDemand: true
        tlsVerify: true
        content:
        - prefix: "**"
          destination: /containerdisks
          stripPrefix: true
```

For more complicated installations of resources, you can use the ``App`` resource from ``kapp-controller`` to install resources for a workshop session. Do note though that relying on ``kapp-controller`` to delete the resources at the end of a workshop session will not always work. This is because there is no way to guarantee that the service account used by the ``App`` will not be deleted before reconciliation on deletion of ``App`` runs.

The result will be that deletion of the workshop session will hang if the service account used by the ``App`` is deleted before ``App`` is reconciled on deletion, with the workshop session only being cleaned up properly after a few minutes when the Educates operator steps in and detects that deletion has hung and takes steps to forcibly delete it. This should be avoided as the hung session will consume resources until it is deleted, which could prevent further sessions being created if resources are limited. It also can still result in cluster scoped resources being left behind, which can cause problems if a workshop environment is deleted and recreated, as the cluster scoped resources will already exist when there is a subsequent attempt to create them again.

To avoid this, one can set ``noopDelete: true`` on ``App``, but you will also need to modify any existing ``App`` resource definition to apply an overlay to all resources generated by the ``App`` before they are deployed to the cluster. This can be done by adding an additional ``template`` phase which adds owner references against the ``WorkshopSession`` resource for the workshop session.

```yaml
    - apiVersion: kappctrl.k14s.io/v1alpha1
      kind: App
      metadata:
        name: $(session_name)-admin-vcluster-package
        namespace: $(workshop_namespace)
      spec:
        serviceAccountName: kapp-installer
        syncPeriod: 720h
        noopDelete: true
        fetch:
        - helmChart:
            name: vcluster
            repository:
              url: https://charts.loft.sh
        template:
        - helmTemplate:
            name: admin-vcluster
            namespace: $(session_namespace)
            valuesFrom:
            - secretRef:
                name: $(session_name)-admin-vcluster-values
        - ytt:
            inline:
              paths:
                overlays.yaml: |
                  #@ load("@ytt:data", "data")
                  #@ load("@ytt:overlay", "overlay")
                  #@overlay/match by=overlay.all, expects="1+"
                  ---
                  metadata:
                    #@overlay/match missing_ok=True
                    ownerReferences:
                      - apiVersion: training.educates.dev/v1beta1
                        kind: WorkshopSession
                        blockOwnerDeletion: true
                        controller: true
                        name: $(session_name)
                        uid: $(workshop_session_uid)
        deploy:
        - kapp:
            rawOptions:
            - --app-changes-max-to-keep=5
```

This can be added after an existing ``ytt`` or ``helmTemplate`` section under ``template``, it is not necessary to merge it with an existing ``ytt`` section under ``template``.

The purpose of the overlay is to set the owner of all resources generated to be the ``WorkshopSession`` resource for the workshop session. This will ensure that any resources will be automatically deleted when the workshop session is deleted, without relying on ``kapp-controller`` to clean them up.

(overriding-default-rbac-rules)=
Overriding default RBAC rules
-----------------------------

By default the service account created for the workshop instance, has ``admin`` role access to the session namespace created for that workshop instance. This enables the service account to be used to deploy applications to the session namespace, as well as manage secrets and service accounts.

Where a workshop doesn't require ``admin`` access for the namespace, you can reduce the level of access it has to ``edit`` or ``view`` by setting the ``session.namespaces.role`` field.

```yaml
spec:
  session:
    namespaces:
      role: view
```

If you need to add additional roles to the service account, such as the ability to work with custom resource types which have been added to the cluster, you can add the appropriate ``Role`` and ``RoleBinding`` definitions to the ``session.objects`` field described previously.

```yaml
spec:
  session:
    objects:
    - apiVersion: rbac.authorization.k8s.io/v1
      kind: Role
      metadata:
        name: kpack-user
      rules:
      - apiGroups:
        - build.pivotal.io
        resources:
        - builds
        - builders
        - images
        - sourceresolvers
        verbs:
        - get
        - list
        - watch
        - create
        - delete
        - patch
        - update
    - apiVersion: rbac.authorization.k8s.io/v1
      kind: RoleBinding
      metadata:
        name: kpack-user
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: Role
        name: kpack-user
      subjects:
      - kind: ServiceAccount
        namespace: $(workshop_namespace)
        name: $(service_account)
```

Because the subject of a ``RoleBinding`` needs to specify the service account name and namespace it is contained within, both of which are unknown in advance, references to parameters for the workshop namespace and service account for the workshop instance are used when defining the subject.

Adding additional resources via ``session.objects`` can also be used to grant cluster level roles, which would be necessary if you need to grant the service account ``cluster-admin`` role.

```yaml
spec:
  session:
    objects:
    - apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: $(session_name)-cluster-admin
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: cluster-admin
      subjects:
      - kind: ServiceAccount
        namespace: $(workshop_namespace)
        name: $(service_account)
```

In this case the name of the cluster role binding resource embeds ``$(session_name)`` so that its name is unique to the workshop instance and doesn't overlap with a binding for a different workshop instance.

(blocking-access-to-kubernetes)=
Blocking access to Kubernetes
-----------------------------

Although by default a namespace in the Kubernetes cluster is made available for use in a workshop to deploy workloads, some workshops may not actually require it. This might be the case where the workshop isn't teaching how to do things with Kubernetes, but is perhaps teaching on some other topic. In this case, access to the Kubernetes cluster can be blocked for the workshop. To do this set `session.namespaces.security.token.enabled` to `false` in the workshop definition.

```yaml
spec:
  session:
    namespaces:
      security:
        token:
          enabled: false
```

Note that previously one would patch the workshop pod template and set ``automountServiceAccountToken`` to ``false``. That method no longer works as how the access token is mounted into the workshop container is now handled differently.

(running-user-containers-as-root)=
Running user containers as root
-------------------------------

In addition to RBAC which controls what resources a user can create and work with, pod security policies are applied to restrict what pods/containers a user deploys can do.

By default the deployments that can be created by a workshop user are only allowed to run containers as a non root user. This means that many container images available on registries such as Docker Hub may not be able to be used.

If you are creating a workshop where a user needs to be able to run containers as the root user, you need to override the default ``restricted`` security policy and select the ``baseline`` security policy using the ``session.namespaces.security.policy`` setting.

```yaml
spec:
  session:
    namespaces:
      security:
        policy: baseline
```

This setting applies to the primary session namespace and any secondary namespaces that may be created.

(creating-additional-namespaces)=
Creating additional namespaces
------------------------------

For each workshop instance a primary session namespace is created, into which applications can be pre-deployed, or deployed as part of the workshop.

If you need more than one namespace per workshop instance, you can create secondary namespaces in a couple of ways.

If the secondary namespaces are to be created empty, you can list the details of the namespaces under the property ``session.namespaces.secondary``.

```yaml
spec:
  session:
    namespaces:
      role: admin
      budget: medium
      secondary:
      - name: $(session_name)-apps
        role: edit
        budget: large
        limits:
          default:
            memory: 512mi
```

When secondary namespaces are created, by default, the role, resource quotas and limit ranges will be set the same as the primary session namespace. Each namespace will though have a separate resource budget, it is not shared.

If required, you can override what ``role``, ``budget`` and ``limits`` should be applied within the entry for the namespace.

Similarly, you can override the security policy for secondary namespaces on a case by case basis by adding the ``security.policy`` setting under the entry for the secondary namespace.

If you also need to create resources in the namespaces you want to create, you may prefer creating the namespaces by adding an appropriate ``Namespace`` resource to ``session.objects``, along with the definitions of the resources you want to create in the namespaces.

```yaml
spec:
  session:
    objects:
    - apiVersion: v1
      kind: Namespace
      metadata:
        name: $(session_namespace)-apps
```

When listing any other resources to be created within the additional namespace, such as deployments, ensure that the ``namespace`` is set in the ``metadata`` of the resource, e.g., ``$(session_namespace)-apps``.

If you need to override what role the service account for the workshop instance has in the additional namespace, you can set the ``training.educates.dev/session.role`` annotation on the ``Namespace`` resource.

```yaml
spec:
  session:
    objects:
    - apiVersion: v1
      kind: Namespace
      metadata:
        name: $(session_namespace)-apps
        annotations:
          training.educates.dev/session.role: view
```

If you need to have a different resource budget set for the additional namespace, you can add the annotation ``training.educates.dev/session.budget`` in the ``Namespace`` resource metadata and set the value to the required resource budget.

```yaml
spec:
  session:
    objects:
    - apiVersion: v1
      kind: Namespace
      metadata:
        name: $(session_namespace)-apps
        annotations:
          training.educates.dev/session.budget: large
```

In order to override the limit range values applied corresponding to the budget applied, you can add annotations starting with ``training.educates.dev/session.limits.`` for each entry.

```yaml
spec:
  session:
    objects:
    - apiVersion: v1
      kind: Namespace
      metadata:
        name: $(session_namespace)-apps
        annotations:
          training.educates.dev/session.limits.min.cpu: 50m
          training.educates.dev/session.limits.min.memory: 32Mi
          training.educates.dev/session.limits.max.cpu: 1
          training.educates.dev/session.limits.max.memory: 1Gi
          training.educates.dev/session.limits.defaultrequest.cpu: 50m
          training.educates.dev/session.limits.defaultrequest.memory: 128Mi
          training.educates.dev/session.limits.request.cpu: 500m
          training.educates.dev/session.limits.request.memory: 1Gi
```

You only need to supply annotations for the values you want to override.

If you need more fine grained control over the limit ranges and resource quotas, set the value of the annotation for the budget to ``custom`` and add the ``LimitRange`` and ``ResourceQuota`` definitions to ``session.objects``.

In this case you must set the ``namespace`` for the ``LimitRange`` and ``ResourceQuota`` resource to the name of the namespace, e.g., ``$(session_namespace)-apps`` so they are only applied to that namespace.

If you need to set the security policy for a specific namespace different to the primary session namespace, you can add the annotation ``training.educates.dev/session.security.policy`` in the ``Namespace`` resource metadata and set the value to ``restricted`` or ``baseline`` as necessary.

Shared workshop resources
-------------------------

Adding a list of resources to ``session.objects`` will result in the given resources being created for each workshop instance, where namespaced resources will default to being created in the session namespace for that workshop instance.

If instead you want to have one common shared set of resources created once for the whole workshop environment, that is, used by all workshop instances, you can list them in the ``environment.objects`` field.

This might for example be used to deploy a single image registry which is used by all workshop instances, with a Kubernetes job used to import a set of images into the image registry, which are then referenced by the workshop instances.

For namespaced resources, it is not necessary to specify the ``namespace`` field of the resource ``metadata``. When the ``namespace`` field is not present the resource will automatically be created within the workshop namespace for that workshop environment.

When resources are created, owner references are added making the ``WorkshopEnvironment`` custom resource corresponding to the workshop environment the owner. This means that when the workshop environment is deleted, any resources will be automatically deleted. If using ``App`` resource from ``kapp-controller`` to install resources, as with ``session.objects`` you should set ``noopDelete: true`` and apply overlays to all generated resources to set owner references. In this case though the owner reference should be to the ``WorkshopEnvironment`` resource, where the ``uid`` for it is given by the ``$(workshop_environment_uid)`` data variable.

Values of fields in the list of resource objects can reference any of the environment data variables which may be appropriate.

If you want to create additional namespaces associated with the workshop environment, embed a reference to ``$(workshop_namespace)`` in the name of the additional namespaces, with an appropriate suffix. Be mindful that the suffix doesn't overlap with the range of session IDs for workshop instances.

When creating deployments in the workshop namespace, set the ``serviceAccountName`` of the ``Deployment`` resouce to ``$(service_account)``. This will ensure the deployment makes use of a special pod security policy set up by Educates. If this isn't used and the cluster imposes a more strict default pod security policy, your deployment may not work, especially if any image expects to run as ``root``.

(shared-assets-repository)=
Shared assets repository
------------------------

One use for shared workshop resources is to deploy a HTTP server in the workshop environment, which is then accessed by workshop sessions to download common resources. This could include workshop content, packages or any other files. To create such a deployment a custom HTTP server image would however be required.

Because such a common HTTP server for local caching of files for use by workshop sessions has benefits in as much as ensuring data locality and avoiding pulling down data from remote servers for every workshop session, an inbuilt capacity of a shared assets repository is provided. This deploys a HTTP server in the workshop environment for any workshop sessions to use. The HTTP server is prepopulated on startup with files downloaded using ``vendir``.

```yaml
spec:
  environment:
    assets:
      files:
      - image:
          url: ghcr.io/educates/workshop-files:latest
```

The URL values can reference any of the environment download data variables which may be appropriate.

The hostname for accessing the assets repository is available in a workshop definition using the data variable ``$(assets_repository)``, however, by default the assets repository is not exposed publicly outside of the Kubernetes cluster. If you want to have a public ingress created for it, it can be enabled in two different ways.

The first method is to use the ability to configure additional ingresses for each workshop session that proxy to an internal Kubernetes service.

```yaml
spec:
  session:
    ingresses:
    - name: assets
      host: $(assets_repository)
```

The URL for access would be equivalent to ``$(ingress_protocol)://assets-$(session_name).$(ingress_domain)``.

Using this method, access would be authenticated using the workshop session credentials, or you could optionally enable anonymous access.

The second method is to enable creation of a shared ingress when specifying the source for the assets.

```yaml
spec:
  environment:
    assets:
      ingress:
        enabled: true
      files:
      - image:
          url: ghcr.io/educates/workshop-files:latest
```

In this case the data variable ``$(assets_repository)`` will be the public hostname for the assets server and anonymous access is always possible. The URL for accessing the assets server would be ``$(ingress_protocol)://$(assets_repository)``.

Whichever way is used, there are no access controls in place to restrict access to the assets repository within the Kubernetes cluster and so it should not be used for resources which need to be protected.

Also note that at present, the `vendir` snippet for specifying how to download content to be hosted by the assets repository, does not support use of secret credentials for accessing any remote sites.

Any files downloaded to be hosted by the assets repository are by default in ephemeral container storage. If you need to guarantee storage space available, you can specify storage space and a persistent volume will be used for assets storage.

```yaml
spec:
  environment:
    assets:
      storage: 5Gi
      files:
      - image:
          url: ghcr.io/educates/workshop-files:latest
```

The HTTP server used to serve up assets by default will be given 128Mi of memory. If you need to customize this value you can override the memory:

```yaml
spec:
  environment:
    assets:
      memory: 128Mi
```

As the ``vendir`` program used to download content to be served up by the assets server will unpack tar/zip archives (by default) and OCI images (always), if used to cache the workshop files required for a workshop session they will all exist as indvidual files and not a single archive which can be downloaded.

To make it possible to still use the assets server as a local cache for workshop files which are to be downloaded into each workshop session, the custom HTTP server used allows a URL to be used where the path maps to a directory, with a path suffix consisting of an extension for the type of archive file you want to download.

```yaml
spec:
  workshop:
    files:
    - http:
        url: $(ingress_protocol)://$(assets_repository)/workshop-files/.tgz
      includePaths:
      - /workshop/**
      - /templates/**
      - /README.md
      path: .
  environment:
    assets:
      ingress:
        enabled: true
      files:
      - image:
          url: ghcr.io/educates/workshop-files:latest
        path: workshop-files
```

The extensions for the different archive types supported are ``.tar``, ``.tar.gz``, ``.tgz`` and ``.zip``.

Note that ``vendir`` doesn't preserve execute permissions on any files when unpacking a tar/zip archive. Educates will restore execute permissions on any ``setup.d`` script files, however if you have any other files which need execute permissions to be set, you will need to provide a ``setup.d`` scripts which re-applies execute permissions.

(shared-oci-image-cache)=
Shared OCI image cache
----------------------

Workshops can make use of OCI container images for a number of reasons. These might be to hold workshop files, extension packages, custom workshop base images, or for application images which are deployed to the Kubernetes cluster.

Although in the case of deploying applications to Kubernetes the container images are cached on the Kubernetes cluster nodes for subsequent use, in general container images will need to be pulled down each time from whatever remote image repository they are hosted on.

In order to reduce the amount of remote network traffic and make downloads of container images quicker, an OCI image registry can be enabled to act as a cache for any OCI container images that a workshop requires.

This OCI image caching functionality makes use of the [Zot Registry](https://zotregistry.dev/), with a set of synchronization rules being able to be supplied. These rules can setup an automatic mirroring of specified OCI container images so they are cached, or on demand pulling of images can be performed, with them then being cached for subsequent requests. Do note though that automatic mirroring will only work where the remote image registry supports accessing the image catalog.

As example, the following uses the OCI image cache to cache the OCI image holding the workshop files, with them then being source from the image cache for each session.

```yaml
spec:
  workshop:
    files:
    - image:
        url: $(oci_image_cache)/lab-k8s-fundamentals-files:5.0
      includePaths:
      - /workshop/**
      - /templates/**
      - /README.md
  environment:
    images:
      registries:
      - content:
        - destination: /lab-k8s-fundamentals-files
          prefix: /educates/lab-k8s-fundamentals-files
          stripPrefix: true
        onDemand: true
        urls:
        - https://ghcr.io
```

Values can reference any of the environment download data variables which may be appropriate.

The ``$(oci_image_cache)`` variable can be used in the workshop definition when needing to refer to the image cache.

Do note though that in order to use the OCI image cache with container images to be deployed to the Kubernetes cluster, it will be necessary to expose the image cache using an ingress and you must have secure ingress enabled for Educates.

```yaml
spec:
  environment:
    images:
      ingress:
        enabled: true
      registries:
      - content:
        - destination: /docker.io
          prefix: /library/busybox
        onDemand: true
        urls:
        - https://index.docker.io
```

When there is no ingress ``$(oci_image_cache)`` will be the service hostname of the image cache internal to the Kubernetes cluster. When exposed by an ingress it will be the public hostname.

There is no authentication on pulling images from the image cache, so it should not be used to store images which should not be public. There is currently no way to configure the image cache with credentials to pull images from private registries.

If needing to reference the image cache hostname in workshop instructions you can use the ``oci_image_cache`` variable, and if required in the shell environment, use ``OCI_IMAGE_CACHE``.

Any OCI images downloaded and cached are by default in ephemeral container storage. If you need to guarantee storage space available, you can specify storage space and a persistent volume will be used for assets storage.

```yaml
spec:
  environment:
    images:
      storage: 5Gi
```

The image registry used will by default will be given 512Mi of memory. If you need to customize this value you can override the memory:

```yaml
spec:
  environment:
    images:
      memory: 512Mi
```

For more details on configuring the synchronization rules for registries see the Zot Registry documentation on [mirroring](https://zotregistry.dev/v1.4.3/articles/mirroring/). Only details under ``registries`` can be supplied, with the exception that providing certificates via ``certDir`` is not supported.

(injecting-workshop-secrets)=
Injecting workshop secrets
--------------------------

Kubernetes resources to be created common to the workshop environment can be specified in ``environment.objects``, and any which need to be created for each workshop session can be specified in ``session.objects``. Both these can include Kubernetes secrets, however often secrets will exist independently and it is not appropriate to embed the secret definition within the workshop definition.

To cater for Kubernetes secrets that need to be maintained separately, it is possible to specify a list of secrets that are required for a workshop. This list consists of entries giving the name of the namespace the secret is contained in, and the name of the secret.

```yaml
spec:
  environment:
    secrets:
    - name: image-repository-pull
      namespace: default
```

When the workshop environment is created, a secret copier definition is setup for the bundled secret copier which results in a copy of the secret being copied into the workshop namespace. Further, any updates to the original secret will also be automatically propogated to the workshop namespace.

As necessary the secrets could be mounted into a workshop container by patching the workshop template to add the volume mount, or it could be used by workloads deployed to the workshop namespace, including jobs, created by being listed in ``environment.objects`` or ``session.objects``.

In the case of downloading workshop content, or adding any extension packages, the ``vendir`` configuration for the download can reference secrets from the ``environment.secrets`` list. If this occurs, the workshop deployment will use an init container, which the secrets will be mounted in, to run ``vendir`` when downloading any files. In this way any secrets are kept separate from the main workshop container and will not be exposed to a workshop user.

(passing-parameters-to-a-session)=
Passing parameters to a session
-------------------------------

When using the ability to inject secrets into a workshop, the contents of any secret is the same for all workshop sessions. Such secrets can therefore only be used to supply common configuration or credentials.

If you need to customize a workshop session specific to each workshop user, it is possible to pass in a unique set of parameters when requesting a workshop session and allocating it to a user. This mechanism is only available though when using the REST API of the training portal to request workshop sessions and cannot be used to customize a workshop session when using the training portal's builtin web based user interface.

The names of any allowed parameters and the default values must be supplied by setting ``request.parameters``.

```yaml
spec:
  request:
    parameters:
    - name: WORKSHOP_USERNAME
      value: "default"
```

If no default value is provided, then an empty string will be used. Any parameters which are supplied when requesting a workshop session via the REST API which are not in this list will be ignored.

When a workshop session is requested via the REST API, the list of desired parameters are supplied in the body of the POST request as JSON.

```yaml
{
  "parameters": [
    {
      "name": "WORKSHOP_USERNAME",
      "value": "grumpy"
    }
  ]
}
```

When a request for a workshop session is received via the REST API of the training portal, a secret specific to the workshop session will be created in the workshop namespace containing the list of parameters as data. The name of this secret will be of the form ``$(session_name)-request``.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: lab-parameters-sample-w01-s001-request
  namespace: lab-parameters-sample-w01
data:
  WORKSHOP_USERNAME: Z3J1bXB5
```

Under normal circumstances the deployment of the workshop dashboard for a workshop session will have no dependency on this secret. As such, if reserved sessions were configured for a workshop then the workshop dashboard would have been created prior to this secret being created.

In order to delay deployment of the workshop dashboard and inject the request parameters into the workshop session such that they are available through environment variables, it is possible to use ``envFrom`` when specifying environment variables for the workshop.

```yaml
spec:
  session:
    envFrom:
    - secretRef:
        name: $(session_name)-request
```

Because all data values in the secret are mounted as environment variables, you should avoid parameter names which conflict with builtin environment variables set for a workshop or by the shell.

If you need to remap the name of a parameter when injecting it as an environment variable, you can instead use ``valueFrom`` when using ``env`` to specify the environment variables.

```yaml
spec:
  session:
    env:
    - name: WORKSHOP_USERNAME
      valueFrom:
        secretKeyRef:
          name: $(session_name)-request
          key: username
```

Note that when using reserved workshop sessions and such a dependency is declared on the secret, the deployment of the workshop pod will not initially be able to complete and will show as being stuck in a state with error ``CreateContainerConfigError``. Although this occurs, once the secret has been created at the time of the workshop session being allocated to a user, the deployment will proceed and be able to complete.

In addition to injecting the parameters from the secret as environment variables, they could also be injected into the workshop pod by supplying a ``patches`` configuration to mount data as a file volume. If desired you could also configure RBAC for the workshop session service account to be able to query just the secret for their workshop session and use ``kubectl`` to access it.

```yaml
spec:
  session:
    objects:
    - apiVersion: rbac.authorization.k8s.io/v1
      kind: Role
      metadata:
        namespace: $(workshop_namespace)
        name: $(session_name)-secrets
      rules:
      - apiGroups:
        -  ""
        resources:
        - secrets
        resourceNames:
        - $(session_name)-request
        verbs:
        - get
    - apiVersion: rbac.authorization.k8s.io/v1
      kind: RoleBinding
      metadata:
        namespace: $(workshop_namespace)
        name: $(session_name)-secrets
      subjects:
      - kind: ServiceAccount
        namespace: $(workshop_namespace)
        name: $(service_account)
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: Role
        name: $(session_name)-secrets
```

When a workshop has been configured to accept request parameters, if the training portal web user interface is still used to request the workshop session and not the REST API, the workshop session can still be allocated, however the secret for the parameters will be populated only with the default values and they would not be able to be overridden. If a workshop is to be setup so that it can be requested and allocated to a workshop user using both ways, the workshop would need to detect when the default values are used and adjust the behaviour of the workshop and what instructions are displayed as necessary.

If relying on the default values for parameters, and also using parameters to pass in values such as credentials that need to be random and different per workshop session, you can configure the default value to be a random generated value.

```yaml
spec:
  request:
    parameters:
    - name: WORKSHOP_USERNAME
      value: "default"
    - name: WORKSHOP_PASSWORD
      generate: expression
      from: "[A-Z0-9]{8}"
```

The `from` value is defined as a limited form of regex (sometimes referred to as a xeger pattern). The resulting value will be a random string which matches the pattern supplied. Any regex pattern can include literal prefixes or suffixes, or you could even have multiple regex patterns joined together or with separators.

(resource-creation-on-allocation)=
Resource creation on allocation
-------------------------------

When using request parameters in a workshop you can bind them to the workshop pod as environment variables or volume mounts. The same could also be done with deployments created from resources listed in ``session.objects``. In both cases since the deployments are created when a workshop session is initially provisioned, which could as a result of reserved sessions be some time prior to the workshop session being actually required for allocation to a workshop user, the dependency on the secret by the deployments delays them until the point the secret is created. During this time the deployment will show in an error state of ``CreateContainerConfigError``.

For the case of resources listed in ``session.objects``, if you would prefer that the resources not be created until the point that the workshop session is allocated to a user, instead of adding them to ``session.objects``, you can instead list them in ``request.objects``. Such resources will only be created after the secret containing the parameters is created.

Using ``request.objects`` you can therefore avoid having deployments which are stuck in ``CreateContainerConfigError`` as they will only be created when the workshop session is allocated to the user and the parameters are available.

As the parameters are available at this point, as well as being able to use the secret holding the parameters when defining the environment variables or a volume mount for a deployment, the parameters themselves are available as data variables which can be used in the definition of the resource listed in ``request.objects``. You can therefore bypass referencing the secret and use the parameters directly. This can be useful where needing to create secrets which are composed from multiple parameters, or where the parameters are needed as a values in a custom resource.

```yaml
spec:
  request:
    objects:
    - apiVersion: example.com/v1
      kind: UserAccount
      metadata:
        name: user-account
        namespace: $(session_namespace)
      spec:
        username: $(WORKSHOP_USERNAME)
        password: $(WORKSHOP_PASSWORD)
```

In addition to the parameters being able to be used in `request.objects`, a few extra data variables are also available. These include user information from the training portal. Whether these are useful depend on what type of authentication is used with the training portal. For example, if anonymous user authentication is used, the used identifier would be a UUID and other fields empty.

* `username` - The name of the user as registered by the training portal.
* `first_name` - The first name of the registered user.
* `last_name` - The last name of the registered user.
* `email` - The email address of the registered user.

Note that this only applies to ``request.objects``. The request parameters and user data variables cannot be used in this way with ``session.objects`` or ``environment.objects``.

(defining-additional-ingress-points)=
Defining additional ingress points
----------------------------------

If running additional background applications, by default they are only accessible to other processes within the same container. In order for an application to be accessible to a user via their web browser, an ingress needs to be created mapping to the port for the application.

You can do this by supplying a list of the ingress points, and the internal container port they map to, by setting the ``session.ingresses`` field in the workshop definition.

```yaml
spec:
  session:
    ingresses:
    - name: application
      port: 8080
```

The form of the hostname used in URL to access the service will be:

```text
application-$(session_name).$(ingress_domain)
```

Note that it is also possible to specify ``-application`` as a suffix in the first component of the full host name. This was an older convention and is still supported, however the ``application-`` prefix is preferred as it would allow a workshop to be deployed standalone using ``docker`` independent of Educates, with access using a ``nip.io`` style address.

You should not use as the name, the name of any builtin dashboards, ``terminal``, ``console``, ``slides`` or ``editor``. These are reserved for the corresponding builtin capabilities providing those features.

In addition to specifying ingresses for proxying to internal ports within the same pod, you can specify a ``host``, ``protocol`` and ``port`` corresponding to a separate service running in the Kubernetes cluster.

```yaml
spec:
  session:
    ingresses:
    - name: application
      protocol: http
      host: service.namespace.svc.cluster.local
      port: 8080
```

Session data variables providing information about the current session can be used within the ``host`` property if required.

```yaml
spec:
  session:
    ingresses:
    - name: application
      protocol: http
      host: service.$(session_namespace).svc.$(cluster_domain)
      port: 8080
```

If the service uses standard ``http`` or ``https`` ports, you can leave out the ``port`` property and the port will be set based on the value of ``protocol``.

When ``protocol`` is ``https`` the target must use a valid signed SSL certificate that can be verified. If SSL certificate verification should be skipped, the ``secure`` property can be set to ``false``.

```yaml
spec:
  session:
    ingresses:
    - name: application
      protocol: https
      host: service.$(session_namespace).svc.$(cluster_domain)
      secure: false
```

If you have mulitple backend services which need to be exposed under a single ingress host you can specify multiple ingress points for the same name and direct requests to different backends based on URL path prefix.

```yaml
spec:
  session:
    ingresses:
    - name: application
      protocol: http
      host: service-2.$(session_namespace).svc.$(cluster_domain)
      port: 8080
      path: /api/
    - name: application
      protocol: http
      host: service-1.$(session_namespace).svc.$(cluster_domain)
      port: 8080
```

When multiple entries exist for the same ingress name, they are processed in order and as such the more deeply nested URL path should be listed before others.

If the URL path needs to be rewritten when it passes through the proxy you can use the ``pathRewrite`` property.

```yaml
spec:
  session:
    ingresses:
    - name: application
      protocol: http
      host: service-2.$(session_namespace).svc.$(cluster_domain)
      port: 8080
      path: /api/
      pathRewrite:
      - pattern: "^/api/"
        replacement: "/"
    - name: application
      protocol: http
      host: service-1.$(session_namespace).svc.$(cluster_domain)
      port: 8080
```

When a request is being proxied, you can specify additional request headers that should be passed to the service.

```yaml
spec:
  session:
    ingresses:
    - name: application
      protocol: http
      host: service.$(session_namespace).svc.$(cluster_domain)
      port: 8080
      headers:
      - name: Authorization
        value: "Bearer $(kubernetes_token)"
```

The value of a header can reference the following variables.

* ``kubernetes_token`` - The access token of the service account for the current workshop session, used for accessing the Kubernetes REST API.

When the HTTP requested is proxied to the target web application, the ``Host`` header will be rewritten to match the hostname of the target so that a web server using name based virtual hosts will work. If you need for the original ``Host`` header with the hostname of the workshop session to be preserved you can set the ``changeOrigin`` property to ``false``.

```yaml
spec:
  session:
    ingresses:
    - name: application
      protocol: http
      host: service.$(session_namespace).svc.$(cluster_domain)
      changeOrigin: false
      port: 8080
```

Accessing any service via the ingress will be protected by any access controls enforced by the workshop environment or training portal. If the training portal is used this should be transparent, otherwise you will need to supply any login credentials for the workshop again when prompted by your web browser.

If you want to disable the access controls you can override the authentication type for the ingress. The default for ``authentication.type`` is ``session``. To disable, set this to ``none``.

```yaml
spec:
  session:
    ingresses:
    - name: application
      authentication:
        type: none
      protocol: http
      host: service.namespace.svc.cluster.local
      port: 8080
```

If required, the downstream service can implement its own authentication, such as HTTP basic authentication.

(adding-extra-init-containers)=
Adding extra init containers
----------------------------

Within the workshop container ``setup.d`` scripts supplied with the workshop files can be executed to perform special setup for the workshop session. In cases where special setup is required that needs access to sensitive information you don't want this to be done in the workshop container where a workshop user can view the sensitive information.

In this case you may be able to perform the special setup from within an init container. Such an init container can be defined in ``initContainers``.

```yaml
spec:
  session:
    initContainers:
      - name: special-workshop-setup
        image: $(workshop_image)
        imagePullPolicy: $(workshop_image_pull_policy)
        command: ["/opt/special-workshop-setup-scripts/setup.sh"]
        volumeMounts:
        - name: special-workshop-setup-scripts
          mountPath: /opt/special-workshop-setup-scripts
    volumes:
    - name: special-workshop-setup-scripts
      secret:
        secretName: $(session_name)-special-workshop-setup-scripts
        defaultMode: 0755
    objects:
    - apiVersion: v1
      kind: Secret
      metadata:
        name: $(session_name)-special-workshop-setup-scripts
        namespace: $(workshop_namespace)
      stringData:
        setup.sh: |
          #!/bin/bash
          ...
```

As you don't have access to the workshop files in the init container, you will need to either use a custom container image, or inject a script into the init container using a secret and a volume mount.

(patching-workshop-deployment)=
Patching workshop deployment
----------------------------

The workshop definition provides various ways you can customize how the workshop pod is deployed. If you need to make other changes to the pod template for the deployment used to create the workshop instance, you need to provide an overlay patch. Such a patch might be used to override the default CPU and memory limit applied to the workshop instance, or to mount a volume.

The patches are provided by setting ``session.patches``. The patch will be applied to the ``spec`` field of the pod template.

```yaml
spec:
  session:
    patches:
      containers:
      - name: workshop
        resources:
          requests:
            memory: "1Gi"
          limits:
            memory: "1Gi"
```

In this example the default memory limit of "512Mi" is increased to "1Gi". Although memory is being set via a patch in this example, the ``session.resources.memory`` field is the preferred way to override the memory allocated to the container the workshop environment is running in.

The patch when applied works a bit differently to overlay patches as found elsewhere in Kubernetes. Specifically, when patching an array and the array contains a list of objects, a search is performed on the destination array and if an object already exists with the same value for the ``name`` field, the item in the source array will be overlaid on top of the existing item in the destination array. If there is no matching item in the destination array, the item in the source array will be added to the end of the destination array.

This means an array doesn't outright replace an existing array, but a more intelligent merge is performed of elements in the array.

Workshop instructions layout
----------------------------

Workshop instructions will by default be displayed in a panel on the left hand side of the workshop dashboard. If you instead want the workshop instructions to be displayed in a tab of the workshop dashboard, with no left hand side panel, you can override the layout for the workshop instructions using:

```yaml
spec:
  session:
    applications:
      workshop:
        layout: tab
```

The name of the tab displayed in the workshop dashboard will be ``Workshop`` and it will appear first amongst any tabs. If you want to change the name of the tab, you will need to disable the standard layouts for the workshop instructions and configure your own additional dashboard tab.

```yaml
spec:
  session:
    applications:
      workshop:
        layout: none
    dashboards:
    - name: Help
      url: /workshop/
```

Note that when this is done, the dashboard tab will come after any tabs for embedded features such as the terminal, editor or Kubernetes web console.

(external-workshop-instructions)=
External workshop instructions
------------------------------

In place of using workshop instructions provided with the workshop content, you can use separately hosted instructions instead. This can be configured in two different ways. In the first case you can cause a browser redirect to a separate web site for the workshop instructions.

To do this set ``sessions.applications.workshop.url`` to the URL of the separate web site.

```yaml
spec:
  session:
    applications:
      workshop:
        url: https://www.example.com/instructions
```

The external web site must be able to be displayed in an HTML iframe, will be shown as is and should provide its own page navigation and table of contents if required. 

The URL value can reference any of the session data variables which may be appropriate.

These could be used for example to reference workshops instructions hosted as part of the workshop environment.

```yaml
spec:
  session:
    applications:
      workshop:
        url: $(ingress_protocol)://instructions-$(workshop_namespace).$(ingress_domain)
  environment:
    objects:
    - ...
```

In this case ``environment.objects`` of the workshop ``spec`` would need to include resources to deploy the application hosting the instructions and expose it via an appropriate ingress.

Instead of forcing a browser redirect to the separately hosted workshop instructions, you can instead configure the workshop dashboard to act as a proxy and pass web requests through to the separately hosted workshop instructions.

The configuration for this is similar to when configuring an additional ingress for a workshop.

```yaml
spec:
  session:
    applications:
      workshop:
        proxy:
          protocol: https
          host: www.example.com
```

Other properties that can be defined for the proxy are ``port``, ``headers`` and ``changeOrigin``, with the latter being able to be set to ``false`` where the original hostname for the workshop dashboard should be propagated when accessing the separate web service.

For example, if wanting to proxy through to workshop instructions served up from the local host system using the ``educates`` CLI, you could use:

```yaml
spec:
  session:
    applications:
      workshop:
        proxy:
          protocol: http
          host: localhost.$(ingress_domain)
          port: 10081
          changeOrigin: false
          headers:
          - name: X-Session-Name
            value: $(session_name)
```

When the HTTP request is proxied, the URL path will be the original used to access the workshop dashboard, which since only the URL paths for the workshop instructions is proxied will mean that all URL paths are prefixed with ``/workshop/content/``.

If you need to rewrite the URL path due to the separate web site hosting workshop instructions at the root of the web site, you can specify a path rewrite rule.

```yaml
spec:
  session:
    applications:
      workshop:
        proxy:
          protocol: https
          host: www.example.com
          pathRewrite:
          - pattern: "^/workshop/content/"
            replacement: "/"
```

In this case the separate web site would need to ensure it always generates relative URL paths.

(static-workshop-instructions)=
Static workshop instructions
----------------------------

If you want to host workshop instructions from the workshop container, but generate static HTML for the workshop instructions using a separate tool instead of using the builtin local renderer for workshop instructions, you can set the directory path for where the static HTML files are located.

```yaml
spec:
  session:
    applications:
      workshop:
        path: public
```

The static HTML files in this case would need to reside in the ``/opt/workshop/public``directory.

If the static HTML files already exist in the ``workshop/public`` directory of the downloaded workshop files, these will be copied under ``/opt/workshop`` automatically, along with everything else under the ``workshop`` directory.

Alternatively, you could generate the static HTML files from a setup script when the workshop container starts. The latter would allow the generated static HTML files to still embed customized instructions based on session information provided as environment variables to the workshop session.

Note that all URL accesses for static HTML content are prefixed with ``/workshop/content/`` so generated static HTML files must be configured with that as the base URL path, or must use relative paths for internal links.

Disabling workshop instructions
-------------------------------

The aim of the workshop environment is to provide instructions for a workshop which users can follow. If you want instead to use the workshop environment as a development environment, or use it as an admistration console which provides access to a Kubernetes cluster, you can disable the display of workshop instructions provided with the workshop content. In this case only the workarea with the terminals, console etc, will be displayed. To disable display of workshop instructions, add a ``session.applications.workshop`` section and set the ``enabled`` property to ``false``.

```yaml
spec:
  session:
    applications:
      workshop:
        enabled: false
```

(enabling-presentation-slides)=
Enabling presentation slides
----------------------------

If a workshop includes a presentation, slides can be included by placing them in the ``workshop/slides`` directory. Anything in this directory will be served up as static files via a HTTP web server. The default web page should be provided as ``index.html``. A dashboard tab will be automatically created which displays the slides.

```yaml
spec:
  session:
    applications:
      slides:
        enabled: true
```

For slides bundled as a PDF file, add the PDF file to ``workshop/slides`` and then add an ``index.html`` which displays the PDF [embedded](https://stackoverflow.com/questions/291813/recommended-way-to-embed-pdf-in-html) in the page.

To support the use of [reveal.js](https://revealjs.com/), static media assets for that package are already bundled and available for the two major versions (3.X and 4.X).

To enable and select the version of reveal.js, supply in the workshop definition:

```yaml
spec:
  session:
    applications:
      slides:
        enabled: true
        reveal.js:
          version: 3.X
```

The version can specify the exact version supplied, or a semver style version selector, including range specification.

If you are using reveal.js for the slides and you have history enabled, or are using section IDs to support named links, you can use an anchor to a specific slide and that slide will be opened when clicked on:

```text
/sildes/#/questions
```

When using embedded links to the slides in workshop content, if the workshop content is displayed as part of the dashboard, the slides will be opened in the tab to the right rather than as a separate browser window or tab.

To support the use of [impress.js](https://impress.js.org/), static media assets for that package are already bundled and available for version 1.X.

To enable and select the version of impress.js, supply in the workshop definition:

```yaml
spec:
  session:
    applications:
      slides:
        enabled: true
        impress.js:
          version: 1.X
```

Enabling the Kubernetes console
-------------------------------

By default the Kubernetes console is not enabled. If you want to enable it and make it available through the web browser when accessing a workshop, you need to add a ``session.applications.console`` section to the workshop definition, and set the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      console:
        enabled: true
```

The Kubernetes dashboard provided by the Kubernetes project will be used. If you would rather use Octant as the console, you can set the ``vendor`` property to ``octant``.

```yaml
spec:
  session:
    applications:
      console:
        enabled: true
        vendor: octant
```

When ``vendor`` is not set, ``kubernetes`` is assumed.

Enabling the integrated editor
------------------------------

By default the integrated web based editor is not enabled. If you want to enable it and make it available through the web browser when accessing a workshop, you need to add a ``session.applications.editor`` section to the workshop definition, and set the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      editor:
        enabled: true
```

The integrated editor which is used is based on VS Code. Details of the editor can be found at:

* [https://github.com/cdr/code-server](https://github.com/cdr/code-server)

If you need to install additional VS Code extensions, this can be done from the editor. Alternatively, if building a custom workshop, you can install them from your ``Dockerfile`` into your workshop image by running:

```text
code-server --install-extension vendor.extension
```

Replace ``vendor.extension`` with the name of the extension, where the name identifies the extension on the VS Code extensions marketplace used by the editor, or provide a path name to a local ``.vsix`` file.

This will install the extensions into ``$HOME/.config/code-server/extensions``.

If downloading extensions yourself and unpacking them, or you have them as part of your Git repository, you can instead locate them in the ``workshop/code-server/extensions`` directory.

(provisioning-a-virtual-cluster)=
Provisioning a virtual cluster
------------------------------

For each workshop session, by default, a workshop user is given access to a single Kubernetes namespace in the host Kubernetes cluster into which they can deploy workloads as part of the workshop. Access is controlled using RBAC so that this is the only Kubernetes namespace they have access to. A workshop user is unable to create additional namespaces and is not able to perform any actions which a cluster admin would normally do.

For workshops which require a greater level of access to a Kubernetes cluster, such as being able to install Kubernetes operators, or perform other operations that cluster admin access is required, provisioning of a virtual cluster can be enabled. If this is done a workshop user will have the appearance of having full access to a Kubernetes cluster, but where the cluster is actually a virtual cluster running out of the namespace of the underlying Kubernetes cluster. This doesn't allow you to do everything you could do with a Kubernetes cluster, but can be useful for many workshops requiring additional privileges.

To enable provisioning of the virtual cluster add the ``session.application.vcluster`` section to the workshop definition, and set the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      vcluster:
        enabled: true
```

The version of Kubernetes used by the virtual cluster will be whatever is the latest version supported by the bundled support for virtual clusters. If you need to have a specific version of Kubernetes from among the supported versions used, you can define the version by setting ``session.application.vcluster.version``.

```yaml
spec:
  session:
    applications:
      vcluster:
        enabled: true
        version: "1.27"
```

When a virtual cluster is used the workshop session user only has access to the virtual cluster, there is no direct access to the underlying host Kubernetes cluster REST API. The ``kubeconfig`` file provided to the workshop user will be preconfigured to point at the virtual cluster and the workshop user will have cluster admin access to the virtual cluster.

Where as when a workshop user has access to a session namespace the default security policy applied to workloads is ``restricted``, for the virtual cluster the default is ``baseline``. This will allow deployment of workloads to the virtual cluster which need to run as ``root``, bind system service ports etc. If a workshop doesn't need such level of access, the security policy should be set to be ``restricted`` instead. This is done using the same setting as would normally control the security policy for the session namespaces.

```yaml
spec:
  session:
    namespaces:
      security:
        policy: restricted
```

Resource quotas and limit ranges will be applied to the virtual cluster. For the resource quota, any quota which would normally apply to the session namespace will apply to the virtual cluster as a whole. The limit ranges will be applied to any pods deployed in the virtual cluster. If you need to set the resource budget use the same setting as would normally set the resource budget for the session namespace.

```yaml
spec:
  session:
    namespaces:
      budget: custom
```

The selected resource budget needs to accomodate that CoreDNS will always be deployed in the virtual cluster. It sets it's own explicit resource requests so default limit ranges do not get applied in that case.

Other control plane services for the virtual cluster are deployed in a separate namespace of the underlying host Kubernetes cluster so don't count in the resource budget. Those control plane services do reserve significant resources which may in many cases be more than what is required. The default is ``1Gi`` for the virtual cluster syncer application. To override these values to reduce memory reserved, or increase it, add a ``session.applications.vcluster.resources`` section. 

```yaml
spec:
  session:
    applications:
      vcluster:
        enabled: true
        resources:
          syncer:
            memory: 768Mi
```

Any ``Ingress`` resources which are created in the virtual cluster are automatically synced to the underlying host Kubernetes cluster so that workloads can receive HTTP requests. If you need more advanced features provided by the Contour ingress controller, you can enable it and it will be automatically deployed to the virtual cluster with traffic for select ingress subdomains routed to it.

To enable installation of Contour provide the ``session.applications.vcluster.ingress`` section and set ``enabled`` to ``true``.

```yaml
spec:
  session:
    applications:
      vcluster:
        enabled: true
        ingress:
          enabled: true
```

The only ingress subdomains which will be routed in this case to the virtual cluster for a session are:

```
$(session_name).$(ingress_domain)
default.$(session_name).$(ingress_domain)
```

The host for any ``Ingress`` you create must be within these subdomains.

If you wish to have additional subdomains of ``$(session_name).$(ingress_domain)``, besides just ``default``, routed to the virtual cluster ingress controller, you can list them under ``session.applications.vcluster.ingress.subdomains``:

```yaml
spec:
  session:
    applications:
      vcluster:
        enabled: true
        ingress:
          enabled: true
          subdomains:
          - default
```

If you need to have other workloads automatically deployed to the virtual cluster you have two choices.

The first is to provide a list of Kubernetes resource objects as ``session.applications.vcluster.objects``.

```yaml
spec:
  session:
    applications:
      vcluster:
        enabled: true
        objects:
        - apiVersion: v1
          kind: ConfigMap
          metadata:
            name: session-details
            namespace: default
          data:
            SESSION_NAMESPACE: "$(session_namespace)"
            INGRESS_DOMAIN: "$(ingress_domain)"
```

If this includes namespaced resources, the ``namespace`` must be specified. Session variables can be referenced in the resource definitions and they will be substituted with the appropriate values.

For more complicated deployments they need to be installable using ``kapp-controller``, and ``kapp-controller`` must be available in the host Kubernetes cluster. Appropriate ``App`` resources should then be added to ``session.objects``.

Note that these will be deployed some time after the virtual cluster is created and you cannot include in ``session.applications.vcluster.objects`` any resource types which would only be created when these subsequent packages listed in ``session.objects`` are installed.

For example, to install ``kapp-controller`` into the virtual cluster you can add to ``session.objects``:

```yaml
spec:
  session:
    objects:
    - apiVersion: kappctrl.k14s.io/v1alpha1
      kind: App
      metadata:
        name: kapp-controller.0.44.9
        namespace: $(vcluster_namespace)
      spec:
        noopDelete: true
        syncPeriod: 24h
        cluster:
          namespace: default
          kubeconfigSecretRef:
            name: $(vcluster_secret)
            key: config
        fetch:
        - http:
            url: https://github.com/carvel-dev/kapp-controller/releases/download/v0.44.9/release.yml
        template:
        - ytt: {}
        deploy:
        - kapp: {}
```

The namespace on the ``App`` resource must be ``$(vcluster_namespace)``. This variable references the namespace where the virtual cluster control plane processes run. In order for ``kapp-controller`` to know to install the packages into the virtual cluster, the ``App`` definition must include ``spec.cluster`` section defined as:

```yaml
spec:
  cluster:
    namespace: default
    kubeconfigSecretRef:
      name: $(vcluster_secret)
      key: config
```

The ``$(vcluster_secret)`` variable reference will be replaced with the name of the secret in that namespace which contains the ``kubeconfig`` file for accessing the virtual cluster.

Note that the ``App`` resource must include the property:

```yaml
spec:
  noopDelete: true
```

This is because in the case of installing into the virtual cluster, there is no need to delete the resources on completion of the workshop session as the whole virtual cluster will be deleted.

It is also recommended that the ``App`` resource include the property:

```yaml
spec:
  syncPeriod: 24h
```

This ensures that ``kapp-controller`` will not attempt to reconcile the installed package every 10 minutes (default), which is unnecessary in the context of a workshop.

If for some reason you did still want periodic reconciliation to be done, define in the ``App`` or ``Package`` resource additional options to be passed to ``kapp`` so that it limits the size of change set descriptions it keeps.

```yaml
spec:
  template:
    spec:
      deploy:
      - kapp:
          rawOptions:
          - "--app-changes-max-to-keep=5"
```

As well as ``kapp-controller``, any packages which are packaged using Carvel packages can similarly be installed. There is no need for any package repositories to be registered with ``kapp-controller`` in the host Kubernetes cluster and you should instead include an ``App`` resource constructed from any information in ``Package`` and ``PackageInstall`` to the package.

Any further workloads which need to be deployed to the virtual cluster will need to be done from a workshop ``setup.d`` script or as part of the workshop instructions. Such setup scripts must check that anything they require which was installed from ``session.objects`` has completed installation, including custom resource types being available and deployed workloads being ready. For example, a setup script which ensures that ``kapp-controller`` is installed would include:

```bash
#!/bin/bash

# Wait for CRDs for kapp-controller to have been created.

STATUS=1
ATTEMPTS=0
COMMAND="kubectl get crd/packagerepositories.packaging.carvel.dev"

until [ $STATUS -eq 0 ] || $COMMAND || [ $ATTEMPTS -eq 12 ]; do
    sleep 5
    $COMMAND
    STATUS=$?
    ATTEMPTS=$((ATTEMPTS + 1))
done

# Now wait for deployment of kapp-controller.

STATUS=1
ATTEMPTS=0
COMMAND="kubectl rollout status deployment/kapp-controller -n kapp-controller"

until [ $STATUS -eq 0 ] || $COMMAND || [ $ATTEMPTS -eq 12 ]; do
    sleep 5
    $COMMAND
    STATUS=$?
    ATTEMPTS=$((ATTEMPTS + 1))
done
```

Note that you do not need to install ``kapp-controller`` into the virtual cluster if using ``kapp-controller`` in the host Kubernetes as the means to install packages, and nothing in the workshop setup scripts or instructions tries to install additional packages.

When deploying an application in the virtual cluster which includes a Kubernetes `ClusterIP` service, and you need to expose that service to other users in the underlying host Kubernetes cluster using a predictable name, you can declare a mapping for the Kubernetes service.

```yaml
spec:
  session:
    applications:
      vcluster:
        enabled: true
        services:
          fromVirtual:
          - from: my-virtual-namespace/my-virtual-service
            to: my-host-service
```

The ``my-host-service`` created in the underlying host Kubernetes cluster will always be created in the namespace set aside for a workshop session for the virtual cluster. The name of this namespace is available as a data variable ``vcluster_namespace`` in the workshop definition and workshop instructions.

To inject a Kubernetes service from the host Kubernetes cluster into the virtual cluster instance for a workshop, a mapping in the other direction can be specified.

```yaml
spec:
  session:
    applications:
      vcluster:
        enabled: true
        services:
          fromHost:
          - from: $(workshop_namespace)/my-host-service
            to: my-virtual-namespace/my-virtual-service
```

The Kubernetes service being mapped can be from any namespace in the host Kubernetes cluster, but for workshops this would usually be ``$(workshop_namespace)`` if a shared service created from ``environment.objects``, or ``$(session_namespace)`` where created for each workshop session from ``session.objects``.

(enabling-the-local-git-server)=
Enabling the local Git server
-----------------------------

Workshops can sometimes require access to a Git server. This might for example be where a workshop explores deployment of CI/CD pipelines in Kubernetes. Where such workshops only pull static source code from the Git repository one can use hosted Git services such as GitHub or GitLab. If however the workshop instructions require source code to be modified and pushed back to the original Git repository this complicates things because if using a hosted Git service you would need to require each workshop user to create their own account on that service, clone some original source repository into their own account and work with their copy.

To avoid this problem of requiring users to create separate accounts on a Git service, a local Git server can be enabled.  This capability can be enabled by adding the ``session.applications.git`` section to the workshop definition, and setting the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      git:
        enabled: true
```

When this is done a Git server will be hosted out of the workshop container. For use in the interactive terminal, the following environment variables are set:

* ``GIT_PROTOCOL`` - The protocol used to access the Git server. This will be either ``http`` or ``https``.
* ``GIT_HOST`` - The full hostname for accessing the Git server.
* ``GIT_USERNAME`` - The username for accessing the Git server.
* ``GIT_PASSWORD`` - The password for the user account on the Git server.

In workshop instructions the data variable names which can be used are the same but in lower case.

As part of workshop instructions you can have a workshop user create a new empty source code repository on the Git server by running:

```bash
git clone $GIT_PROTOCOL://$GIT_HOST/project.git
```

It is not necessary to supply credentials as the Git configuration for the workshop user account has already been pre-configured to know about the credentials for the Git server. A workshop user would only need to know the credentials if needing to add them into configuration for a separate system, such as a CI/CD pipeline being deployed in Kubernetes.

One the new empty Git repository has been cloned, the workshop user can add new files to the checkout directory, add and commit the changes, and push the changes back to the Git server.

```
~$ git clone $GIT_PROTOCOL://$GIT_HOST/project.git
Cloning into 'project'...
warning: You appear to have cloned an empty repository.

~$ cd project/

~/project$ date > date.txt

~/project$ git add .

~/project$ git commit -m "Initial files."
[main (root-commit) e43277f] Initial files.
 1 file changed, 1 insertion(+)
 create mode 100644 date.txt

~/project$ git push
Enumerating objects: 3, done.
Counting objects: 100% (3/3), done.
Writing objects: 100% (3/3), 264 bytes | 264.00 KiB/s, done.
Total 3 (delta 0), reused 0 (delta 0), pack-reused 0
To https://git-labs-markdown-sample-w01-s001.educates-local-dev.xyz/project.git
 * [new branch]      main -> main
```

If you want to pre-create one or more source code repositories on the Git server, this can be done from a workshop ``setup.d`` script.

```bash
#!/bin/bash

set -eo pipefail
set -x

cd /opt/git/repositories

git clone --bare https://github.com/example/project.git
```

When cloning a repository from a remote Git server you must use the ``--bare`` option to ``git clone``. This is because what is required is not a full checkout of the remote source code repository, but just the bare repository which would normally be found in the ``.git`` directory of a full checkout.

If you need to configure a webhook to be fired whenever changes to a source code repository are pushed to the Git server, you can provide an executable ``post-receive`` [hook script](https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks) in the ``hooks`` directory of the source code repository under ``/opt/git/repositories``.

(enabling-workshop-downloads)=
Enabling workshop downloads
---------------------------

At times you may want to provide a way for a workshop user to download files which are provided as part of the workshop content. This capability can be enabled by adding the ``session.applications.files`` section to the workshop definition, and setting the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      files:
        enabled: true
```

The recommended way of providing access to files from workshop instructions is using the ``files:download-file`` clickable action block. This action will ensure any file is downloaded to the local machine and not simply displayed in the browser in place of the workshop instructions.

By default any files located under the home directory of the workshop user account can be accessed. To restrict where files can be download from, set the ``directory`` setting.

```yaml
spec:
  session:
    applications:
      files:
        enabled: true
        directory: exercises
```

When the specified directory is a relative path, it is evaluated relative to the home directory of the workshop user.

In the case where you want to display the contents of a downloadable file in a separate tab of the workshop dashboard, you can use the ``/files/`` path prefix with the path to the file relative to the download directory root in the URL. The dashboard tab could be created dynamically using the ``dashboard:create-dashboard`` clickable action, or as a permanent dashboard tab by declaring it in the workshop definition.

```yaml
spec:
  session:
    applications:
      files:
        enabled: true
    dashboards:
    - name: Config
      url: /files/config.yaml
```

Access to any downloadable file is by default only possible from the web browser as access is protected via the cookie based authentication used for the workshop session.

If you need to provide a way for a workshop user to download the file from the command line of their local machine using a tool such as ``curl``, you need to include a special token as a query string parameter to the URL, where the token is the general services password made available with a workshop session. This could be used in a clickable action for creating a copy of a command:

~~~text
```workshop:copy
text: curl -o config.yaml {{ingress_protocol}}://{{session_name}}.{{ingress_domain}}/files/config.yaml?token={{services_password}}
```
~~~

(enabling-workshop-uploads)=
Enabling workshop uploads
-------------------------

If a workshop needs to operate against a distinct service or infrastructure that you have separately deployed, you may want to be able to upload a configuration file for access. You may also just want to upload source files into the workshop container. This capability can be enabled by adding the ``session.applications.uploads`` section to the workshop definition, and setting the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      uploads:
        enabled: true
```

To upload files, the workshop instructions can then use the ``files:upload-file`` or ``files:upload-files`` clickable actions to upload a single name file, or an arbitrary set of files.

By default, any files which are uploaded will be placed under the ``uploads`` subdirectory of the workshop user's home directory.

If you want to specify an alternate location for files to be uploaded, you can set the ``directory`` property. This path will be intepreted relative to the workshop user's home directory. If the ``directory`` property is set to the empty string, files will be placed under the workshop user's home directory.

```yaml
spec:
  session:
    applications:
      uploads:
        enabled: true
        directory: uploads
```

The ability to upload a file is by default only possible from the web browser as access is protected via the cookie based authentication used for the workshop session.

If you need to provide a way for a workshop user to upload a single file from the command line of their local machine using a tool such as ``curl``, you need to include a special token as a query string parameter to the URL, where the token is the general services password made available with a workshop session. This could be used in a clickable action for creating a copy of a command:

~~~text
```workshop:copy
text: curl -F path=example.yaml -F file=@example.yaml '{{ingress_protocol}}://{{session_name}}.{{ingress_domain}}/upload/file?token={{services_password}}'
```
~~~

Multiple files can be uploaded using the alternative ``curl`` command line of:

~~~text
```workshop:copy
text: curl -F files=@example-1.yaml -F files=@example-2.yaml '{{ingress_protocol}}://{{session_name}}.{{ingress_domain}}/upload/files?token={{services_password}}'
```
~~~

Note that the form parameters used in each case, as well as the upload URL, differ for the single and multiple file use cases.

Enabling the test examiner
--------------------------

The test examiner is a feature which allows a workshop to have verification checks which can be triggered from the workshop instructions. The test examiner is disabled by default. If you want to enable it, you need to add a ``session.applications.examiner`` section to the workshop definition, and set the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      examiner:
        enabled: true
```

Any executable test programs to be used for verification checks need to be provided in the ``workshop/examiner/tests`` directory.

The test programs should return an exit status of 0 if the test is successful and non zero if a failure. The test programs should not be persistent programs that would run forever.

Clickable actions for the test examiner are used within the workshop instructions to trigger the verification checks, or they can be configured to be automatically started when the page of the workshop instructions is loaded.

(enabling-session-image-registry)=
Enabling session image registry
-------------------------------

Workshops using tools such as ``kpack`` or ``tekton`` and which need a place to push container images when built, can enable an image registry. A separate image registry is deployed for each workshop session.

Note that the image registry is only currently fully usable if workshops are deployed under an Educates operator configuration which uses secure ingress. This is because an insecure registry would not be trusted by the Kubernetes cluster as the source of container images when doing deployments.

To enable the deployment of an image registry per workshop session you need to add a ``session.applications.registry`` section to the workshop definition, and set the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      registry:
        enabled: true
```

The image registry will mount a persistent volume for storing of images. By default the size of that persistent volume is 5Gi. If you need to override the size of the persistent volume add the ``storage`` property under the ``registry`` section.

```yaml
spec:
  session:
    applications:
      registry:
        enabled: true
        storage: 20Gi
```

If instead of declaring the size for storage, you want to create the persistent volume claim yourself, for example as part of ``session.objects``, you can specify the name of the separate persistent volume claim. This may include a ``subPath`` within the persistent volume you want to use.

```yaml
spec:
  session:
    resources:
      volume:
        name: $(session_name)-registry
        subPath: storage
```

The amount of memory provided to the image registry will default to 768Mi. If you need to increase this, add the ``memory`` property under the ``registry`` section.

```yaml
spec:
  session:
    applications:
      registry:
        enabled: true
        memory: 1Gi
```

The image registry will be secured with a username and password unique to the workshop session and expects access over a secure connection.

To allow access from the workshop session, the file ``$HOME/.docker/config.json`` containing the registry credentials will be injected into the workshop session. This will be automatically used by tools such as ``docker``.

For deployments in Kubernetes, a secret of type ``kubernetes.io/dockerconfigjson`` is created in the namespace and automatically applied to the ``default`` service account in the namespace. This means deployments made using the default service account will be able to pull images from the image registry without additional configuration. If creating deployments using other service accounts, you will need to add configuration to the service account or deployment to add the registry secret for pulling images.

If you need access to the raw registry host details and credentials, they are provided as environment variables in the workshop session. The environment variables are:

* ``REGISTRY_HOST`` - Contains the host name for the image registry for the workshop session.
* ``REGISTRY_AUTH_FILE`` - Contains the location of the ``docker`` configuration file. Should always be the equivalent of ``$HOME/.docker/config.json``.
* ``REGISTRY_USERNAME`` - Contains the username for accessing the image registry.
* ``REGISTRY_PASSWORD`` - Contains the password for accessing the image registry. This will be different for each workshop session.
* ``REGISTRY_AUTH_TOKEN`` - Contains username and password for accessing the image registry, separated by a colon and base64 encoded.
* ``REGISTRY_SECRET`` - Contains the name of a Kubernetes secret of type ``kubernetes.io/dockerconfigjson`` added to the session namespace and which contains the registry credentials.

The URL for accessing the image registry adopts the HTTP protocol scheme inherited from the environment variable ``INGRESS_PROTOCOL``. This would be the same HTTP protocol scheme as the workshop sessions themselves use.

If you want to use any of the variables in workshop content, use the same variable name but in lower case. Thus, ``registry_host``, ``registry_auth_file``, ``registry_username``, ``registry_password``, ``registry_auth_token`` and ``registry_secret``.

The ``registry_host``, ``registry_username``, ``registry_password``, ``registry_auth_token`` and ``registry_secret`` will also be available as additional session data variables you can use in the workshop definition.

(enabling-ability-to-use-docker)=
Enabling ability to use docker
------------------------------

If you need to be able to build container images in a workshop using ``docker``, it needs to be enabled first. Each workshop session will be provided with its own separate docker daemon instance running in a container.

Note that enabling of support for running ``docker`` requires the use of a privileged container for running the docker daemon. Because of the security implications of providing access to docker with this configuration, it is strongly recommended that if you don't trust the people doing the workshop, any workshops which require docker only be hosted in a disposable Kubernetes cluster which is destroyed at the completion of the workshop. You should never enable docker for workshops hosted on a public service which is always kept running and where arbitrary users could access the workshops.

To enable support for being able to use ``docker`` add a ``session.applications.docker`` section to the workshop definition, and set the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      docker:
        enabled: true
```

The container which runs the docker daemon will mount a persistent volume for storing of images which are pulled down or built locally. By default the size of that persistent volume is 5Gi. If you need to override the size of the persistent volume add the ``storage`` property under the ``docker`` section.

```yaml
spec:
  session:
    applications:
      docker:
        enabled: true
        storage: 20Gi
```

If instead of declaring the size for storage, you want to create the persistent volume claim yourself, for example as part of ``session.objects``, you can specify the name of the separate persistent volume claim. This may include a ``subPath`` within the persistent volume you want to use.

```yaml
spec:
  session:
    resources:
      volume:
        name: $(session_name)-docker
        subPath: storage
```

The amount of memory provided to the container running the docker daemon will default to 768Mi. If you need to increase this, add the ``memory`` property under the ``registry`` section.

```yaml
spec:
  session:
    applications:
      docker:
        enabled: true
        memory: 1Gi
```

Access to the docker daemon from the workshop session uses a local UNIX socket shared with the container running the docker daemon. If using a local tool which wants to access the socket connection for the docker daemon directly rather than by running ``docker``, it should use the ``DOCKER_HOST`` environment variable to determine the location of the socket.

The docker daemon is only available from within the workshop session and cannot be accessed outside of the pod by any tools deployed separately to Kubernetes.

If you want to automatically start services running in docker when the workshop session is started, they can be started from a `setup.d` script file. Alternatively you can provide configuration for any services in the workshop definition using configuration compatible with `docker compose`. The configuration for the services should be added under ``session.applications.docker.compose``.

```yaml
spec:
  session:
    applications:
      docker:
        enabled: true
        compose:
          services:
            grafana-workshop:
              image: grafana/grafana:7.1.3
              ports:
              - "127.0.0.1:3000:3000"
              environment:
              - GF_AUTH_ANONYMOUS_ENABLED=true
            influxdb-workshop:
              image: influxdb:1.8.1
              ports:
              - "127.0.0.1:8086:8086"
              environment:
              - INFLUXDB_DB=workshop
              - INFLUXDB_USER=workshop
              - INFLUXDB_USER_PASSWORD=$(session_name)
```

Note that ports need to be explicitly exposed to ``127.0.0.1`` to be accessible from the workshop container.

If a specific service needs to access the workshop files, they can include a volume mount of type ``volume``, with the required ``target`` mount point, and where the ``source`` name of the volume is ``workshop``. This will be remapped to an appropriate file system mount depending on how the workshop session is being deployed.

```yaml
spec:
  session:
    applications:
      docker:
        enabled: true
        compose:
          services:
            service-workshop:
              volumes:
              - type: volume
                source: workshop
                target: /mnt
```

When services are provided in this way, by default the docker socket will not be made available in the workshop container, where as if no services were defined it would be.

In order to explictly indicate that the docker socket should be made available in the workshop container, set ``session.applications.docker.socket.enabled`` to ``true``.

```yaml
spec:
  session:
    applications:
      docker:
        enabled: true
        socket:
          enabled: true
```

(enabling-remote-ssh-access)=
Enabling remote SSH access
--------------------------

The interactive terminals for a workshop session can be accessed by a workshop user through their browser. The only other means usually available for accessing a workshop session container is by using ``kubectl exec``, but this requires the client to have a ``kubeconfig`` file associated with a service account which has appropriate privileges granted via RBAC for accessing the workshop pod. For an untrusted workshop user this means of access is not safe however, as in providing access for ``kubectl exec`` to work, they can view sensitive information about the workshop pod, including potentially credentials.

If you need to provide an alternative means of access to the workshop container, an SSH daemon can be enabled and run in the workshop container. To enable support add a ``session.applications.sshd`` section to the workshop definition, and set the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      sshd:
        enabled: true
```

This will enable SSH access to the workshop container from within the Kubernetes cluster. The hostname used for access is that of the Kubernetes service for the workshop pod. That is, the equivalent of the following SSH command could be used:

```shell
ssh eduk8s@$SESSION_NAME.$WORKSHOP_NAMESPACE
```

The only user in the workshop container that can be exposed is that of the workshop user. In order for access to work, the client side must have a copy of the SSH private key for the workshop user. This is available in the workshop container at ``$HOME/.ssh/id_rsa``, but is also available in the workshop namespace in the Kubernetes secret with name given by ``$(ssh_keys_secret)``, which a deployment created from ``session.objects`` could depend on if needing to access the workshop container over SSH.

In order to be able to access the workshop container over SSH from outside of the cluster, an SSH tunneling proxy can be enabled for the workshop using:

```yaml
spec:
  session:
    applications:
      sshd:
        enabled: true
        tunnel:
          enabled: true
```

The SSH tunneling proxy uses websockets to provide access and so any SSH client needs to be configured with a proxy command to use a special program to manage access over the websocket for each SSH connection.

At this time no standalone program is provided to manage the tunnel, but an experimental client is provided as part of the Educates CLI for testing. In order to use this, it is necessary to add to a remote users local SSH config (usually the file ``$HOME/.ssh/config``) the following:

```
Host *.educates-local-dev.xyz
  User eduk8s
  StrictHostKeyChecking no
  IdentitiesOnly yes
  IdentityFile ~/.ssh/%h.key
  ProxyCommand educates tunnel connect --url wss://%h/tunnel/
```

The ``Host`` header should be the wildcard domain corresponding to the ingress domain used for Educates.

The SSH private key for the workshop is still required by the remote client. This could be downloaded by a workshop user as part of the workshop instructions using the clickable action for file download:

~~~
```files:download-file
path: .ssh/id_rsa
download: {{session_name}}.{{ingress_domain}}.key
```
~~~

The workshop user would need to manually move this file into the ``~/.ssh`` directory and ensure it has file mode permissions of ``0600``.

With that done they should then be able to access the workshop container by running ``ssh`` and giving it the fully qualified hostname of the workshop session as argument. That is, the equivalent of:

```shell
ssh $SESSION_NAME.$INGRESS_DOMAIN
```

A user does not need to be specified in this case as it is mapped in the local SSH config file to the required user.

In addition to being able to use the ``ssh`` command line client, it is possible to use other SSH clients, such as that used by VS Code and other IDEs to implement remote workspaces over SSH.

Note that the Educates CLI is an internal tool and should not be redistributed to external users. Sample code in Python and Go is available if you want to create a standalone client for handling the connection via the websocket proxy tunnel. Please talk to the Educates developers before going down a path of implementing a workshop which makes use of the SSH access mechanism.

Enabling WebDAV access to files
-------------------------------

Local files within the workshop session can be accessed or updated from the terminal command line or editor of the workshop dashboard. The local files reside in the filesystem of the container the workshop session is running in.

If there is a need to be able to access the files remotely, it is possible to enable WebDAV support for the workshop session.

To enable support for being able to access files over WebDAV add a ``session.applications.webdav`` section to the workshop definition, and set the ``enabled`` property to ``true``.

```yaml
spec:
  session:
    applications:
      webdav:
        enabled: true
```

The result of this will be that a WebDAV server will be run within the workshop session environment. A set of credentials will also be automatically generated which are available as environment variables. The environment variables are:

* ``WEBDAV_USERNAME`` - Contains the username which needs to be used when authenticating over WebDAV.
* ``WEBDAV_PASSWORD`` - Contains the password which needs to be used authenticating over WebDAV.

If you need to use any of the environment variables related to the image registry as data variables in workshop content, you will need to declare this in the ``workshop/modules.yaml`` file in the ``config.vars`` section.

```yaml
config:
  vars:
  - name: WEBDAV_USERNAME
  - name: WEBDAV_PASSWORD
```

The URL endpoint for accessing the WebDAV server is the same as the workshop session, with ``/webdav/`` path added. This can be constructed from the terminal using:

```text
$INGRESS_PROTOCOL://$SESSION_NAME.$INGRESS_DOMAIN/webdav/
```

In workshop content it can be constructed using:

```text
{{ingress_protocol}}://{{session_name}}.{{ingress_domain}}/webdav/
```

You should be able to use WebDAV client support provided by your operating system, of by using a standalone WebDAV client such as [CyberDuck](https://cyberduck.io/).

Using WebDAV can make it easier if you need to transfer files to or from the workshop session.

Customizing the terminal layout
-------------------------------

By default a single terminal is provided in the web browser when accessing the workshop. If required, you can enable alternate layouts which provide additional terminals. To set the layout, you need to add the ``session.applications.terminal`` section and include the ``layout`` property with the desired layout.

```yaml
spec:
  session:
    applications:
      terminal:
        enabled: true
        layout: split
```

The options for the ``layout`` property are:

* ``default`` - Single terminal.
* ``split`` - Two terminals stacked above each other in ratio 60/40.
* ``split/2`` - Three terminals stacked above each other in ratio 50/25/25.
* ``lower`` - A single terminal is placed below any dashboard tabs, rather than being a tab of its own. The ratio of dashboard tab to terminal is 70/30.
* ``none`` - No terminal is displayed, but they can still be created from the drop down menu.

When adding the ``terminal`` section, you must include the ``enabled`` property and set it to ``true`` as it is a required field when including the section.

If you didn't want a terminal displayed, and also wanted to disable the ability to create terminals from the drop down menu, set ``enabled`` to ``false``.

(adding-custom-dashboard-tabs)=
Adding custom dashboard tabs
----------------------------

Exposed applications, external sites and additional terminals, can be given their own custom dashboard tab. This is done by specifying the list of dashboard panels and the target URL.

```yaml
spec:
  session:
    ingresses:
    - name: application
      port: 8080
    dashboards:
    - name: Internal
      url: "$(ingress_protocol)://application-$(session_name).$(ingress_domain)/"
    - name: External
      url: http://www.example.com
```

The URL values can reference any of the session data variables which may be appropriate.

The URL can reference an external web site if required. Do note however, that any web site must not prohibit being embedded in a HTML iframe. Further, if Educates is configured to use secure ingress, the site being embedded in the dashboard cannot use HTTP and must also use a secure HTTPS URL otherwise the browser will prohibit accessing the embedded site due to mixed content.

In the case of wanting to have a custom dashboard tab provide an additional terminal, the ``url`` property should use the form ``terminal:<session>``, where ``<session>`` is replaced with the name of the terminal session. The name of the terminal session can be any name you choose, but should be restricted to lower case letters, numbers and '-'. You should avoid using numeric terminal session names such as "1", "2" and "3" as these are use for the default terminal sessions.

```yaml
spec:
  session:
    dashboards:
    - name: Example
      url: terminal:example
```

Note that by default builtin tabs will be displayed in order first with the terminal tab always being the first. If you want to override the order, you can supply empty entries for the builtin dashboard tabs, with the desired location relative to the other tabs.

```yaml
spec:
  session:
    dashboards:
    - name: Editor
    - name: Terminal
    - name: Example
      url: terminal:example
```

When doing this, if you don't add an entry for all builtin dashboard tabs which are enabled, then those you don't list will be added last.
