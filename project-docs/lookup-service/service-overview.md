(lookup-service-service-overview)=
Service Overview
================

The Educates lookup service provides a centralized REST API for managing and accessing workshop sessions across multiple Educates training portals and Kubernetes clusters. Rather than interacting with individual training portals directly, a custom front-end portal can use the lookup service as a single entry point for discovering available workshops and requesting workshop sessions.

When to use the lookup service
------------------------------

The Educates training portal provides its own web interface for end users to browse and launch workshops. It also exposes a REST API which a custom front-end portal can use to integrate workshop access into an existing web site. For details of the training portal REST API, see the [Portal REST API](../portal-rest-api/client-authentication) documentation.

While the training portal REST API works well when you have a single training portal, in a production environment you may need to deploy multiple training portals across one or more Kubernetes clusters. This could be to handle a larger number of concurrent workshop sessions than a single cluster can support, or to provide different sets of workshops to different groups of users.

In this scenario, rather than having a custom front-end portal interact with each training portal individually, the lookup service provides a single aggregation point. It monitors the state of training portals across all configured clusters and exposes a unified REST API that handles the details of locating available capacity and routing workshop session requests to the appropriate training portal.

Key concepts
------------

The lookup service introduces several concepts for organizing how workshops are accessed across a multi-cluster environment.

**Clusters** represent the Kubernetes clusters being monitored by the lookup service. Each cluster runs one or more Educates training portals. A cluster can be the same cluster the lookup service is deployed on (the local cluster), or a remote cluster accessed via a kubeconfig. Clusters can be assigned labels for categorization, such as identifying the customer or environment type (production, staging) they serve.

**Tenants** provide a logical partitioning of the available workshop resources. A tenant configuration defines rules that determine which clusters and training portals are accessible through that tenant. Rules can match clusters and portals by name or by labels. For example, you might create separate tenants for production and staging environments, or for different customers, each mapping to a different set of clusters and portals.

**Clients** are the user accounts that authenticate against the lookup service REST API. Each client is assigned one or more roles and is granted access to specific tenants. Two roles are supported. The ``admin`` role provides access to all administrative endpoints for querying the state of clusters, portals, and sessions. The ``tenant`` role provides access to the workshop listing and session request endpoints, restricted to the tenants the client has been granted access to.

**Training portals** are the standard Educates training portals deployed on monitored clusters. The lookup service watches for training portals across all configured clusters and tracks their available workshops, capacity, and active sessions. When a workshop session is requested through the lookup service, it selects the most appropriate training portal based on available capacity.

Enabling the lookup service
---------------------------

The lookup service is an optional component of Educates. To enable it, include the ``lookupService`` configuration when deploying Educates:

```yaml
lookupService:
  enabled: true
```

Once deployed, the lookup service will be accessible via an ingress at a URL of the form ``http://educates-api.<ingress-domain>``. Before it can be used, you will need to configure at least one monitored cluster, one tenant, and one client. These are configured by creating custom resources in the ``educates-config`` namespace of the cluster where the lookup service is running.
