Workshops REST API
==================

The lookup service REST API provides endpoints for querying available workshops and requesting workshop sessions. These endpoints are available to clients with either the ``admin`` or ``tenant`` role.

Listing available workshops
---------------------------

To retrieve the list of workshops available through a tenant, send an HTTP ``GET`` request to the ``/api/v1/workshops`` endpoint with the ``tenant`` query string parameter:

```
curl -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  http://educates-api.<ingress-domain>/api/v1/workshops?tenant=<tenant-name>
```

The ``tenant`` parameter is required and specifies which tenant to query workshops for. A client with the ``tenant`` role can only query tenants it has been granted access to, as defined in the ``spec.tenants`` field of its ``ClientConfig`` resource.

On success, the response will be an HTTP 200 with a JSON body listing the available workshops:

```json
{
  "workshops": [
    {
      "name": "lab-k8s-fundamentals",
      "title": "Kubernetes Fundamentals",
      "description": "An introduction to Kubernetes concepts"
    }
  ]
}
```

Each workshop entry includes the workshop ``name`` (used when requesting a session), along with the ``title`` and ``description`` for display purposes.

If the same workshop is available through multiple training portals within the tenant, it will appear only once in the list. The lookup service handles the deduplication.

The possible error responses are:

* HTTP 400 - The ``tenant`` query string parameter is missing.
* HTTP 401 - The access token is invalid or has expired.
* HTTP 403 - The client is not permitted to access the specified tenant.
* HTTP 503 - The specified tenant configuration does not exist.

A custom front-end portal can use this endpoint to dynamically build a catalog of available workshops for its users. Alternatively, if the portal maintains its own database of workshops, it may not need to call this endpoint and can instead make session requests directly.

(requesting-a-workshop-session)=
Requesting a workshop session
-----------------------------

To request a new workshop session, send an HTTP ``POST`` request to the ``/api/v1/workshops`` endpoint with a JSON body:

```
curl -X POST -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "tenantName": "tenant-1",
    "workshopName": "lab-k8s-fundamentals",
    "clientIndexUrl": "https://portal.example.com/",
    "clientUserId": "user-12345"
  }' \
  http://educates-api.<ingress-domain>/api/v1/workshops
```

The request body is a JSON object with the following properties:

* ``tenantName`` (required) - The name of the tenant to request the workshop session through. The tenant determines which clusters and portals are considered when allocating the session.

* ``workshopName`` (required) - The name of the workshop to request a session for. This must match the ``name`` of one of the workshops returned by the listing endpoint.

* ``clientIndexUrl`` (optional) - A URL that the user will be redirected to when the workshop session ends. This could be the main page of your custom portal. If not specified, the user will be redirected to the training portal's own index page when the session ends. For more details on how redirection works, see [Portal Integration](portal-integration).

* ``clientUserId`` (optional) - A unique identifier for the end user requesting the workshop. If provided and the user already has an active session for the same workshop, the existing session will be returned rather than creating a new one. This prevents users from accidentally creating duplicate sessions and allows them to reconnect to an existing session if they close their browser. If not provided, a new session will be created for every request. It is recommended that custom portals always provide this parameter.

* ``clientActionId`` (optional) - An identifier for tracking this specific session request. This is for the custom portal's own use and is not used by the lookup service beyond including it in internal logging.

* ``userEmailAddress`` (optional) - The email address of the end user. This is passed through to the training portal and recorded against the user's session for administrative reference.

* ``userFirstName`` (optional) - The first name of the end user. This is passed through to the training portal and recorded against the user's session for administrative reference.

* ``userLastName`` (optional) - The last name of the end user. This is passed through to the training portal and recorded against the user's session for administrative reference.

* ``workshopParams`` (optional) - A list of parameter objects to pass to the workshop session. Each object should have ``name`` and ``value`` fields. The workshop definition must declare in its configuration what parameters it accepts. These parameters can be used to customize the workshop experience for individual users.

* ``analyticsWebhookUrl`` (optional) - A webhook URL for receiving analytics events for this specific workshop session. If provided, the training portal will send analytics events to this URL as the user progresses through the workshop. It is recommended that the custom portal include its own unique identifier as part of this URL if it needs to distinguish events for different workshop sessions.

On success, the response will be an HTTP 200 with a JSON body:

```json
{
  "tenantName": "tenant-1",
  "clusterName": "remote-cluster-1",
  "portalName": "portal-1",
  "environmentName": "lab-k8s-fundamentals-w01",
  "sessionName": "lab-k8s-fundamentals-w01-s001",
  "clientUserId": "user-12345",
  "sessionActivationUrl": "https://portal-1.cluster-1.example.com/workshops/session/lab-k8s-fundamentals-w01-s001/activate/?token=..."
}
```

The key property in the response is ``sessionActivationUrl``. This is the URL that the end user's browser should be directed to in order to activate and access the workshop session. The remaining properties (``tenantName``, ``clusterName``, ``portalName``, ``environmentName``, ``sessionName``) are provided for debugging and auditing purposes.

The activation URL contains a time-limited token that is valid for 60 seconds. The user's browser must be directed to this URL within that window to activate the session. If the activation token expires before the user accesses the URL, the session will be automatically cleaned up and a new session request will need to be made.

The possible error responses are:

* HTTP 400 - The request body is malformed or missing required fields.
* HTTP 401 - The access token is invalid or has expired.
* HTTP 403 - The client is not permitted to access the specified tenant.
* HTTP 503 - The specified tenant configuration does not exist, or no capacity is available to fulfill the request.

Session allocation
------------------

When a workshop session is requested, the lookup service selects the most appropriate training portal and workshop environment based on available capacity. The selection process considers:

* Whether the training portal has remaining session capacity (portals can have a maximum session limit).
* Whether the workshop environment has remaining capacity (environments can have a maximum number of concurrent slots).
* The number of reserved session slots available in each environment.
* The current allocation level across environments, preferring those with the most available capacity.

If the same workshop is available through multiple training portals across different clusters within the tenant, the lookup service will distribute session requests across them based on remaining capacity. This provides natural load balancing and allows you to scale capacity by adding more clusters and training portals rather than scaling individual clusters.

End user identification
-----------------------

The ``clientUserId`` parameter plays an important role in session management. When provided, the lookup service checks whether the identified user already has an active session for the requested workshop. If so, the existing session's activation URL is returned instead of creating a new session.

This behavior is useful in several scenarios:

* If a user accidentally closes their browser tab, they can return to the portal and click the workshop again to reconnect to their existing session.
* It prevents a single user from consuming multiple session slots for the same workshop.
* It allows the custom portal to implement a "resume" workflow for workshops in progress.

If ``clientUserId`` is not provided, every request will create a new session (subject to capacity), with no deduplication or reconnection capability.
