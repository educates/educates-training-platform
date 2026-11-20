(client-authentication)=
Client Authentication
=====================

All access to the lookup service REST API requires authentication. Clients authenticate using a username and password to obtain a time-limited access token, which is then included in subsequent API requests as a bearer token.

Authenticating a client
-----------------------

To authenticate, send an HTTP ``POST`` request to the ``/auth/login`` endpoint with a JSON body containing the client username and password:

```
curl -X POST https://lookup.<ingress-domain>/auth/login \
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

If the request body is not a JSON object, such as malformed JSON, or lacks the ``username`` or the ``password``, the response will be HTTP 400. If the credentials are incorrect, the response will be HTTP 401. Error responses carry a short plain-text message, such as ``Invalid username/password``.

Note that the ``/login`` endpoint also exists but is deprecated. Clients should use ``/auth/login`` instead.

Using the access token
----------------------

After obtaining an access token, include it in the ``Authorization`` header of subsequent API requests using the ``Bearer`` scheme:

```
curl -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  https://lookup.<ingress-domain>/api/v1/workshops?tenant=<tenant-name>
```

If the token has expired or been revoked, the API will return HTTP 401. When this occurs, the client should re-authenticate by calling the ``/auth/login`` endpoint again to obtain a new token.

A request without an ``Authorization`` header gets HTTP 400 with the message ``JWT token not supplied``, and a request whose ``Authorization`` header is not of the form ``Bearer <token>`` gets HTTP 400 too.

Verifying a token
-----------------

To check whether a token is still valid without making an API call, send an HTTP ``GET`` request to the ``/auth/verify`` endpoint:

```
curl -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  https://lookup.<ingress-domain>/auth/verify
```

If the token is valid, the response will be HTTP 200. If the token has expired or been revoked, the response will be HTTP 401. A request without a token gets HTTP 400.

Token revocation
----------------

Access tokens can be explicitly revoked by calling the ``/auth/logout`` endpoint:

```
curl -X POST -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  https://lookup.<ingress-domain>/auth/logout
```

Logging out revokes every token issued to the client up to that moment, not only the token the request carries, so any other process using the same client credentials also needs to authenticate again.

In addition to explicit revocation, access tokens are also invalidated in the following circumstances:

* The token reaches its expiration time.
* The ``ClientConfig`` resource for the client is deleted.
* The ``ClientConfig`` resource is deleted and recreated, even with the same name and password.
* The ``spec.client.password`` of the ``ClientConfig`` resource is changed. This lets you lock out whoever holds tokens obtained with a leaked password.

Any other change to the ``ClientConfig`` resource, such as to its roles or tenants, leaves its tokens as they were: a valid token stays valid, with the client's new roles and tenants applying to it at once, and a revoked token stays revoked.

Tokens stay valid when the lookup service restarts. The lookup service keeps the key it signs tokens with, and the record of which tokens each client has revoked, in the secret ``lookup-service-tokens`` in the ``educates`` namespace, which it creates when it first starts. Uninstalling the lookup service, or Educates, leaves that secret in place, so tokens issued before are still valid when the lookup service is installed again. Delete the secret while the lookup service is not installed if you want a new installation to start with a new signing key, which invalidates every token issued before.

While the lookup service is upgraded or its pod replaced, the old and the new pod run side by side for a moment and share that secret. Each reads the secret again every 10 seconds, so a logout or password change which one pod handles is honoured by the other within 10 seconds.

A custom front-end portal using the lookup service should not assume that a previously obtained token will remain valid indefinitely. It should be designed to handle an HTTP 401 response at any time by re-authenticating and retrying the request.

Capturing the token in a script
-------------------------------

When testing the REST API from the command line, a common pattern is to capture the access token in an environment variable for use in subsequent commands:

```
ACCESS_TOKEN=$(curl --silent -X POST \
  https://lookup.<ingress-domain>/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "custom-portal", "password": "my-secret"}' \
  | jq -r -e .access_token) && echo $ACCESS_TOKEN
```

The token can then be referenced in subsequent ``curl`` commands using ``${ACCESS_TOKEN}``.
