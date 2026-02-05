(configuration-settings)=
Configuration Settings
======================

At the time of installing Educates, various configuration settings can be supplied. Some of these are essential to ensuring Educates will work correctly while others are optional. In a few cases the settings can be overridden on a case by case basis when deploying a training portal, but key settings must be provided when installing Educates.

(defining-configuration-for-ingress)=
Defining configuration for ingress
----------------------------------

If a custom ingress domain is not supplied when Educates is installed then ``educates-local-dev.xyz`` will be used as the default ingress domain. This value will only be useful if you can override local DNS resolution to map the domain name to the host where the ingress router for the Kubernetes cluster runs. That same DNS resolver would also need to be what is used by the Kubernetes cluster. As such, this would usually always need to be overridden with your own custom domain which you control.

Overrides for ingress domain, secret, protocol and class can be set in the values file used to deploy Educates.

To override just the ingress domain use the configuration setting:

```yaml
clusterIngress:
  domain: "workshops.example.com"
```

If you do not have your own custom domain name, it is possible to use a ``nip.io`` address mapped to the IP address of the inbound ingress router host, however, because it will not be possible to obtain a TLS certificate for the domain, you will not be able to use secure ingress.

Where you are using your own custom ingress domain and want to use secure ingress, you need to have a wildcard TLS certificate for the domain. There are two ways the TLS certificate can be supplied when Educates is being installed.

In the first method, you need to create a Kubernetes secret yourself which contains the TLS certificate. This can be placed in the ``default`` namespace, or any other namespace you desire.

If you had used ``certbot`` to generate the certificate from LetsEncrypt using a DNS challenge, you should be able to create the secret resource file using a command similar to:

```bash
kubectl create secret tls workshops.example.com-tls --cert=$HOME/.letsencrypt/config/live/workshops.example.com/fullchain.pem --key=$HOME/.letsencrypt/config/live/workshops.example.com/privkey.pem --dry-run=client -o yaml > workshops.example.com-tls.yaml
```

Replace ``workshops.example.com`` with the name of your custom domain name.

Load the secret into the Kubernetes ``default`` namespace using:

```bash
kubectl apply -n default -f workshops.example.com-tls.yaml
```

The configuration for Educates would then be written as:

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificateRef:
    namespace: "default"
    name: "workshops.example.com-tls"
```

The ``namespace`` setting should be the name of the namespace in which you created the secret containing the TLS certificate.

Rather than use a separate secret for holding the TLS secret, it can be added inline with the configuration settings using:

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificate:
    tls.crt: |
      ...
    tls.key: |
      ...
```

Use of a separate secret is the recommended method.

If HTTPS connections are being terminated using an external load balancer and not by specifying a secret for ingresses managed by the Kubernetes ingress controller, with traffic then routed into the Kubernetes cluster as HTTP connections, you can override the ingress protocol without specifying an ingress secret.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  protocol: "https"
```

In this case there is no need to provide the TLS certificate in the Educates configuration, but the external load balancer will need to be setup to use it.

By default, whatever is the default ingress controller in the Kubernetes cluster will be used. If you need to override this to use an alternate ingress controller, the ingress class can be specified.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  class: "nginx"
```

Do be aware that in overriding the ingress class, this only applies to Educates' own use of ingresses. If any workshop you deploy has users create ingresses, those workshops would need to be customized to use the alternate ingress class.

When supplying a TLS certificate for Educates to use, if it was signed using a certificate authority (CA) certificate which is not a globally trusted certificate, and so would not be trusted by HTTP clients, you can supply your CA certificate for internal use by Educates.

The preferred method for doing this is to create a Kubernetes secret in your cluster containing the certificate under the key ``ca.crt``. This secret can then be referenced by name.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificateRef:
    namespace: "default"
    name: "workshops.example.com-tls"
  caCertificateRef:
    namespace: "default"
    name: "workshops.example.com-ca"
```

Alternatively, the certificate can be provided inline to the configuration.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificate:
    tls.crt: |
      ...
    tls.key: |
      ...
  caCertificate:
    ca.crt: |
      ...
```

For Educates workshops which use per session image registries and where images from those image registries need to be deployed to the Kubernetes cluster, the CA certificate must also be registered with nodes in the Kubernetes cluster and used by the container runtime for the cluster when validating secure connections.

When using the ``educates`` CLI to create a local Kubernetes cluster using Kind, the CA certificate will be automatically injected into the nodes of the Kind cluster. When working with your own Kubernetes cluster, if you want injection of the CA certificates into the nodes of the cluster to be attempted then set the ``clusterIngress.caNodeInjector.enabled`` property.

```yaml
clusterIngress:
  domain: "workshops.example.com"
  tlsCertificateRef:
    namespace: "default"
    name: "workshops.example.com-tls"
  caCertificateRef:
    namespace: "default"
    name: "workshops.example.com-ca"
  caNodeInjector:
    enabled: true
```

Note that the Kubernetes cluster in this case must use a Debian based operating system for nodes and ``containerd`` as the container runtime. Other operating systems or container runtimes are not supported when using this mechanism to inject the CA certificate into the cluster nodes.

Defining cluster policy engine
------------------------------

Due to the nature of how Kubernetes works, by default there will be no restrictions on workshop users being able to make use of privileged features of Kubernetes. This is because the Kubernetes security model assumes that only trusted users will have access to a cluster. If deploying Educates where there is no cluster security policy enforcement being performed, you should never allow access to workshops by untrusted users.

To facilitate untrusted users being able to do workshops hosted using Educates, it is necessary to use one of the builtin features of Kubernetes for security policy enforcement, or use a third party solution.

Different mechanisms have been provided over time with standard Kubernetes distributions and derivatives such as OpenShift. These are:

* Pod security policies (Kubernetes <= 1.25).
* Pod security standards (Kubernetes >= 1.22).
* Security context constraints (OpenShift)

For pod security policies and pod security standards, these both need to be enabled in the Kubernetes cluster at the time the cluster is created, it is not something that can be enabled afterwards. For some Kubernetes distributions it is not possible to enable pod security policies, and pod security standards being new, may also not be supported.

Although pod security standards are the proposed future solution to this problem, the standard security policies it provides (specifically the ``restricted`` policy) are also not a great match for Educates, yet unlike the prior pod security policies feature there is no way to customize pod security standards.

As such, for standard Kubernetes clusters it is recommended that neither pod security policies or pod security standards be used. The recommended cluster security policy enforcement engine when using Educates is instead the third party solution [Kyverno](https://kyverno.io/). You will though need to have Kyverno installed. You do not need to configure Kyverno as Educates will provide the security policies for it when enforcing cluster level security requirements.

Presuming that you will use Kyverno for cluster security policy enforcement, the configuration settings would be:

```yaml
clusterSecurity:
  policyEngine: "kyverno"
```

Note though that Kyverno cannot be used for this purpose if pod security policies are enabled in the Kubernetes cluster and a default role binding has been defined for the cluster as a whole mapping authenticated users to a security policy. In this case you must use ``pod-security-policies`` instead.

```yaml
clusterSecurity:
  policyEngine: "pod-security-policies"
```

In the case of OpenShift, it's security context constraints enforcement engine is always enabled and as such you must instead use ``security-context-constraints`` instead.

```yaml
clusterSecurity:
  policyEngine: "security-context-constraints"
```

If using a recent Kubernetes version, have pod security standards enabled in the cluster configuration and want to experiment with it, you can use ``pod-security-standards`` instead.

```yaml
clusterSecurity:
  policyEngine: "pod-security-standards"
```

Use of pod security standards is not recommended and Kyverno should be used instead. If you do use pod security standards and a workshop sets the security policy to ``restricted`` extra work may be required to customize the workshop such that it works.

If the policy engine is not specified at all, it will default to ``none``, which as already mentioned means there are no restrictions and untrusted users should never be allowed access to workshops hosted using Educates.

Defining workshop policy engine
-------------------------------

In addition to cluster level security policy enforcement which affects workloads and what they can do, Kyverno is separately used for more fine grained policy enforcement in regard to how any Kubernetes resource is used by specific workshops. Kyverno needs to be installed to support workshop security policy enforcement.

```yaml
workshopSecurity:
  policyEngine: "kyverno"
```

This can be set to ``none``, and this is okay for testing on your own local system, but should never be done where untrusted users would be doing workshops.

(overriding-container-runtime-class)=
Overriding container runtime class
----------------------------------

Containers of the workshop session pod are run using the default runtime provider configured for the Kubernetes cluster. If you want to override the runtime class for the workshop pod to which a workshop user has shell access, it can be done as a global configuration setting. Where the Kubernetes cluster has been set up with necessary support, this can be used for example to have containers for the workshop pod run in Kata containers, adding an additional level of security.

```yaml
clusterRuntime:
  class: kata-qemu
```

Note that other components, such as the Educates operator and training portal, as well as any additional deployments created for a workshop session or workshop environment, are still run using the default container runtime class. It is only the containers of the workshop pod created for each workshop session and to which workshops users have shell access that are run with this runtime class.

(restricting-session-manager-permissions)=
Restricting session manager permissions
---------------------------------------

By default, the session manager component in Educates, which is responsible for managing workshop sessions, is granted cluster admin access to the Kubernetes cluster. This default configuration provides convenience for workshop authors, as they do not need to implement any special mechanisms to elevate privileges when their workshops require access to custom resources managed by Kubernetes operators or other cluster-wide resources.

If a cluster administrator is concerned about the session manager having cluster admin permissions, this elevated access can be disabled. When disabled, the session manager will operate with only the minimum permissions necessary to deploy training portals and workshops, manage workshop sessions, and grant users the access required to deploy workloads into the Kubernetes namespace allocated to their workshop session.

When cluster admin permissions are dropped from the session manager, workshops that require additional access permissions beyond the defaults will need those permissions explicitly defined. This is achieved by creating a ``ClusterRole`` that specifies the additional permissions required by the workshop. To have the session manager adopt these extra permissions, the ``ClusterRole`` can leverage Kubernetes cluster role aggregation by applying the appropriate label:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-workshop-permissions
  labels:
    rbac.educates.dev/extends-workshop-permissions: "true"
rules:
- apiGroups:
  - "kappctrl.k14s.io"
  resources:
  - apps
  verbs:
  - "*"
```

Note that dropping cluster admin permissions from the session manager does not eliminate the requirement to install Educates itself with cluster admin privileges. The initial installation still requires elevated access to set up the necessary custom resource definitions, namespaces, and other cluster-level resources.

To disable cluster admin permissions for the session manager, set the following in your configuration when installing Educates:

```yaml
sessionManager:
  clusterAdmin: false
```

Defining image registry pull secrets
------------------------------------

If needing to work with custom workshop images stored in a private image registry, you can define a list of image pull secrets that should be added to the service accounts used to deploy and run the workshop images.

```yaml
clusterSecrets:
  pullSecretRefs:
    - namespace: "default"
      name: "registry.example.com-pull"
```

The secret resources must be of type ``kubernetes.io/dockerconfigjson`` and reside in the defined namespace. The secrets will be copied into the required namespaces by Educates.

Note that this doesn't result in any secrets being added to the namespace created for each workshop session. The secrets are only added to the workshop namespace and are not visible to a user.

Defining storage class for volumes
----------------------------------

Deployments of the training portal web interface and the workshop sessions make use of persistent volumes. By default the persistent volume claims will not specify a storage class for the volume and instead rely on the Kubernetes cluster specifying a default storage class that works. If the Kubernetes cluster doesn't define a suitable default storage class, or you need to override it, you can override the storage class.

```yaml
clusterStorage:
  class: "default"
```

Note that this only applies to persistent volume claims setup by the Educates operator. If the steps in a workshop which a user executes include making persistent volume claims, these will not be automatically adjusted.

Defining storage group for volumes
----------------------------------

Where persistent volumes are used by Educates for the training portal web interface and workshop environments, the application of pod security policies by the cluster is relied on to ensure that the permissions of persistent volumes are set correctly such that they can be accessed by containers mounting the persistent volume. For where the pod security policy admission controller is not enabled, a fallback is instituted to enable access to volumes by enabling group access using the group ID of ``1``.

In situations where the only class of persistent storage available is NFS or similar, it may be necessary to override the group ID applied and set it to an alternate ID dictated by the file system storage provider.

```yaml
clusterStorage:
  group: 1
```

Overriding the group ID to match the persistent storage relies on the group having write permission to the volume. If only the owner of the volume has permission this will not work.

In this case it is necessary to change the owner/group and permissions of the persistent volume such that the owner matches the user ID a container runs as, or the group is set to a known ID which is added as a supplemental group for the container, and the persistent volume updated to be writable to this group. This needs to be done by an init container running in the pod mounting the persistent volume.

To trigger this fixup of ownership and permissions, you can set the user as well as group for storage.

```yaml
clusterStorage:
  user: 1
  group: 1
```

This will result in the init container being run as the root user, with the owner of the mount directory of the persistent volume being set to specified user, the group being set to specified group, and the directory being made group writable. The group will then be added as supplemental group to containers using the persistent volume so they can write to it, regardless of what user ID the container runs as. To that end, the value of the user doesn't matter, as long as it is set, but it may need to be set to a specific user ID based on requirements of the storage provider.

Note that both these variations on the settings only apply to the persistent volumes used by Educates itself. If a workshop asks users to create persistent volumes, those instructions or the resource definitions used may need to be modified in order to work where the storage class available requires access as a specific user or group ID. Further, the second method using the init container to fixup permissions will not work if security policies are enforced, as the ability to run a container as the root user would be blocked in that case due to the policy restrictions applied to workshop instances.

(restricting-network-access)=
Restricting network access
--------------------------

Any processes run from the workshop container and any applications deployed to the session namespaces associated with a workshop instance can contact any network IP addresses accessible from the cluster. If necessary you can add restrictions on what IP addresses or IP subnets can be accessed. This must be a CIDR block range corresponding to the subnet or a portion of a subnet you want to block. A Kubernetes ``NetworkPolicy`` will be used to enforce the restriction so the Kubernetes cluster must use a network layer supporting network policies and the necessary Kubernetes controllers supporting network policies enabled when the cluster was installed.

If deploying to AWS, it is important to block access to the AWS endpoint for querying EC2 metadata as it can expose sensitive information that workshop users should not haves access to. Since AWS may be a common deployment target, blocking of the AWS endpoint is specified as the default.

```yaml
clusterNetwork:
  blockCIDRs:
  - "169.254.169.254/32"
  - "fd00:ec2::254/128"
```

Overriding network packet size
------------------------------

When support for building container images using ``docker`` is enabled for workshops, because of network layering that occurs when doing ``docker build`` or ``docker run``, it is necessary to adjust the network packet size (mtu) used for containers run from ``dockerd`` hosted inside of the workshop container.

The default mtu size for networks is 1500, but when containers are run in Kubernetes the size available to containers is often reduced. To deal with this possibility, the mtu size used when ``dockerd`` is run for a workshop is set as 1400 instead of 1500.

If you experience problems building or running images with the ``docker`` support, including errors or timeouts in pulling images, or when pulling software packages (PyPi, npm, etc) within a build, you may need to override this value to an even lower value.

```yaml
dockerDaemon:
  networkMTU: 1400
```

You can determine what the size may need to be by accessing the ``docker`` container run with a workshop and run ``ifconfig eth0``. This will yield something similar to:

```text
eth0      Link encap:Ethernet  HWaddr 02:42:AC:11:00:07
          inet addr:172.17.0.7  Bcast:172.17.255.255  Mask:255.255.0.0
          UP BROADCAST RUNNING MULTICAST  MTU:1350  Metric:1
          RX packets:270018 errors:0 dropped:0 overruns:0 frame:0
          TX packets:283882 errors:0 dropped:0 overruns:0 carrier:0
          collisions:0 txqueuelen:0
          RX bytes:86363656 (82.3 MiB)  TX bytes:65183730 (62.1 MiB)
```

If the ``MTU`` size is less than 1400, then use the value given, or a smaller value, for the ``dockerd.mtu`` setting.

(image-registry-pull-through-cache)=
Image registry pull through cache
---------------------------------

When running or building container images with ``docker``, if the container image is hosted on Docker Hub it will be pulled down direct from Docker Hub for each separate workshop session of that workshop.

Because the image is pulled from Docker Hub this will be slow for all users, especially for large images. With Docker Hub having introduced limits on how many images can be pulled anonymously from an IP address within a set period, this also could result in the cap on image pulls being reached, preventing the workshop from being used until the period expires.

Docker Hub has a higher limit when pulling images as an authenticated user, but with the limit being applied to the user rather than by IP address. For authenticated users with a paid plan on Docker Hub, there is a much greater limit.

To try and avoid the impact of the limit, the first thing you can do is enable an image registry mirror with image pull through. This is enabled globally and results in an instance of an image registry mirror being created in the workshop environment of workshops which enable ``docker`` support. This mirror will be used for all workshops sessions created against that workshop environment. When the first user attempts to pull an image, it will be pulled down from Docker Hub and cached in the mirror. Subsequent users will be served up from the image registry mirror, avoiding the need to pull the image from Docker Hub again. The subsequent users will also see a speed up in pulling the image because the mirror is deployed to the same cluster.

```yaml
dockerDaemon:
  proxyCache:
    remoteURL: "https://registry-1.docker.io"
```

For authenticated access to Docker Hub, create an access token under your Docker Hub account. Then set the ``username`` and ``password``, using the access token as the ``password``. Do not use the password for the account itself. Using an access token makes it easier to revoke the token if necessary.

```yaml
dockerDaemon:
  proxyCache:
    remoteURL: "https://registry-1.docker.io"
    username: "username"
    password: "access-token"
```

Note that an access token provides write access to Docker Hub. It is thus also recommended you use a separate robot account in Docker Hub which isn't going to be used to host images, and also doesn't have write access to any other organizations. In other words, use it purely for reading images from Docker Hub.

If this is a free account, the higher limit on image pulls will then apply. If the account is paid then higher limits again will apply.

Also note that the image registry mirror is only used when running or building images using the support for running ``docker``. The mirror does not come into play when creating deployments in Kubernetes which make use of images hosted on Docker Hub. Usage of images from Docker Hub in deployments will still be subject to the limit for anonymous access, unless you were to supply image registry credentials for the deployment so an authenticated user were used.

Setting default access credentials
----------------------------------

When deploying a training portal using the ``TrainingPortal`` custom resource, the credentials for accessing the portal will be unique for each instance. The details of the credentials can be found by viewing status information added to the custom resources using ``kubectl describe``.

If desired you can override the credentials for the portals so the same set of credentials are used for each.

```yaml
trainingPortal:
  credentials:
    admin:
      username: "educates"
      password: "admin-password"
    robot:
      username: "robot@educates"
      password: "robot-password"
```

The client ID and secret used for OAuth access by the robot account can also be overridden.

```yaml
trainingPortal:
  clients:
    robot:
      id: "robot-id"
      secret: "robot-secret"
```

If the ``TrainingPortal`` has specified credentials or client information, they will still take precedence over the values specified in the system profile.

Tracking using workshop events
------------------------------

To collect analytics data on usage of workshops, you can supply a webhook URL. When this is supplied, events will be posted to the webhook URL for events such as workshop environments being created, workshop sessions being created and allocated to users, pages of a workshop being viewed, expiration of a workshop session, completion of a workshop session, termination of a workshop session, termination of a workshop environment and clicking on designated actions.

```yaml
workshopAnalytics:
  webhook:
    url: "https://metrics.educates.dev/?client=name&token=password"
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
      "total_pages": 4
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
      "total_pages": 4,
      "event_name": "open-example-web-site"
    }
  }
}
```

The ``user`` field will be the same portal user identity that is returned by the REST API when creating workshop sessions. In the case of a workshop session being created, the ``user`` field can be null where the workshop session is being created in reserve as opposed to on demand for a specific user.

Note that the event stream only produces events for things as they happen. If you need a snapshot of all current workshop sessions, you should use the REST API to request the catalog of available workshop environments, enabling the inclusion of current workshop sessions.

Instead of enabling tracking of workshop globally, it can also be configured when creating a training portal. 

(tracking-using-google-analytics)=
Tracking using Google Analytics
-------------------------------

If you want to record analytics data on usage of workshops using Google Analytics, you can enable tracking by supplying a tracking ID for Google Analytics.

```yaml
workshopAnalytics:
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

Note that Google Analytics is not a reliable way to collect data. This is because individuals or corporate firewalls can block the reporting of Google Analytics data. For more precise statistics, you should use the webhook URL for collecting analytics with a custom data collection platform.

Instead of enabling Google analytics globally, it can also be configured when creating a training portal. 

(tracking-using-microsoft-clarity)=
Tracking using Microsoft Clarity
--------------------------------

If you want to record analytics data on usage of workshops using Microsoft Clarity, you can enable tracking by supplying a tracking ID for Microsoft Clarity.

```yaml
workshopAnalytics:
  clarity:
    trackingId: "XXXXXXXXXX"
```

As Microsoft Clarity doesn't support custom user events, events generated by Educates are not able to be sent to it.

Instead of enabling Microsoft Clarity analytics globally, it can also be configured when creating a training portal. 

(tracking-using-amplitude)=
Tracking using Amplitude
------------------------

If you want to record analytics data on usage of workshops using Amplitude, you can enable tracking by supplying a tracking ID for Amplitude.

```yaml
workshopAnalytics:
  amplitude:
    trackingId: "XXXXXXXXXX"
```

Instead of enabling Amplitude analytics globally, it can also be configured when creating a training portal. 

(overriding-styling-of-the-workshop)=
Overriding styling of the workshop
----------------------------------

If using the REST API to create/manage workshop sessions and the workshop dashboard is then embedded into an iframe of a separate site, it is possible to perform minor styling changes of the dashboard, workshop content and portal to match the separate site using CSS or Javascript.

```yaml
websiteStyling:
  workshopDashboard:
    html: |
      <!-- HTML to include in head of dashboard pages. -->
    script:  |
      console.log("Dashboard theme overrides.");
    style: |
      body {
        font-family: "Comic Sans MS", cursive, sans-serif;
      }
  workshopInstructions:
    html: |
      <!-- HTML to include in head of workshop instructions pages. -->
    script: |
      console.log("Workshop theme overrides.");
    style: |
      body {
        font-family: "Comic Sans MS", cursive, sans-serif;
      }
  trainingPortal:
    html: |
      <!-- HTML to include in head of training portal pages. -->
    script: |
      console.log("Portal theme overrides.");
    style: |
      body {
        font-family: "Comic Sans MS", cursive, sans-serif;
      }
```

It is also possible to customize the description displayed in the finished workshop dialog. This could be just a change to the description, or an embedded form could be included to allow entering into a raffle where Educates is being used to host workshops at a conference booth. Alternatively, you might generate a QR code that people could scan on their own device so as to enter a raffle or fill out some other type of survey away from the booth and thus free up the booth laptop for other users.

Because this customization is only offered for the specific dialog shown when a workshop user fully finishes the workshop, and not if they exit the session early, in order to warn them of the required path they must take to get their reward, custom content for a new dialog to be shown when the workshop is started can also be provided.

```yaml
websiteStyling:
  workshopStarted:
    html: ""
  workshopFinished:
    html: ""
```

The above settings for overriding the styling act as a global default across all training portals and workshop sessions created from them. If you need to be able to have different styling for different training portals, you can instead provide theme files via Kubernetes secrets.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: workshops.example.com-theme
  namespace: default
stringData:
  workshop-dashboard.html: ""
  workshop-dashboard.css: ""
  workshop-dashboard.js: ""
  workshop-instructions.html: ""
  workshop-instructions.js: ""
  workshop-instructions.css: ""
  workshop-started.html: ""
  workshop-finished.html: ""
  training-portal.html: ""
  training-portal.js: ""
  training-portal.css: ""
```

These secrets can then be referenced under ``websiteStyling.themeDataRefs`` as:

```yaml
websiteStyling:
  themeDataRefs:
  - name: workshops.example.com-theme
    namespace: default
```

To select one of the themes specified by a secret as a global default in place of the inline definition, you can set the ``defaultTheme`` property:

```yaml
websiteStyling:
  defaultTheme: workshops.example.com-theme
  themeDataRefs:
  - name: workshops.example.com-theme
    namespace: default
```

You can also override the name of the theme to be used in a training portal resource definition.

```yaml
spec:
  portal:
    theme:
      name: workshops.example.com-theme
```

Note that all data items in the secret for a theme will be made available to the training portal or workshop dashboard container. You can therefore include additional assets such as image files and reference them from your HTML, Javascript or CSS customizations.

(allowing-sites-to-embed-workshops)=
Allowing sites to embed workshops
---------------------------------

When modifying the theme for the training portal and workshop sessions, it is often because you are embedding access to them into a separate web site. In this case the training portal and workshop session will be embedded in a HTML iframe of the separate web site.

In this case where you are embedding into a separate web site you will need to configure Educates to allow it. This can be done by supplying the hostnames of the sites doing the embedding.

```yaml
websiteStyling:
  frameAncestors:
  - example.com
```

The frame ancestors can also be overridden on a per training portal definition in the training portal definition. This option may also have to be used in conjunction with options for specifying a custom cookie domain.

(overriding-session-cookie-domain)=
Overriding session cookie domain
--------------------------------

Browser cookies are used by the training portal and workshop sessions to track the identity of the workshop user. By default the cookie domain is set to the respective hostnames of the training portal or workshop session.

In cases where the training portal or workshop session dashboard is embedded within a separate web site, to avoid problems arising from restrictions on cross domain cookies when embedding using iframes with some web browsers, the cookie domain may need to be overridden.

For this to work the training portal, workshop sessions and the separate web site into which they are embedded must share a common domain. If this is satisified, the cookie domain can be overridden and set to the common parent domain.

```yaml
sessionCookies:
  domain: "example.com"
```

The cookie domain can also be overridden on a per training portal definition in the training portal definition. This option may also have to be used in conjunction with options for specifying allowed frame ancestors when embedding.
