Workshops REST API
==================

The lookup service REST API provides endpoints for querying available workshops and requesting workshop sessions. These endpoints are available to clients with either the ``admin`` or ``tenant`` role.

Listing available workshops
---------------------------

To retrieve the list of workshops available through a tenant, send an HTTP ``GET`` request to the ``/api/v1/workshops`` endpoint with the ``tenant`` query string parameter:

```
curl -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  https://lookup.<ingress-domain>/api/v1/workshops?tenant=<tenant-name>
```

The ``tenant`` parameter specifies which tenant to query workshops for. A client with the ``tenant`` role must supply it, and can only query tenants it has been granted access to, as defined in the ``spec.tenants`` field of its ``ClientConfig`` resource. For a client with only the ``admin`` role the parameter is optional: without it, the response lists the workshops of every training portal on every cluster, and with it, those of the tenant, whatever the client's ``spec.tenants`` says.

The same list for a tenant can also be requested with the tenant in the path:

```
curl -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  https://lookup.<ingress-domain>/api/v1/tenants/<tenant-name>/workshops
```

On success, the response will be an HTTP 200 with a JSON body listing the available workshops:

```json
{
  "workshops": [
    {
      "name": "lab-k8s-fundamentals",
      "title": "Kubernetes Fundamentals",
      "description": "An introduction to Kubernetes concepts",
      "labels": [
        {
          "name": "level",
          "value": "beginner"
        }
      ]
    }
  ]
}
```

Each workshop entry includes the workshop ``name`` (used when requesting a session), along with the ``title`` and ``description`` for display purposes, and the ``labels`` of the workshop definition, its ``spec.labels``. The ``name`` is the name the training portal offers the workshop under, which is its ``alias`` when the training portal gives one. The ``title`` and ``description`` are ``null`` for a workshop that does not give them, and a label without a value has no ``value`` property.

Only workshops whose workshop environment is running are listed. If the same workshop is available through multiple training portals within the tenant, it will appear only once in the list. The lookup service handles the deduplication.

The possible error responses are:

* HTTP 400 - The request has no access token, or a client with the ``tenant`` role did not give the ``tenant`` query string parameter.
* HTTP 401 - The access token is invalid, has expired or has been revoked.
* HTTP 403 - The client is not permitted to access the specified tenant.
* HTTP 503 - The specified tenant configuration does not exist. The endpoint with the tenant in the path answers HTTP 404 instead.

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
  https://lookup.<ingress-domain>/api/v1/workshops
```

The request body is a JSON object with the following properties:

* ``tenantName`` (required) - The name of the tenant to request the workshop session through. The tenant determines which clusters and portals are considered when allocating the session. The client must be granted access to the tenant by the ``spec.tenants`` field of its ``ClientConfig`` resource, whether it has the ``tenant`` or the ``admin`` role.

* ``workshopName`` (required) - The name of the workshop to request a session for. This must match the ``name`` of one of the workshops returned by the listing endpoint.

* ``clientIndexUrl`` (required) - A URL that the user will be redirected to when the workshop session ends. This could be the main page of your custom portal. A request without one is refused with HTTP 400 and the message ``Missing clientIndexUrl``, before any training portal is contacted. For more details on how redirection works, see [Portal Integration](portal-integration).

* ``clientUserId`` (optional) - A unique identifier for the end user requesting the workshop. If provided and the user already has a live session for the same workshop, the existing session will be returned rather than creating a new one, as described under [End user identification](end-user-identification). This prevents users from accidentally creating duplicate sessions and allows them to reconnect to an existing session if they close their browser. If not provided, a new session will be created for every request. It is recommended that custom portals always provide this parameter. When the ``ClientConfig`` resource of the client sets ``spec.user``, that user is used for every request of the client in place of ``clientUserId``.

* ``clientActionId`` (optional) - An identifier for tracking this specific session request. This is for the custom portal's own use and is not used by the lookup service beyond including it in its logs.

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

The key property in the response is ``sessionActivationUrl``. This is the URL that the end user's browser should be directed to in order to activate and access the workshop session. The remaining properties (``tenantName``, ``clusterName``, ``portalName``, ``environmentName``, ``sessionName``) are provided for debugging and auditing purposes, and ``clientUserId`` is the user the session was requested for.

The activation URL contains a time-limited token that is valid for 60 seconds. The user's browser must be directed to this URL within that window to activate the session. If the activation token expires before the user accesses the URL, the session will be automatically cleaned up and a new session request will need to be made.

The possible error responses are:

* HTTP 400 - The request has no access token, its body is not a JSON object, such as malformed JSON, or it lacks ``tenantName``, ``workshopName`` or ``clientIndexUrl``. The plain-text message names a missing property, as in ``Missing clientIndexUrl``.
* HTTP 401 - The access token is invalid, has expired or has been revoked.
* HTTP 403 - The client is not permitted to access the specified tenant.
* HTTP 503 - The specified tenant configuration does not exist, with the message ``Tenant not available``, or no training portal gave a session, with the message ``Workshop not available``.

Session allocation
------------------

When a workshop session is requested for a user who has no live session of the workshop, the lookup service chooses the workshop environment to request it from.

The candidates are the running workshop environments of the requested workshop on the training portals the tenant can access, on the clusters the lookup service is connected to. A workshop environment whose capacity is 0 is never a candidate.

For each candidate, the lookup service counts as taken the sessions awaiting activation, in use or stopping, as well as the session requests it has sent to the workshop environment whose sessions it has not yet seen, and counts as available the reserved sessions waiting for a user. A training portal's taken sessions are those of all its workshop environments. Its capacity is the ``spec.portal.sessions.maximum`` of the ``TrainingPortal``, and a workshop environment's capacity is the one its training portal gives it.

The candidates are ordered by the following, each one deciding only between candidates that the ones before it rank equally:

1. Whether the training portal has room for another session. A training portal without a maximum always has room.
2. Whether the workshop environment has room for another session. A workshop environment without a capacity always has room.
3. The training portal's remaining capacity, highest first. A training portal without a maximum counts as having one session left, so a training portal with a maximum and more room to spare comes first.
4. The number of reserved sessions the workshop environment has waiting for a user, highest first.
5. The workshop environment's remaining capacity, highest first. A workshop environment without a capacity counts as having one session left.

Candidates that rank equally are ordered at random for each request, so the order in which clusters and training portals were configured never decides where a session goes.

With the same workshop on two clusters at the same capacity, new sessions therefore alternate between the clusters. With different capacities, the cluster with more room takes new sessions until both have the same room left, and then the two alternate. This provides natural load balancing and allows you to scale capacity by adding more clusters and training portals rather than scaling individual clusters.

The lookup service then requests a session from the candidates in that order, and moves on to the next candidate when a training portal refuses the request, answers with an error, cannot be reached, or does not answer within 10 seconds. A full candidate is still tried, last, since its training portal may have room the lookup service has not yet seen. When no candidate gives a session, the request fails with HTTP 503 and the message ``Workshop not available``. The reason each candidate failed is in the lookup service logs, with the cluster, training portal and workshop environment it concerns.

(end-user-identification)=
End user identification
-----------------------

The ``clientUserId`` parameter plays an important role in session management. When provided, the lookup service checks whether the identified user already has a live session for the requested workshop, one awaiting activation or in use, on a training portal the tenant can access. If so, it asks the training portal for that session again and returns an activation URL for it instead of creating a new session.

Only the training portals the tenant can access are searched, so two tenants whose custom portals use the same user identifier never reach each other's sessions. A session that is stopping is never returned. If the training portal does not give the session back, the lookup service tries the user's next live session, if there is one, and otherwise requests a new session as described above.

This behavior is useful in several scenarios:

* If a user accidentally closes their browser tab, they can return to the portal and click the workshop again to reconnect to their existing session.
* It prevents a single user from consuming multiple session slots for the same workshop.
* It allows the custom portal to implement a "resume" workflow for workshops in progress.

If ``clientUserId`` is not provided, and the client's ``ClientConfig`` sets no ``spec.user``, every request will create a new session (subject to capacity), with no deduplication or reconnection capability.
