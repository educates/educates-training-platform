Working on Content
==================

Workshop content will either be packaged up as an OCI image artifact, hosted in a Git repository, or on a web server, with the conent downloaded when the workshop session is created. Alternatively it could be built into a custom workshop image. To speed up the iterative loop of editing and testing a workshop when developing workshop content, the `educates` CLI provides various commands to faciliate development of workshop content in a local environment. When using the `educates` CLI many of the details of how everything is configured to achieve this is hidden. This document covers some details of using the `educates` CLI, but also aims to explain what some of the underlying configuraton is in case you need to work on workshop content in a hosted cluster and the `educates` CLI cannot be used.

Publishing of workshop content
------------------------------

When using the `educates` CLI to create a local Kubernetes cluster using Kind, it will also deploy an image registry for you automatically and link it into the Kubernetes cluster. This registry can be used to hold published workshops.

When using this capability the workshop definition would need to be in the form:

```yaml
spec:
  publish:
    image: $(image_repository)/{name}-files:$(workshop_version)
  workshop:
    files:
    - image:
        url: $(image_repository)/{name}-files:$(workshop_version)
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
```

where `{name}` would be the name of the workshop.

When publishing this workshop using the command:

```
educates publish-workshop
```

the value of `$(image_repository)` in the destination for publishing the workshop as specified by `publish.image` will be replaced with `localhost:5001` and `$(workshop_version)` will be replaced with `latest`.

If you subsequently run:

```
educates deploy-workshop
```

to deploy the workshop to the local Kubernetes cluster, when evaluating `workshop.files.image.url`, the value of `$(image_repository)` will be replaced with `registry.default.svc.cluster.local`, which is the internal hostname for the local registry when accessed from inside of the Kubernetes cluster.

In the case of accessing the registry from inside of the cluster using tools such as `imgpkg`, they will allow insecure access because the image registry has a `.local` address. Some other tools may require you to tell them to access it in an insecure way. The latter may be the case when accessing the registry as `localhost:5001` from your local system also.

If working against a remote Kubernetes cluster it will not have access to the local registry so you can instead use a remote registry to host public workshops instead.

To publish to a remote registry you can use a command like:

```
educates publish-workshop --image-repository docker.io/username
```

Depending on the remote registry type, you should either be logged in using `docker login`, or you need to supply credentials on the command line to `educates publish-workshop`.

With the workshop published to the registry, if the registry was publicly accessible and you wanted someone else to be able to deploy the workshop, you could run:

```
educates publish-workshop --image-repository docker.io/username --workshop-version 1.0 --export-workshop published-workshop.yaml
```

When this is done a modified version of the workshop defintion will be written to the file `publish-workshop.yaml` with any references to `$(image_repository)` being replaced with the remote registry address. Supplying `--workshop-version` allows you to specify a specific version for the publish workshop rather than `latest`.

If you need to generate the modified workshop definition separate to publishing it, you can run:

```
educates export-workshop --image-repository docker.io/username --workshop-version 1.0 > published-workshop.yaml
```

The recipient of the modified workshop definition could use:

```
educates deploy-workshop -f published-workshop.yaml
```

to publish the workshop, or use `kubectl` to apply it to a cluster and register the workshop with a training portal.

Local build of workshop image
-----------------------------

When using the local Kubernetes cluster created using the `educates` CLI, the local image registry created can also be used to hold a custom workshop base image if used. In this case the workshop definition can be defined as:

```yaml
spec:
  workshop:
    image: $(image_repository)/{name}-image:$(workshop_version)
```

As with the published workshop files, the `$(image_repository)` and `$(workshop_version)` variable references will be automatically replaced with appropriate values when a workshop is deployed to the local Kubernetes cluster.

The address of the local image registry will be `localhost:5001` when accessed from your local machine. Access to the registry will be insecure, but local tools like `docker` will allow insecure access since the host is `localhost`.

The host of the registry when accessed from inside of the local cluster will be `registry.default.svc.cluster.local`. When accessed from inside of the cluster the default port 80 will be used and access is still not secure, however, the `containerd` process in Kubernetes will be configured to trust the registry.

When using `educates publish-workshop --export-workshop` or `educates export-workshop` with a remote image registry the variable references for `$(image_repository)` and `$(workshop_version)` in `workshop.image` will also be replaced. This means the custom workshop image would need to be hosted under the same image registry as the workshop was published.

Disabling reserved sessions
---------------------------

An instance of a training portal should be used when developing content where reserved sessions are disabled.

```
apiVersion: training.educates.dev/v1beta1
kind: TrainingPortal
metadata:
  name: lab-sample-workshop
spec:
  portal:
    sessions:
      maximum: 1
  workshops:
  - name: lab-sample-workshop
    reserved: 0
    expires: 120m
    orphaned: 15m
```

If you don't disable reserved sessions then a new session will always be created ready for the next workshop session when there is available capacity to do so. If you are modifying workshop content while testing the current workshop session, then terminate the session and start a new one it will pickup the reserved session, which will still have a copy of the old content.

By disabling reserved sessions a new workshop session will always be created on demand, ensuring the latest workshop content is used.

Note that there can be a slight delay in being able to create a new workshop as the existing workshop session will need to be shutdown first. The new workshop session may also take some time to start if an updated version of the workshop image has to be pulled down.

(accessing-workshop-error-logs)=
Accessing workshop error logs
-----------------------------

If workshop content is not able to be downloaded due to an error, an extension package does not arrive, a setup script included with the workshop content fails, or workshop instructions cannot be rendered when using the Hugo renderer, an error dialog will be displayed when the workshop session dashboard is displayed. The dialog will point you at the error logs for details. You have two options for finding details of the errors in these cases.

The first way is to determine the name of the deployment for the workshop session and which namespace it is in, and use the ``kubectl logs`` command to access the logs. 

The second and easier way if you have access to the workshop session dashboard, is to use the embedded terminal to look at the log files located under the directory ``$HOME/.local/share/workshop``.

The two main log files are:

* ``$HOME/.local/share/workshop/download-workshop.log``
* ``$HOME/.local/share/workshop/setup-scripts.log``

You can tell in which phase the error occurred, as there will be a corresponding marker file in the same directory, with same basename, but with ``.failed`` extension.

Extension packages are reported in ``download-workshop.log`` along with the workshop content, whether they were downloaded or mounted into the session. Each extension package declared as an image contributes one line naming the package and how it arrived, and a package which did not arrive is named on a line of its own saying that its package manifest is missing. The most common cause is an image reference which points at an ordinary application image rather than an image built as an extension package.

Live updates to the content
---------------------------

If workshop content is being downloaded as an OCI image artifact, from a Git repository, or a web server, and you are only doing simple updates to workshop instructions or scripts or files bundled with the workshop, you can update the content in place without needing to restart the workshop session. To perform an update, after you have pushed back any changes to the hosted Git repository or updated the content available via the web server, from the workshop session terminal run:

```
update-workshop
```

This command will download any workshop content from the OCI artifact registry, Git repository or web server, then unpack it into the live workshop session and re-run any script files found in the ``workshop/setup.d`` directory.

Once the workshop content has been updated you can reload the current page of the workshop instructions by clicking on the reload icon on the dashboard while holding down the `<SHIFT>` key.

When running this command, any errors which occur in downloading the workshop content, running the setup script, or rendering the workshop instructions, will be output to the terminal so this command can be used instead of consulting the logs to look for errors. Do be aware though that since this can be the second time setup scripts have run, the behaviour may not be the same as the first time they have run. So for original details of errors you will still need to look at the log files.

Note that if using the `classic` renderer and additional pages were added to the workshop instructions, or pages renamed, you will need to restart the workshop renderer process. This can be done by running:

```
restart-workshop
```

If using the `hugo` renderer you do not not need to run the `restart-workshop` and doing so will have no affect.

So long as you hadn't renamed or deleted the current page you are viewing, you can trigger a reload of the current page. If you have eliminated the current page by deleting it or renaming it, click on the home icon or refresh the whole browser window.

If action blocks within the workshop instructions are broken and you wanted to make a change to the workshop instructions within the live workshop session to test, edits can be made to the appropriate page under ``/opt/workshop/content``. If using the `classic` renderer you can then navigate to the modified page or reload it to verify the change. If using the `hugo` renderer, you will first need to run the command:

```
rebuild-content
```

If wanting to make a change to setup scripts which create files specific to a workshop session and re-run them, make the edit to the script under ``/opt/workshop/setup.d``. To trigger running of any setup scripts, then run:

```
rebuild-workshop
```

If local changes to the workshop session check out okay then you can modify the file back in the original Git repository where you are keeping content.

Note that updating workshop content in a live session in this way isn't going to undo any deployments or changes you make in the Kubernetes cluster for that session. So if wanting to retest parts of the workshop instructions you may have to manually undo changes in the cluster in order to replay them. This will depend on your specific workshop content.

Custom workshop image changes
-----------------------------

If your workshop is using a custom workshop image because of the need to provide additional tools, and you have as a result also included the workshop instructions as part of the workshop image, during development of workshop content always use an image tag of ``main``, ``master``, ``develop`` or ``latest``, do not use a version image reference.

```
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-sample-workshop
spec:
  title: Sample Workshop
  description: A sample workshop
  workshop:
    image: ghcr.io/educates/lab-sample-workshop:latest
```

When an image tag of ``main``, ``master``, ``develop`` or ``latest`` is used, the image pull policy will be set to ``Always`` ensuring that the custom workshop image will be pulled down again for a new workshop session if the remote image had changed. If the image tag was for a specific version, it would be necessary to change the workshop definition every time there was a change to the workshop image.

Custom workshop image overlay
-----------------------------

Even where you have a custom workshop image, setup the workshop definition to also pull down the workshop content from the hosted Git repository or web server.

```
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-sample-workshop
spec:
  title: Sample Workshop
  description: A sample workshop
  workshop:
    image: ghcr.io/educates/lab-sample-workshop-image:latest
    files:
    - image:
        url: ghcr.io/educates/lab-sample-workshop-files:latest
```

By pulling down the workshop content as an overlay when the workshop session started, on top of the contents of the custom workshop image, you only need to rebuild the custom workshop image when needing to make changes to what additional tools are needed, or when you want to ensure the latest workshop instructions are also a part of the final custom workshop image.

Using this method, as the location of the workshop files is known you can then also do live updates of workshop content in the session as described previously.

If the additional set of tools required for a workshop is not too specific to a workshop, it is recommended to create a standalone workshop base image where just the tools are added. Content for a specific workshop would then always be pulled down when the workshop session is started.

```
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-sample-workshop
spec:
  title: Sample Workshop
  description: A sample workshop
  workshop:
    image: ghcr.io/educates/custom-environment:latest
    files:
    - image:
        url: ghcr.io/educates/lab-sample-workshop-files:latest
```

This separates generic tooling from specific workshops and allows the custom workshop base image to be used for multiple workshops on different, but related topics, which require the same tooling.

(proxy-to-local-workshop-content)=
Proxy to local workshop content
-------------------------------

The `educates` CLI provides a way to serve workshop instructions from your local host system and have it embedded in the workshop session hosted within the cluster. This is triggered by running from the local copy of the workshop files the command:

```
educates serve-workshop --patch-workshop
```

**NOTE**: For this to work, you need [hugo](https://gohugo.io/) to be available on your development machine.

The `--patch-workshop` command in this case will cause the workshop definition for the workshop to be patched so that workshop instructions will be sourced from a HTTP server run by the `educates serve-workshop` command.

Under the covers what the `--patch-workshop` command is doing is injecting the following configuration into the workshop definition.

```yaml
spec:
  workshop:
    enabled: true
    proxy:
      changeOrigin: false
      headers:
      - name: X-Session-Name
        value: $(session_name)
      host: localhost.$(ingress_domain)
      port: 10081
      protocol: http
```

Rather than relying on the `--patch-workshop` option, you could instead add this to the workshop definition yourself and then run the `educates CLI` as just:

```
educates serve-workshop
```

Note that when the `--patch-workshop` is used, in addition to the above configuration, it is also automatically configuring a shared secret token so that access from the workshop session is trusted. This will not be present if you inject this specific configuration yourself and anyone could access the local workshop instructions.

If you want to secure access via a shared access token you should instead use:

```yaml
spec:
  workshop:
    enabled: true
    proxy:
      changeOrigin: false
      headers:
      - name: X-Session-Name
        value: $(session_name)
      - name: X-Access-Token
        value: secret-token
      protocol: http
      host: localhost.$(ingress_domain)
      port: 10081
```

When this is done you should then run the CLI as:

```
educates serve-workshop --access-token secret-token
```

In the example above and when using the `educates` CLI to patch the workshop definition, the Kubernetes cluster needs to be running on the local system under Kind. If manually modifying the workshop definition to add the configuration, you can override the `protocol`, `host` and `port` to specify a different target. This would allow you for example to use a Cloudflare Tunnel to expose port 10081 from your local system via a public hostname. You could then set the configuration to access the locally served content from your your system via the Cloudflare Tunnel, even when using a distinct hosted Kubernetes cluster.

```yaml
spec:
  workshop:
    enabled: true
    proxy:
      changeOrigin: false
      headers:
      - name: X-Session-Name
        value: $(session_name)
      - name: X-Access-Token
        value: secret-token
      protocol: https
      host: tunnel.example.com
      port: 443
```

If using the `educates` CLI to deploy the workshop to a separate hosted cluster, you can still have it patch the workshop definition when using such a tunnel by running:

```
educates serve-workshop --patch-workshop --proxy-protocol https --proxy-host tunnel.example.com --proxy-port 443
```

Changes to workshop defintion
-----------------------------

By default, if you need to modify the definition for a workshop, you would need to delete the training portal instance, update the workshop definition in the cluster, and recreate the training portal.

During development of workshop content, when working on the workshop definition itself to change things like resource allocations, role access, or what resource objects are automatically created for the workshop environment or a specific workshop session, you can in the training portal definition enable automatic updates on changes to the workshop definition.

```
apiVersion: training.educates.dev/v1beta1
kind: TrainingPortal
metadata:
  name: lab-sample-workshop
spec:
  portal:
    sessions:
      maximum: 1
    updates:
      workshop: true
  workshops:
  - name: lab-sample-workshop
    expires: 120m
    orphaned: 15m
```

With this option enabled, whenever the workshop definition in the cluster is modified, the existing workshop environment managed by the training portal for that workshop, will be shutdown and replaced with a new workshop environment using the updated workshop definition.

In the case there is still an active workshop session running, the actual deletion of the old workshop environment will be delayed until that workshop session is terminated.
