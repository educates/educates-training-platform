Admin REST API
==============

In addition to the workshop listing and session request endpoints available to clients with the ``tenant`` role, the lookup service provides a set of administrative endpoints for clients with the ``admin`` role. These endpoints allow querying the state of clusters, portals, tenants, and sessions across the entire infrastructure.

All admin endpoints require authentication with a valid access token for a client that has the ``admin`` role. See [Client Authentication](client-authentication) for details on obtaining an access token.

An endpoint naming a client, cluster, portal, environment or tenant which does not exist answers HTTP 404, with a plain-text message such as ``Cluster not available``.

Client endpoints
----------------

* ``GET /api/v1/clients`` - List all configured clients with their names, roles, and tenant access.
* ``GET /api/v1/clients/<client>`` - Get details for a specific client.

Cluster endpoints
-----------------

* ``GET /api/v1/clusters`` - List all registered clusters with their names and labels. A cluster whose ``ClusterConfig`` gives no kubeconfig the lookup service can use, such as one whose kubeconfig is refused, is not listed.
* ``GET /api/v1/clusters/<cluster>`` - Get details for a specific cluster.
* ``GET /api/v1/clusters/<cluster>/kubeconfig`` - Retrieve the kubeconfig the lookup service uses for a specific cluster as YAML, holding only the context it uses, with its credentials.
* ``GET /api/v1/clusters/<cluster>/portals`` - List all training portals on a specific cluster.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>`` - Get details for a specific portal on a cluster, including capacity and allocation.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments`` - List workshop environments for a specific portal.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments/<environment>`` - Get details for a specific workshop environment, including capacity, reserved slots, and allocation.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments/<environment>/sessions`` - List every workshop session of a specific environment, whether it is reserved and waiting for a user, awaiting activation, in use or stopping.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments/<environment>/users`` - List the user identifiers of the sessions of a specific environment, each once, with ``null`` standing for the reserved sessions which have no user yet.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments/<environment>/users/<user>/sessions`` - List sessions for a specific user in a specific environment.

In the details of a portal or a workshop environment, ``allocated`` is the number of its sessions which are awaiting activation, in use or stopping. In the details of a workshop environment, ``available`` is the number of its reserved sessions waiting for a user. A ``capacity`` of ``0`` for a portal means it has no maximum number of sessions. For a workshop environment, a ``capacity`` of ``0`` stands both for a workshop environment whose status gives no capacity, which the lookup service treats as having no limit, and for one whose capacity is ``0``, which is never a candidate for a session request. The ``status.educates.capacity`` of the ``WorkshopEnvironment`` tells the two apart.

Portal endpoints
----------------

* ``GET /api/v1/portals`` - List all training portals across all clusters.

Tenant endpoints
----------------

* ``GET /api/v1/tenants`` - List all configured tenants with their mapped clients.
* ``GET /api/v1/tenants/<tenant>`` - Get details for a specific tenant.
* ``GET /api/v1/tenants/<tenant>/portals`` - List the training portals accessible through a specific tenant.
* ``GET /api/v1/tenants/<tenant>/workshops`` - List the workshops available through a specific tenant.

Note that a client with the ``tenant`` role can also access ``GET /api/v1/clients/<client>`` for its own client details, and ``GET /api/v1/tenants/<tenant>/workshops`` for the tenants it has been granted access to, as described in [Workshops REST API](workshops-api), but cannot access any other admin endpoints.
