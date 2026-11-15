Client Authentication
=====================

The training portal web interface is a quick way of providing access to a set of workshops when running a supervised training workshop. For integrating access to workshops into an existing web site, or for creating a custom web interface for accessing workshops hosted across one or more training portals, you can use can use the portal REST API.

The REST API will give you access to the list of workshops hosted by a training portal instance and allow you to request and access workshop sessions. This bypasses the training portal's own user registration and login so you can implement whatever access controls you need. This could include anonymous access, or access integrated into an organisations single sign on system.

Querying the credentials
------------------------

To provide access to the REST API a robot account is automatically
provisioned for each training portal. Its robot credentials, the username and
password of the robot account together with the client id and client secret
of the OAuth client used for authentication, are kept in a secret in the
namespace of the training portal, ``<portal-name>-ui``, once the training
portal has been deployed.

The status of the training portal names the secret under
``status.educates.credentialSecrets.robot``, and the namespace it is in under
``status.educates.namespace``. The secret is
``training-portal-robot-credentials``, holding the robot credentials under the
keys ``username``, ``password``, ``client-id`` and ``client-secret``. To read
them for the training portal ``lab-markdown-sample`` using ``kubectl``, you
would use:

```
kubectl get secret training-portal-robot-credentials -n lab-markdown-sample-ui -o jsonpath='{.data.username}' | base64 --decode
kubectl get secret training-portal-robot-credentials -n lab-markdown-sample-ui -o jsonpath='{.data.password}' | base64 --decode
kubectl get secret training-portal-robot-credentials -n lab-markdown-sample-ui -o jsonpath='{.data.client-id}' | base64 --decode
kubectl get secret training-portal-robot-credentials -n lab-markdown-sample-ui -o jsonpath='{.data.client-secret}' | base64 --decode
```

Reading the secret needs ``get`` access on secrets in that namespace. The
``educates`` command line tool reads the robot credentials from the same
secret, through the kube context it is run against, when its
``educates cluster session`` commands and
``educates cluster workshop request`` log in to the REST API, so the identity
of that kube context needs the same access.

The robot credentials are what you would use if you wish to access the REST
API. The admin credentials of the training portal, which you would use to log
into the training portal web interface to access the admin pages, are kept in
a separate secret, which the status names under
``status.educates.credentialSecrets.admin``. The section on overriding portal
credentials in the [Training Portal](../custom-resources/training-portal)
documentation describes how the credentials are generated and how to set them
yourself.

Requesting an access token
--------------------------

Before you can make requests against the REST API to query details on workshops or request a workshop session, you need to login via the REST API to get an access token.

This would be done from any front end web application or provisioning system, but the step is equivalent to making a REST API call using ``curl`` of:

```
curl -v -X POST -d "grant_type=password&username=robot@educates&password=<robot-password>" -u "<robot-client-id>:<robot-client-secret>" https://lab-markdown-sample-ui.test/oauth2/token/
```

The URL sub path is ``/oauth2/token/``.

Upon success, the output will be a JSON response consisting of:

```
{
    "access_token": "tg31ied56fOo4axuhuZLHj5JpUYCEL",
    "expires_in": 36000,
    "token_type": "Bearer",
    "scope": "user:info",
    "refresh_token": "1ryXhXbNA9RsTRuCE8fDAyZToJmp30"
}
```

Refreshing the access token
---------------------------

The access token which is provided will expire and will need to be refreshed before it expires if being used by a long running application.

To refresh the access token you would use the equivalent of:

```
curl -v -X POST -d "grant_type=refresh_token&refresh_token=<refresh-token>&client_id=<robot-client-id>&client_secret=<robot-client-secret>" https://lab-markdown-sample-ui.test/oauth2/token/
```

As with requesting the initial access token, the URL sub path is ``/oauth2/token/``.

The JSON response will be of the same format as if a new token had been requested.

Revoking the access token
-------------------------

The access token would normally be retained by a client and used on subsequent requests until it expires. If this is not being done, a client should revoke the token so it is no longer valid.

To revoke the access token you would use the equivalent of:

```
curl -v -X POST -d "token=<access-token>&client_id=<robot-client-id>&client_secret=<robot-client-secret>" https://lab-markdown-sample-ui.test/oauth2/revoke-token/
```

The URL sub path is ``/oauth2/revoke-token/``.
