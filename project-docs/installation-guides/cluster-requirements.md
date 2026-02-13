Cluster Requirements
====================

The [local Educates environment](quick-start-guide) is the recommended method for getting started with Educates, as well as providing an environment for developing new workshop content. When you are ready to host your workshops for others to use, you will want to setup a separate Kubernetes cluster to run Educates along with your workshops.

In the case of the local Educates environment the configuration of the local Kind cluster was done for you. In deploying Educates to a distinct Kubernetes cluster you may need to perform some setup of that cluster in order to satisify the requirements for deploying Educates.

The Educates package installer can help with installing some pre-requisites for you, or you can choose to install them yourself. The following outlines the general requirements. For more information before performing any setup, also see the installation instructions for Educates.

Dedicated Kubernetes cluster
----------------------------

As the goal is to host workshops which other users will then do, and those users may not always be trusted users, it is strongly recommended that a dedicated Kubernetes cluster always be setup just for running Educates and your workshops. Avoid deploying Educates into an existing Kubernetes cluster which you use for other purposes, be that for development or production.

This is because although Educates makes use of role based access controls (RBAC), a security policy engine, and you can specify quotas and limit ranges to apply to workshop users to restrict what can be done, there is always a risk that a workshop user could do something that has an impact on the Kubernetes cluster as a whole or other workloads deployed to the cluster.

Size of the Kubernetes cluster
------------------------------

There is no simple answer to this question as it will depend on the workshops you want to deploy, what their specific requirements are, and the number of concurrent users you want to support. Separate guidelines will be provided later to help answer this question, but if just starting out a good starting point is a Kubernetes cluster with three worker nodes where each worker node has 16-32GB of memory available.

Be careful using worker nodes with any more memory than 32GB, as some infrastructure providers have limitations on the number of persistent volumes that can be attached to each node in a cluster. Depending on how many persistent volumes may be needed for a workshop, you may end up in a situation where you exhaust the capacity for the number of persistent volumes that can be used on a worker node before you exhaust memory. Thus it is preferable to use worker nodes with less memory and spread the load across more nodes, rather than use very big worker nodes.

Kubernetes ingress controller
-----------------------------

The cluster must have an ingress router configured. Only a basic deployment of the ingress controller is usually required, but it must use the standard ports of 80/443. You do not need to configure the ingress controller to handle cluster wide edge termination of secure HTTP connections. Educates will create Kubernetes ingress resources and supply any secret for use with secure HTTP connections for each ingress.

For the ingress controller it is strongly recommended you use [Contour](https://projectcontour.io/) over alternatives such as nginx. An nginx based ingress controller has a less than optimal design whereby every time a new ingress is created or deleted, the nginx config is reloaded resulting in websocket connections being terminated after a period of time. Educates terminals are implemented to reconnect automatically in the case of the websocket connection being lost, but not all applications you may use with specific workshops may handle loss of websocket connections so gracefully and so they may be impacted due to the use of an nginx ingress controller. This problem is not specific to Educates and can impact any application when using an nginx ingress controller and ingresses are created/deleted frequently.

If using a hosted Kubernetes solution from an IaaS provider such as Google, AWS or Azure, you may need to ensure that any HTTP request timeout specified on the inbound load balancer for the ingress controller is increased such that long lived websocket connections can be used. Load balancers of hosted Kubernetes solutions in some cases only have a 30 second timeout. If possible configure the timeout which would apply to websockets to be 1 hour.

If delivering workshops to users where you expect many to be using the Safari web browser on macOS or iOS, be aware that Safari has a known issue with connection reuse/coalescing when using HTTP/2 to a router hosting common domains. Specifically, Safari doesn't respect rejected requests with HTTP status code of 421 and attempt a reconnection. This can result in Safari failing when connecting to Educates workshop sessions. To workaround this issue in Safari you will need to disable HTTP/2 in the ingress controller configuration.

Whatever the ingress controller is that is used, it should be configured as the default ingress provider. It is possible to configure Educates to use a specific named ingress class in order to select a different ingress provider, however this only applies to Educates own use of ingresses and any workshop you want to use which creates ingresses, would need to be customized to use the non standard ingress class.

Wildcard DNS record for hosts
-----------------------------

Educates requires wildcard DNS for the ingress domain because each workshop session is assigned its own primary hostname, so each user accesses their session through a separate URL. A session can also receive additional hostnames under the same root domain for web applications bundled with the workshop. This is important because many web applications expect to run on a dedicated hostname and do not function correctly when served from a sub-URL path of another site. In DNS, you typically provision a wildcard record (commonly a `CNAME` such as `*.workshops.example.com`) that points to the platform ingress/load balancer hostname so any session-specific hostname resolves automatically.

If you want secure HTTPS access for workshop sessions, you also need a matching wildcard TLS/SSL certificate for that same wildcard domain (for example, `*.workshops.example.com`). This is required because TLS validation happens before application routing, and the certificate presented by the ingress must match the exact hostname the user opens. Without a wildcard certificate (or equivalent per-host certificates), browsers will show certificate mismatch warnings and secure access will fail.

Kubernetes persistent volumes
-----------------------------

Educates uses persistent volumes and expects persistent volumes of type ``ReadWriteOnce (RWO)`` to be available through the default storage provider. If no default storage class is specified, or a specified storage class is required, Educates can be configured with the name of the storage class, however this only applies to Educates own use of storage and any workshop you want to use which wants to claim storage, would need to be customized to use the non standard storage class.

For some Kubernetes distributions, including hosted solutions available from IBM, the storage provided is based on NFS or similar technologies and it may be necessary to configure Educates to know about what user and group should be used for persistent volumes. This is required so that permissions on storage can be fixed up before use. As before, this only helps in the case of Educates own use of storage and workshops may need to be customized to work with these specific Kubernetes clusters.

Also be aware that the support in Educates to try and accomodate use of NFS storage solutions for persistent volumes, and the need to fixup permissions, is not regularly tested and as such there is a chance it may not work. If needing to deploy to a Kubernetes cluster with such a requirement for storage and you have issues, make sure you report any problem so it can be addressed.

(cluster-security-enforcement)=
Cluster security enforcement
----------------------------

RBAC within a Kubernetes cluster can only be used to control what sort of resources can be created, and what actions a user can make against resources in a cluster. There is still configuration that can be set for pods deployed with a workload which do not necessarily come under RBAC rules. This includes the ability to run containers as root, adjust kernel privileges (Linux capabilities) or run containers as privileged. By default Kubernetes clusters place no restrictions on a users ability to override these settings.

In order to enforce some sort of security policy around what a user can do, different mechanisms have been provided over time with standard Kubernetes distributions and derivatives such as OpenShift. These are:

* Pod security policies (Kubernetes <= 1.25).
* Pod security standards (Kubernetes >= 1.22).
* Security context constraints (OpenShift)

For pod security policies and pod security standards, these both need to be enabled in the Kubernetes cluster at the time the cluster is created, it is not something that can be enabled afterwards. For some Kubernetes distributions it is not possible to enable pod security policies, and pod security standards being new, may also not be supported.

Although pod security standards are the proposed future solution to this problem, the standard security policies it provides (specifically the ``restricted`` policy) are also not a great match for Educates, yet unlike the prior pod security policies feature there is no way to easily customize pod security standards.

As such, for standard Kubernetes clusters it is recommended that neither pod security policies or pod security standards be used. The recommended cluster security policy enforcement engine when using Educates is instead the third party solution [Kyverno](https://kyverno.io/). You therefore need to have Kyverno installed. You do not need to configure Kyverno as Educates will provide the security policies for it when enforcing cluster level security requirements.

If Kyverno is not installed or enabled, nor pod security policies or pod security standards enabled, there will be no restrictions on workshop users being able to make use of the features mentioned, which will be a security risk. If deploying Educates where there is no cluster security policy enforcement being performed, you should never allow access to workshops to untrusted users.

In the case of OpenShift, there is currently no option but to use OpenShift's own security context constraints feature for cluster security enforcement.

Even if using one of the supported security policy engines, if you want to try and apply an even greater level of security protection, Educates supports use of Kata Containers or similar systems which rely on setting the runtime class for pods in a Kubernetes cluster. See configuration settings for Educates for more information.

(workshop-security-enforcement)=
Workshop security enforcement
-----------------------------

Cluster provided features such as pod security policies, pod security standards and security context constraints, can only enforce a core set of constraints around what privileges specific workloads can use. A separate tool such as Kyverno can enforce similar restrictions, as well as additional restrictions which also can't be satisfied through other mechanisms such as resource quotas, limit ranges and RBAC.

In the context of Educates, where it is necessary to control access for different workshop users so they cannot interfere with other workshops users, or other workloads deployed to the cluster, additional restrictions are necessary and for this Kyverno is used. As such, even if not using Kyverno for cluster security policy enforcement in place of pod security policies, pod security standards and security context constraints, Kyverno is still required for workshop security policy enforcement.

If Kyverno is not installed or enabled, enforcement of any security policies to workshops cannot be done for any extra restrictions which are required. If deploying Educates where there is no workshop security policy enforcement being performed, you should never allow access to workshops to untrusted users.

Carvel package installation
---------------------------

The installation method for Educates relies on the [Carvel](https://carvel.dev/) packaging system. You have two options for installing Educates into an existing Kubernetes cluster.

The first option is to use the `educates` CLI to deploy Educates and any required services to the Kubernetes cluster. In this case, although Educates uses the Carvel packaging system, you do not need the Carvel tools installed on your local host computer, nor do you need to have the Carvel [kapp-controller](https://carvel.dev/kapp-controller/) operator pre-installed into the Kubernetes cluster.

The second option, and one which may be more suitable if setting up clusters to run Educates as part of a GitOps or CI/CD based installation process, is to have `kapp-controller` pre-installed into the Kubernetes cluster and use it to install Educates and any required services.

If using Tanzu Kubernetes Grid (TKG) or Tanzu Mission Control (TMC), `kapp-controller` will already exist upon the Kubernetes cluster being created, however, for other Kubernetes distributions you will need to install `kapp-controller` yourself if wanting to use it to install Educates.
