(client-authentication)=
Client Authentication
=====================

All access to the lookup service REST API requires authentication. Clients authenticate using a username and password to obtain a time-limited access token, which is then included in subsequent API requests as a bearer token.

Authenticating a client
-----------------------

To authenticate, send an HTTP ``POST`` request to the ``/auth/login`` endpoint with a JSON body containing the client username and password:

```
curl -X POST http://educates-api.<ingress-domain>/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "<name>", "password": "<password>"}'
```

The username corresponds to the ``metadata.name`` of the ``ClientConfig`` resource, and the password corresponds to the ``spec.client.password`` field.

On success, the response will be an HTTP 200 with a JSON body:

```json
{
  "access_token": "<token>",
  "token_type": "Bearer",
  "expires_at": 1706745600
}
```

The ``access_token`` is a JWT token that should be used in subsequent API requests. The ``token_type`` will always be ``Bearer``. The ``expires_at`` field is the token expiration time expressed as seconds since the UNIX epoch.

The access token currently expires after 72 hours, however this default may change in future versions. Clients should not assume a fixed expiration period and should always be prepared to handle token expiry regardless of the expected lifetime.

If the request body is malformed or missing required fields, the response will be HTTP 400. If the credentials are incorrect, the response will be HTTP 401.

Note that the ``/login`` endpoint also exists but is deprecated. Clients should use ``/auth/login`` instead.

Using the access token
----------------------

After obtaining an access token, include it in the ``Authorization`` header of subsequent API requests using the ``Bearer`` scheme:

```
curl -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  http://educates-api.<ingress-domain>/api/v1/workshops?tenant=<tenant-name>
```

If the token has expired or been revoked, the API will return HTTP 401. When this occurs, the client should re-authenticate by calling the ``/auth/login`` endpoint again to obtain a new token.

Verifying a token
-----------------

To check whether a token is still valid without making an API call, send an HTTP ``GET`` request to the ``/auth/verify`` endpoint:

```
curl -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  http://educates-api.<ingress-domain>/auth/verify
```

If the token is valid, the response will be HTTP 200. If the token has expired or been revoked, the response will be HTTP 401.

Token revocation
----------------

An access token can be explicitly revoked by calling the ``/auth/logout`` endpoint:

```
curl -X POST -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  http://educates-api.<ingress-domain>/auth/logout
```

In addition to explicit revocation, access tokens are also invalidated in the following circumstances:

* The token reaches its expiration time.
* The ``ClientConfig`` resource for the client is deleted.
* The ``ClientConfig`` resource is deleted and immediately recreated (even with the same name and password).
* The lookup service is restarted.

A custom front-end portal using the lookup service should not assume that a previously obtained token will remain valid indefinitely. It should be designed to handle an HTTP 401 response at any time by re-authenticating and retrying the request.

Capturing the token in a script
-------------------------------

When testing the REST API from the command line, a common pattern is to capture the access token in an environment variable for use in subsequent commands:

```
ACCESS_TOKEN=$(curl --silent -X POST \
  http://educates-api.<ingress-domain>/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "custom-portal", "password": "my-secret"}' \
  | jq -r -e .access_token) && echo $ACCESS_TOKEN
```

The token can then be referenced in subsequent ``curl`` commands using ``${ACCESS_TOKEN}``.
