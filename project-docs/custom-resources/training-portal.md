Training Portal
===============

The ``TrainingPortal`` custom resource triggers the deployment of a set of workshop environments and a set number of workshop instances.

The raw custom resource definition for the ``TrainingPortal`` custom resource can be viewed by running:

```
kubectl get crd/trainingportals.training.educates.dev -o yaml
```

Specifying the workshop definitions
-----------------------------------

Running multiple workshop instances to perform training to a group of people can be done by following the step wise process of creating the workshop environment, and then creating each workshop instance. The ``TrainingPortal`` workshop resource bundles that up as one step.

Before creating the training portal you still need to load the workshop definitions as a separate step.

To specify the names of the workshops to be used for the training, list them under the ``workshops`` field of the training portal specification. Each entry needs to define a ``name`` property, matching the name of the ``Workshop`` resource which was created.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: TrainingPortal
metadata:
  name: sample-workshops
spec:
  portal:
    sessions:
      maximum: 8
  workshops:
  - name: lab-asciidoc-sample
  - name: lab-markdown-sample
```

When the training portal is created, it will setup the underlying workshop environments, create any workshop instances required to be created initially for each workshop, and deploy a web portal for attendees of the training to access their workshop instances.

When requesting workshop sessions via the REST API, either the workshop environment name or the workshop name can be used. Using the workshop name is easier as the name of the workshop environment can change over time if it is refreshed or replaced, and a client would also need to search itself through the workshop environments to find that for the desired workshop name. The name of the workshop though, because it is the name of the ``Workshop`` resource, may be cryptic if the name incorporates versioning information. To make lookup of a workshop easier via the REST API, the ``alias`` property can be supplied which maps the published name for the workshop to an alternate value. The ``alias`` can if desired use mixed case, punctuation and spaces. Whether ``name`` or ``alias`` is used, they must be unique across the set of workshops hosted by the training portal.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: TrainingPortal
metadata:
  name: sample-workshops
spec:
  portal:
    sessions:
      maximum: 8
  workshops:
  - name: lab-asciidoc-sample-v1
    alias: lab-asciidoc-sample
  - name: lab-markdown-sample-v2
    alias: lab-markdown-sample
```

Note that you should never use a name for a training portal which starts with a ``kube-`` prefix. This is because namespaces are created using the name of the training portal as prefix and the ``kube-`` prefix on namespace names is reserved for Kubernetes system namespaces. Although the names used may not actually clash with any used by Kubernetes at the current time, you will see issues with the Educates secrets manager, which will not apply rules when the name of namespace is seen as being one reserved for Kubernetes system namespaces. This is a fail safe to make sure Educates doesn't interfere with anything running out of the Kubernetes system namespaces. An example of what may result is that when a workshop has enabled the per session image registry, the image registry pull secret will not be injected into the ``default`` service account as this behaviour relies on a single generic rule that looks at all namespaces (excluding Kubernetes reserved namespaces), rather than a rule per workshop session where the name of the namespace is given explicitly.

Limiting the number of sessions
-------------------------------

When defining the training portal, you can set a limit on the workshop sessions that can be run concurrently. This is done using the ``portal.sessions.maximum`` property.

```yaml
spec:
  portal:
    sessions:
      maximum: 8
```

When this is specified, the maximum capacity of each workshop will be set to the same maximum value for the portal as a whole. This means that any one workshop can have as many sessions as specified by the maximum, but to achieve that only instances of that workshops could have been created. In other words the maximum applies to the total number of workshop instances created across all workshops.

Note that if you do not set ``portal.sessions.maximum``, you must set the capacity for each individual workshop as detailed below. In only setting the capacities of each workshop and not an overall maximum for sessions, you can't share the overall capacity of the training portal across multiple workshops.

Capacity of individual workshops
--------------------------------

When you have more than one workshop, you may want to limit how many instances of each workshop you can have so that they cannot grow to the maximum number of sessions for the whole training portal, but a lessor maximum. This means you can stop one specific workshop taking over all the capacity of the whole training portal. To do this set the ``capacity`` field under the entry for the workshop.

```yaml
spec:
  portal:
    sessions:
      maximum: 8
  workshops:
  - name: lab-asciidoc-sample
    capacity: 4
  - name: lab-markdown-sample
    capacity: 6
```

The value of ``capacity`` caps the number of workshop sessions for the specific workshop at that value. It should always be less than or equal to the maximum number of workshops sessions as the latter always sets the absolute cap.

Set reserved workshop instances
-------------------------------

By default, one instance of each of the listed workshops will be created up front so that when the initial user requests that workshop, it is available for use immediately.

When such a reserved instance is allocated to a user, provided that the workshop capacity hasn't been reached a new instance of the workshop will be created as a reserve ready for the next user. When a user ends a workshop, if the workshop had been at capacity, when the instance is deleted, then a new reserve will be created. The total of allocated and reserved sessions for a workshop cannot therefore exceed the capacity for that workshop.

If you want to override for a specific workshop how many reserved instances are kept in standby ready for users, you can set the ``reserved`` setting against the workshop.

```yaml
spec:
  portal:
    sessions:
      maximum: 8
  workshops:
  - name: lab-asciidoc-sample
    capacity: 4
    reserved: 2
  - name: lab-markdown-sample
    capacity: 6
    reserved: 4
```

The value of ``reserved`` can be set to 0 if you do not ever want any reserved instances for a workshop and you instead only want instances of that workshop created on demand when required for a user. Only creating instances of a workshop on demand can result in a user needing to wait longer to access their workshop session.

Note that in this instance where workshop instances are always created on demand, but also in other cases where reserved instances are tying up capacity which could be used for a new session of another workshop, the oldest reserved instance will be terminated to allow a new session of the desired workshop to be created instead. This will occur so long as any caps for specific workshops are being satisfied.

Override initial number of sessions
-----------------------------------

The initial number of workshop instances created for each workshop will be what is specified by ``reserved``, or 1 if the setting wasn't provided.

In the case where ``reserved`` is set in order to keep workshop instances on standby, you can indicate that initially you want more than the reserved number of instances created. This is useful where you are running a workshop for a set period of time. You might create up front instances of the workshop corresponding to 75% of the expected number of attendees, but with a smaller reserve number. With this configuration, new reserve instances would only start to be created when getting close to the 75% and all of the extra instances created up front have been allocated to users. This way you can ensure you have enough instances ready for when most people show up, but then can create others if necessary as people trickle in later.

```yaml
spec:
  portal:
    sessions:
      maximum: 100
  workshops:
  - name: lab-kubernetes-fundamentals
    initial: 75
    reserved: 5
```

If ``initial`` is non zero but set to be less than ``reserved``, the supplied value will be overridden and the number specified by ``reserved`` created.

If ``initial`` is zero, then no reserved sessions will be created initially, but once the first request for a session comes in for a specific workshop, that will be created on demand, with additional sessions created at that point to make up the required reserved number for that workshop.

Setting defaults for all workshops
----------------------------------

If you have a list of workshops and they all need to be set with the same values for ``capacity``, ``reserved`` and ``initial``, rather than add the settings to each, you can set defaults to apply to each under the ``portal.workshop.defaults`` section instead.

```yaml
spec:
  portal:
    sessions:
      maximum: 10
    workshop:
      defaults:
        capacity: 6
        reserved: 2
        initial: 4
  workshops:
  - name: lab-asciidoc-sample
  - name: lab-markdown-sample
```

Note that these could previously be set directly within the `portal` section but that usage is now deprecated.

Additional labels for workshops
-------------------------------

The defaults section can also be used to apply additional labels to all workshops.

```yaml
spec:
  portal:
    workshop:
      defaults:
        labels:
        - name: difficulty
          value: easy
  workshops:
  - name: lab-asciidoc-sample
  - name: lab-markdown-sample
```

If the workshop definition provides a label with the same name as one listed in the defaults section, that in the workshop definition will take precedence where the label is returned for a workshop via the training portal REST API, or where used internally to the training portal when matching workshops.

Additional labels for a workshop can also be defined in the entry for that workshop.

```yaml
spec:
  portal:
    workshop:
      defaults:
        labels:
        - name: difficulty
          value: easy
  workshops:
  - name: lab-asciidoc-sample
    labels:
    - name: difficulty
      value: hard
  - name: lab-markdown-sample
```

When a name conflict occurs, the value of this label will override any value specified in the workshop default section, or the workshop definition.

Labels for the training portal
------------------------------

As well as being able to provide additional labels for individual workshops, it is also possible to add labels to the training portal. These labels are distinct from labels added to metadata for Kubernetes resources and are specific to Educates. They can be used by a lookup service to identify the purpose of training portals.

```yaml
spec:
  portal:
    labels:
    - name: environment
      value: staging
```

Setting caps on individual users
--------------------------------

By default a single user can run more than one workshop at a time. You can though cap this if you want to ensure that they can only run one at a time. This avoids the problem of a user wasting resources by starting more than one at the same time, but only proceeding with one, without shutting down the other first.

The setting to apply a limit on how many concurrent workshop sessions a user can start is ``portal.sessions.registered``.

```yaml
spec:
  portal:
    sessions:
      maximum: 8
      registered: 1
  workshops:
  - name: lab-asciidoc-sample
    capacity: 4
    reserved: 2
  - name: lab-markdown-sample
    capacity: 6
    reserved: 4
```

This limit will also apply to anonymous users when anonymous access is enabled through the training portal web interface, or if sessions are being created via the REST API. If you want to set a distinct limit on anonymous users, you can set ``portal.sessions.anonymous`` instead.

```yaml
spec:
  portal:
    sessions:
      maximum: 8
      anonymous: 1
  workshops:
  - name: lab-asciidoc-sample
    capacity: 4
    reserved: 2
  - name: lab-markdown-sample
    capacity: 6
    reserved: 4
```

(expiring-of-workshop-sessions)=
Expiring of workshop sessions
-----------------------------

Once you reach the maximum capacity, no more workshops sessions can be created. Once a workshop session has been allocated to a user, they cannot be re-assigned to another user.

If running a supervised workshop you therefore need to ensure that you set the capacity higher than the expected number in case you have extra users you didn't expect which you need to accomodate. You can use the setting for the reserved number of instances so that although a higher capacity is set, workshop sessions are only created as required, rather than all being created up front.

For supervised workshops when the training is over you would delete the whole training environment and all workshop sessions would then be deleted.

If you need to host a training portal over an extended period and you don't know when users will want to do a workshop, you can setup workshop sessions to expire after a set time. When expired the workshop session will be deleted, and a new workshop session can be created in its place.

The maximum capacity is therefore the maximum at any one point in time, with the number being able to grow and shrink over time. In this way, over an extended time you could handle many more sessions that what the maximum capacity is set to. The maximum capacity is in this case used to ensure you don't try and allocate more workshop sessions than you have resources to handle at any one time.

Setting a maximum time allowed for a workshop session can be done using the ``expires`` setting.

```yaml
spec:
  workshops:
  - name: lab-markdown-sample
    capacity: 8
    reserved: 1
    expires: 60m
```

The value needs to be an integer, followed by a suffix of 's', 'm' or 'h', corresponding to seconds, minutes or hours.

The time period is calculated from when the workshop session is allocated to a user. When the time period is up, the workshop session will be automatically deleted.

When an expiration period is specified, when a user finishes a workshop, or restarts the workshop, it will also be deleted.

To deal with users who are allocated a workshop session, but then close the browser page or otherwise hide or minimise the browser page and thus do not interact with the workshop, you can also set a time period for when a workshop session with no activity is deemed as being orphaned and so deleted. This is done using the ``orphaned`` setting.

```yaml
spec:
  workshops:
  - name: lab-markdown-sample
    capacity: 8
    reserved: 1
    expires: 60m
    orphaned: 5m
```

This will be triggered in the first instance after the specified period for the case of the browser page having being closed, with the workshop session being automatically terminated. In other words, if specified as `5m`, then five minutes after the browser page is closed, the workshop session will be terminated. This automatic termination of a workshop session will also be applied in a second instance, where the browser page is hidden or minimized for a period 3 times the specified orphaned period. In other words, if specified as `5m`, a browser page which had been hidden and was not being interacted with, will be terminated after 15 minutes.

For supervised workshops where the whole event only lasts a certain amount of time, you should avoid this setting so that a users session is not deleted when they take breaks and their computer goes to sleep.

When the period of time specified by ``expires`` is reached the workshop session will be terminated and deleted. If you want to allow the duration of the workshop to be extended, you can in addition to ``expires`` set a maximum time deadline for the workshop. This will allow a workshop user to give themselves more time by clicking on the countdown timer of the workshop dashboard when the timer displays as orange. The time can also be extended from the admin pages of the training portal.

The maximum time deadline is specified by setting ``deadline``.

```yaml
spec:
  workshops:
  - name: lab-markdown-sample
    capacity: 8
    reserved: 1
    expires: 60m
    deadline: 120m
    orphaned: 5m
```

By default each time the workshop session is extended by the workshop user, 25% of the original duration of the workshop is added to the expiration time. If you want to override how much time can be added each time it is extended, you can set ``overtime``.

```yaml
spec:
  workshops:
  - name: lab-markdown-sample
    capacity: 8
    reserved: 1
    expires: 60m
    overtime: 30m
    deadline: 120m
    orphaned: 5m
```

When the expiration time has been extended up to the maximum time deadline it cannot be extended any further and the countdown timer will be displayed as red when it is within the last permitted overtime period.

The settings which affect duration and inactivity can also be set against ``portal.workshop.defaults`` instead, if you want to have them apply to all workshops. Note that you could also previously set ``expires`` and ``orphaned`` directly within the `portal` section but that usage is now deprecated.

(timeout-for-accessing-workshops)=
Timeout for accessing workshops
-------------------------------

When a workshop session is allocated to a workshop user, this could make use of a workshop session which has been pre-created and waiting in reserve, or it could be created on demand.

In either case, that a workshop session has been allocated to a workshop user doesn't necessarily mean that it is immediately in a usable state. The startup of a workshop session could be delayed in cases where it was depending on access to a Kubernetes secret or config map for configuration, or setup scripts run in the workshop container when started could take a period of time to execute.

In the case where initialization of the workshop session gets stuck or takes a lot longer than expected to run, the user web interface will constantly show "Waiting for deployment...". The workshop session can be explicitly deleted in this situation by clicking on the icon on the loading screen to stop the workshop session.

If you would rather have a workshop session automatically deleted when it exceeds some timeout on accessing the workshop dashboard, this can be done using the ``overdue`` setting in the training portal as a global default under ``portal.workshop.defaults``, or against a specific workshop.

```yaml
spec:
  workshops:
  - name: lab-markdown-sample
    expires: 60m
    orphaned: 5m
    overdue: 2m
```

When ``overdue`` is set and the time period specified expires without the workshop user being able to get to the workshop dashboard from their web browser, the workshop user will be automatically redirected to the URL which triggers deletion of the workshop session, followed by being redirected back to the list of workshops in the training portal, or any custom portal used as a front end.

Updates to workshop environments
--------------------------------

The list of workshops for an existing training portal can be changed by modifying the training portal definition applied to the Kubernetes cluster.

If a workshop is removed from the list of workshops, the workshop environment will be marked as stopping, and will be deleted when all active workshop sessions have completed.

If a workshop is added to the list of workshops, a new workshop environment for it will be created.

Changes to settings such as the maximum number of sessions for the training portal, or capacity settings for individual workshops will be applied to the existing workshop environments.

By default a workshop environment will be left unchanged if the corresponding workshop definition is changed. In the default configuration you would therefore need to explicitly delete the workshop from the list of workshops managed by the training portal, and then add it back again, if the workshop definition had changed.

If you would prefer for workshop environments to automatically be replaced when the workshop definition changes, you can enable it by setting the ``portal.updates.workshop`` setting.

```yaml
spec:
  portal:
    updates:
      workshop: true
```

When using this option you should use the ``portal.sessions.maximum`` setting to cap the number of workshop sessions that can be run for the training portal as a whole. This is because when replacing the workshop environment the old workshop environment will be retained so long as there is still an active workshop session being used. If the cap isn't set, then the new workshop environment will be still able to grow to its specific capacity and will not be limited based on how many workshop sessions are running against old instances of the workshop environment.

Overall it is recommended that the option to update workshop environments when workshop definitions change only be used in development environments where working on workshop content, at least until you are quite familiar with the mechanism for how the training portal replaces existing workshop environments, and the resource implications of when you have old and new instances of a workshop environment running at the same time.

(refreshing-workshop-environments)=
Refreshing workshop environments
--------------------------------

An instance of a workshop environment will persistent until the workshop is removed from the list of workshops against a training portal, or the workshop definition is updated and updates to workshop environments were enabled, in which case the existing workshop environment would be replaced with a new workshop environment instance.

In some cases you may want to periodically discard a workshop environment and replace it with a new one using the same workshop definition. This is useful where a common shared service is deployed with the workshop environment and used by each workshop session, and it accumulates resources over time which periodically need to be cleaned up and released.

To have a workshop environment periodically refreshed (deleted and replaced) you can provide the ``refresh`` property for a specific workshop, or you could also set it as a default for all workshops.

```yaml
spec:
  portal:
    sessions:
      maximum: 8
      anonymous: 1
    workshop:
      defaults:
        refresh: 168h
  workshops:
  - name: lab-asciidoc-sample
    capacity: 4
    reserved: 2
    refresh: 24h
  - name: lab-markdown-sample
    capacity: 6
    reserved: 4
```

When a workshop environment is being refreshed, the existing workshop environment will be marked as stopping, a new workshop environment created in its place with new workshop requests going to it, and with the old workshop environment only finally being deleted when all active workshop sessions running against it have completed.

(overiding-the-portal-hostname)=
Overriding the portal hostname
------------------------------

The default hostname given to the training portal will be the name of the resource with ``-ui`` suffix, followed by the domain specified by the resource, or the default inherited from the configuration of the Educates operator.

If you want to override the generated hostname, you can set ``portal.ingress.hostname``.

```yaml
spec:
  portal:
    ingress:
      hostname: labs
```

This will result in the hostname being ``labs.training.educates.dev``, rather than the default generated name such as ``lab-markdown-sample-ui.training.educates.dev``.

You can set the value of the ``hostname`` property to be a fully qualified domain name (FQDN), but it must share a common parent domain with the cluster ingress domain Educates is configured to use else workshop sessions will not work due to cross site cookie restrictions.

When using secure cluster ingress and a wildcard TLS certificate was supplied, any FQDN supplied for ``hostname`` must still match the wildcard TLS certificate. Alternatively, you need to supply a separate TLS certificate for the hostname.

```yaml
spec:
  portal:
    ingress:
      hostname: labs.educates.dev
      tlsCertificateRef:
        name: labs-educates-dev-tls
        namespace: default
```

Setting extra environment variables
-----------------------------------

If you want to override any environment variables for workshop instances created for a specific workshop, you can provide the environment variables in the ``env`` field of that workshop.

```yaml
spec:
  workshops:
  - name: lab-coding-sample
    env:
    - name: PROGRAMMING_LANGUAGE
      value: Java
```

Values of fields in the list of resource objects can reference a number of pre-defined parameters. The available parameters are:

* ``session_id`` - A unique ID for the workshop instance within the workshop environment.
* ``session_namespace`` - The namespace created for and bound to the workshop instance. This is the namespace unique to the session and where a workshop can create their own resources.
* ``environment_name`` - The name of the workshop environment. For now this is the same as the name of the namespace for the workshop environment. Don't rely on them being the same, and use the most appropriate to cope with any future change.
* ``workshop_namespace`` - The namespace for the workshop environment. This is the namespace where all deployments of the workshop instances are created, and where the service account that the workshop instance runs as exists.
* ``service_account`` - The name of the service account the workshop instance runs as, and which has access to the namespace created for that workshop instance.
* ``ingress_domain`` - The host domain under which hostnames can be created when creating ingress routes.
* ``ingress_protocol`` - The protocol (http/https) that is used for ingress routes which are created for workshops.
* ``services_password`` - A unique random password value for use with arbitrary services deployed with a workshop.
* ``ssh_private_key`` - The private part of a unique SSH key pair generated for the workshop session.
* ``ssh_public_key`` - The public part of a unique SSH key pair generated for the workshop session.
* ``ssh_keys_secret`` - The name of the Kubernetes secret in the workshop namespace holding the SSH key pair for the workshop session.
* ``platform_arch`` - The CPU architecture the workshop container is running on, ``amd64`` or ``arm64``.

The syntax for referencing one of the parameters is ``$(parameter_name)``.

If needed, you can use the ``alias`` property to create multiple different workshop environments using the same ``Workshop`` definition, but with different ``env`` values to customize the workshop.

```yaml
spec:
  workshops:
  - name: lab-coding-sample
    alias: lab-java-sample
    env:
    - name: PROGRAMMING_LANGUAGE
      value: Java
  - name: lab-coding-sample
    alias: lab-python-sample
    env:
    - name: PROGRAMMING_LANGUAGE
      value: Python
```

The ``alias`` property needs to be set to different values for each workshop in this case as the ``name`` property otherwise would need to be unique for each across all workshop necessitating two separate copies of the ``Workshop`` definition.

Overriding portal credentials
-----------------------------

When a training portal is deployed, the username for the admin and robot accounts will use the defaults of ``educates`` and ``robot@educates``. The passwords for each account will be randomly set.

For the robot account, the OAuth application client details used with the REST API will also be randomly generated.

You can see what the credentials and client details are by running ``kubectl describe`` against the training portal resource. This will yield output which includes:

```text
Status:
  educates:
    Clients:
      Robot:
        Id:      ACZpcaLIT3qr725YWmXu8et9REl4HBg1
        Secret:  t5IfXbGZQThAKR43apoc9usOFVDv2BLE
    Credentials:
      Admin:
        Password:  0kGmMlYw46BZT2vCntyrRuFf1gQq5ohi
        Username:  educates
      Robot:
        Password:  QrnY67ME9yGasNhq2OTbgWA4RzipUvo5
        Username:  robot@educates
```

If you wish to override any of these values in order to be able to set them to a pre-determined value, you can add ``credentials`` and ``clients`` sections to the training portal specification.

To overload the credentials for the admin and robot accounts use:

```yaml
spec:
  portal:
    credentials:
      admin:
        username: admin-user
        password: top-secret
      robot:
        username: robot-user
        password: top-secret
```

To override the application client details for OAuth access by the robot account use:

```yaml
spec:
  portal:
    clients:
      robot:
        id: application-id
        secret: top-secret
```

Controlling registration type
-----------------------------

By default the training portal web interface will present a registration page for users to create an account, before they can select a workshop to do. If you only want to allow the administrator to login, you can disable the registration page. This would be done if using the REST API to create and allocate workshop sessions from a separate application.

```yaml
spec:
  portal:
    registration:
      type: one-step
      enabled: false
```

If rather than requiring users to register, you want to allow anonymous access, you can switch the registration type to anonymous.

```yaml
spec:
  portal:
    registration:
      type: anonymous
```

In anonymous mode, when users visit the home page for the training portal an account will be automatically created and they will be logged in.

Specifying an event access code
-------------------------------

Where deploying the training portal with anonymous access, or open registration, anyone would be able to access workshops who knows the URL. If you want to at least prevent access to those who know a common event access code or password, you can set ``portal.password``.

```yaml
spec:
  portal:
    password: workshops-2020-07-01
```

When the training portal URL is accessed, users will be asked to enter the event access code before they are redirected to the list of workshops (when anonymous access is enabled), or to the login page.

Making list of workshops public
-------------------------------

By default the index page providing the catalog of available workshop images is only available once a user has logged in, be that through a registered account or as an anonymous user.

If you want to make the catalog of available workshops public, so they can be viewed before logging in, you can set the ``portal.catalog.visibility`` property.

```yaml
spec:
  portal:
    catalog:
      visibility: public
```

By default the catalog has visibility set to ``private``. Use ``public`` to expose it.

Note that this will also make it possible to access the list of available workshops from the catalog, via the REST API, without authenticating against the REST API.

Using an external list of workshops
-----------------------------------

If you are using the training portal with registration disabled and are using the REST API from a separate web site to control creation of sessions, you can specify an alternate URL for providing the list of workshops.

This helps in the situation where for a session created by the REST API, cookies were deleted, or a session URL was shared with a different user, meaning the value for the ``index_url`` supplied with the REST API request is lost.

The property to set the URL for the external site is ``portal.index``.

```yaml
spec:
  portal:
    index: https://www.example.com/
    registration:
      type: one-step
      enabled: false
```

If the property is supplied, passing the ``index_url`` when creating a workshop session using the REST API is optional, and the value of this property will be used. You may still want to supply ``index_url`` when using the REST API however if you want a user to be redirected back to a sub category for workshops on the site providing the list of workshops. The URL provided here in the training portal definition would then act only as a fallback when the redirect URL becomes unavailable, and would direct back to the top level page for the external list of workshops.

Note that if the training portal or workshop dashboards are embedded within an iframe of a larger site, the URL supplied index URL must map to a URL handler which uses Javascript to force a redirection of the whole browser to a desired page by setting ``window.top.location``, ``window.parent.location``, or equivalent for an appropriate frame context. If this is not done only the contents of the iframe will be redirected, resulting in the target being embedded within the larger site, possibly resulting in undesirable nesting of site pages.

In all cases, if a user has logged into the training portal as the admin user, they will not be redirected to the external site and will still see the training portals own list of workshops.

Overriding portal title and logo
--------------------------------

The web interface for the training portal will display a generic Educates logo by default, along with a page title of "Workshops". If you want to override these, you can set ``portal.title`` and ``portal.logo``.

```yaml
spec:
  portal:
    title: Workshops
    logo: data:image/png;base64,....
```

The ``logo`` field should be a graphical image provided in embedded data URI format which displays the branding you desire. The image is displayed with a fixed height of "40px".

(selecting-the-user-interface-theme)=
Selecting the user interface theme
----------------------------------

The styling of the web interface for the training portal and workshop sessions created from them is determined by the global configuration used when Educates is deployed.

If in the global Educates configuration you provided a set of themes for styling the web interface, a specific theme can be selected by setting ``portal.theme.name``.

```yaml
spec:
  portal:
    theme:
      name: labs-educates-dev-theme
```

For more information on adding themes as part of the Educates global configuration see [Overriding the styling of the workshop](overriding-styling-of-the-workshop).

(allowing-the-portal-in-an-iframe)=
Allowing the portal in an iframe
--------------------------------

By default if you try and display the web interface for the training portal in an iframe of another web site, it will be prohibited due to content security policies applying to the training portal web site.

If you want to enable the ability to iframe the full training portal web interface, or even a specific workshop session created using the REST API, you need to provide the hostname of the site which will embed it. This can be done using the ``portal.theme.frame.ancestors`` property.

```yaml
spec:
  portal:
    theme:
      frame:
        ancestors:
        - https://www.example.com
```

The property is a list of hosts, not a single value. If needing to use a URL for the training portal in an iframe of a page, which is in turn embedded in another iframe of a page on a different site again, the hostnames of all sites need to be listed.

In order to work with all popular web browsers it may also be necessary to override the cookie domain used by the training portal and workshop sessions. If this is necessary than all the web sites need to share a common parent domain. The cookie domain can then be set to the common parent domain.

```yaml
spec:
  portal:
    cookies:
      domain: example.com
```

Note that the sites which embed the iframes must be secure and use HTTPS, they cannot use plain HTTP. This is because browser policies prohibit promoting of cookies to an insecure site when embedding using an iframe. If cookies aren't able to be stored, a user would not be able to authenticate against the workshop session.

(collecting-analytics-on-workshops)=
Collecting analytics on workshops
---------------------------------

To collect analytics data on usage of workshops, you can supply a webhook URL. When this is supplied, events will be posted to the webhook URL for events such as workshop environments being created, workshop sessions being created and allocated to users, pages of a workshop being viewed, expiration of a workshop session, completion of a workshop session, termination of a workshop session, termination of a workshop environment and clicking on designated actions.

```yaml
spec:
  analytics:
    webhook:
      url: https://metrics.educates.dev/?client=name&token=password
```

At present there is no metrics collection service compatible with the portal webhook reporting mechanism, so you will need to create a custom service or integrate it with any existing web front end for the portal REST API service.

If the collection service needs to be provided with a client ID or access token, that must be able to be accepted using query string parameters which would be set in the webhook URL.

The details of the event are subsequently included as HTTP POST data using the ``application/json`` content type.

```
{
  "portal": {
    "name": "lab-markdown-sample",
    "uid": "91dfa283-fb60-403b-8e50-fb30943ae87d",
    "generation": 3,
    "url": "https://lab-markdown-sample-ui.training.educates.dev"
  },
  "event": {
    "name": "Session/Started",
    "timestamp": "2021-03-18T02:50:40.861392+00:00",
    "user": "c66db34e-3158-442b-91b7-25391042f037",
    "session": "lab-markdown-sample-w01-s001",
    "environment": "lab-markdown-sample-w01",
    "workshop": "lab-markdown-sample",
    "data": {}
  }
}
```

Where an event has associated data, it is included in the ``data`` dictionary.

```
{
  "portal": {
    "name": "lab-markdown-sample",
    "uid": "91dfa283-fb60-403b-8e50-fb30943ae87d",
    "generation": 3,
    "url": "https://lab-markdown-sample-ui.training.educates.dev"
  },
  "event": {
    "name": "Workshop/View",
    "timestamp": "2021-03-18T02:50:44.590918+00:00",
    "user": "c66db34e-3158-442b-91b7-25391042f037",
    "session": "lab-markdown-sample-w01-s001",
    "environment": "lab-markdown-sample-w01",
    "workshop": "lab-markdown-sample",
    "data": {
      "current_page": "workshop-overview",
      "next_page": "setup-environment",
      "page_number": 1,
      "pages_total": 4
    }
  }
}
```

In the case of clickable action which has been designated to generate an event, the data supplied is similar to that for a page view but has an additional field with the value of the `event` field added against the clickable action.

```
{
  "portal": {
    "name": "lab-markdown-sample",
    "uid": "91dfa283-fb60-403b-8e50-fb30943ae87d",
    "generation": 3,
    "url": "https://lab-markdown-sample-ui.training.educates.dev"
  },
  "event": {
    "name": "Action/Event",
    "timestamp": "2021-03-18T02:51:44.590918+00:00",
    "user": "c66db34e-3158-442b-91b7-25391042f037",
    "session": "lab-markdown-sample-w01-s001",
    "environment": "lab-markdown-sample-w01",
    "workshop": "lab-markdown-sample",
    "data": {
      "current_page": "workshop-overview",
      "next_page": "setup-environment",
      "page_number": 1,
      "pages_total": 4,
      "event_name": "open-example-web-site"
    }
  }
}
```

The ``user`` field will be the same portal user identity that is returned by the REST API when creating workshop sessions. In the case of a workshop session being created, the ``user`` field can be null where the workshop session is being created in reserve as opposed to on demand for a specific user.

Note that the event stream only produces events for things as they happen. If you need a snapshot of all current workshop sessions, you should use the REST API to request the catalog of available workshop environments, enabling the inclusion of current workshop sessions.

Tracking using Google Analytics
-------------------------------

If you want to record analytics data on usage of workshops using Google Analytics, you can enable tracking by supplying a tracking ID for Google Analytics.

```yaml
spec:
  analytics:
    google:
      trackingId: "G-XXXXXXXXXX"
```

You should use Google Analytics 4. The older Universal Analytics is being retired by Google in July 2023 and is no longer supported.

Custom dimensions are used in Universal Analytics to record details about the workshop a user is doing, and through which training portal and cluster it was accessed. You can therefore use the same Google Analytics tracking ID with Educates running on multiple clusters.

To support use of custom dimensions in Google Analytics you must configure the Universal Analytics property with the following custom dimensions. They must be added in the order shown as Universal Analytics doesn't allow you to specify the index position for a custom dimension and will allocate them for you. You can't already have custom dimensions defined for the property, as the new custom dimensions must start at index of 1.

```text
| Custom Dimension Name | Index |
|-----------------------|-------|
| workshop_name         | 1     |
| session_name          | 2     |
| environment_name      | 3     |
| training_portal       | 4     |
| ingress_domain        | 5     |
| ingress_protocol      | 6     |
```

Configuring the dimensions is no longer required in Google Analytics 4.

In addition to custom dimensions against page accesses, events are also generated. These include:

* Workshop/Start
* Workshop/Finish
* Workshop/Expired

If a Google Analytics tracking ID is provided with the ``TrainingPortal`` resource definition, it will take precedence over one set globally in configuration used when Educates was deployed.

Note that Google Analytics is not a reliable way to collect data. This is because individuals or corporate firewalls can block the reporting of Google Analytics data. For more precise statistics, you should use the webhook URL for collecting analytics with a custom data collection platform.

Tracking using Microsoft Clarity
--------------------------------

If you want to record analytics data on usage of workshops using Microsoft Clarity, you can enable tracking by supplying a tracking ID for Microsoft Clarity.

```yaml
spec:
  analytics:
    clarity:
      trackingId: "XXXXXXXXXX"
```

As Microsoft Clarity doesn't support custom user events, events generated by Educates are not able to be sent to it.

If a Microsoft Clarity tracking ID is provided with the ``TrainingPortal`` resource definition, it will take precedence over one set globally in configuration used when Educates was deployed.

Tracking using Amplitude
------------------------

If you want to record analytics data on usage of workshops using Amplitude, you can enable tracking by supplying a tracking ID for Amplitude.

```yaml
spec:
  analytics:
    amplitude:
      trackingId: "XXXXXXXXXX"
```

If a Amplitude tracking ID is provided with the ``TrainingPortal`` resource definition, it will take precedence over one set globally in configuration used when Educates was deployed.

