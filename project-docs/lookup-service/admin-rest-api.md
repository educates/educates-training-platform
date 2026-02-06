Admin REST API
==============

In addition to the workshop listing and session request endpoints available to clients with the ``tenant`` role, the lookup service provides a set of administrative endpoints for clients with the ``admin`` role. These endpoints allow querying the state of clusters, portals, tenants, and sessions across the entire infrastructure.

All admin endpoints require authentication with a valid access token for a client that has the ``admin`` role. See [Client Authentication](client-authentication) for details on obtaining an access token.

Client endpoints
----------------

* ``GET /api/v1/clients`` - List all configured clients with their names, roles, and tenant access.
* ``GET /api/v1/clients/<client>`` - Get details for a specific client.

Cluster endpoints
-----------------

* ``GET /api/v1/clusters`` - List all registered clusters with their names and labels.
* ``GET /api/v1/clusters/<cluster>`` - Get details for a specific cluster.
* ``GET /api/v1/clusters/<cluster>/kubeconfig`` - Retrieve the kubeconfig for a specific cluster as YAML.
* ``GET /api/v1/clusters/<cluster>/portals`` - List all training portals on a specific cluster.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>`` - Get details for a specific portal on a cluster, including capacity and allocation.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments`` - List workshop environments for a specific portal.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments/<environment>`` - Get details for a specific workshop environment, including capacity, reserved slots, and allocation.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments/<environment>/sessions`` - List active workshop sessions in a specific environment.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments/<environment>/users`` - List unique user identifiers with active sessions in a specific environment.
* ``GET /api/v1/clusters/<cluster>/portals/<portal>/environments/<environment>/users/<user>/sessions`` - List sessions for a specific user in a specific environment.

Portal endpoints
----------------

* ``GET /api/v1/portals`` - List all training portals across all clusters.

Tenant endpoints
----------------

* ``GET /api/v1/tenants`` - List all configured tenants with their mapped clients.
* ``GET /api/v1/tenants/<tenant>`` - Get details for a specific tenant.
* ``GET /api/v1/tenants/<tenant>/portals`` - List the training portals accessible through a specific tenant.
* ``GET /api/v1/tenants/<tenant>/workshops`` - List the workshops available through a specific tenant.

Note that a client with the ``tenant`` role can also access ``GET /api/v1/clients/<client>`` for its own client details, but cannot access any other admin endpoints.
