Workshop Session
================

The ``WorkshopSession`` custom resource defines a workshop session.

The raw custom resource definition for the ``WorkshopSession`` custom resource can be viewed by running:

```
kubectl get crd/workshopsessions.training.educates.dev -o yaml
```

Note that as deployment and use of workshops is now all typically managed via a training portal, there is no need to interact with internal custom resources directly such as ``WorkshopEnvironment``, ``WorkshopSession`` and ``WorkshopRequest`` directly. In other words this resource has become an internal implementation detail of the larger system. For that reason the documentation here may not be completely up to date.

Specifying the session identity
-------------------------------

When running training for multiple people, it would be more typical to use the ``TrainingPortal`` custom resource to set up a training environment. Alternatively you would set up a workshop environment using the ``WorkshopEnvironment`` custom resource, then create requests for workshop instances using the ``WorkshopRequest`` custom resource. If doing the latter and you need more control over how the workshop instances are set up, you can use ``WorkshopSession`` custom resource instead of ``WorkshopRequest``.

To specify the workshop environment the workshop instance is created against, set the ``environment.name`` field of the specification for the workshop session. At the same time, you must specify the session ID for the workshop instance.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: WorkshopSession
metadata:
  name: lab-markdown-sample-user1
spec:
  environment:
    name: lab-markdown-sample
  session:
    id: user1
```

The ``name`` of the workshop specified in the ``metadata`` of the training environment needs to be globally unique for the workshop instance being created. You would need to create a separate ``WorkshopSession`` custom resource for each workshop instance you want.

The session ID needs to be unique within the workshop environment the workshop instance is being created against.

Specifying the login credentials
--------------------------------

Access to each workshop instance can be controlled through login credentials. This is so that a workshop attendee cannot interfere with another.

If you want to set login credentials for a workshop instance, you can set the ``session.username`` and ``session.password`` fields.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: WorkshopSession
metadata:
  name: lab-markdown-sample
spec:
  environment:
    name: lab-markdown-sample-user1
  session:
    username: educates
    password: lab-markdown-sample
```

If you do not specify login credentials, there will not be any access controls on the workshop instance and anyone will be able to access it.

Specifying the config password
------------------------------

The workshop instance serves its configuration, such as its kubeconfig and
SSH private key, to any request carrying its config password. A training
portal passes the config password of each workshop session it creates in a
``Secret`` in the namespace of the workshop environment, and names that
``Secret`` in the ``session.config.passwordSecretRef.name`` field. The
``Secret`` holds the password under the key ``password``.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: WorkshopSession
metadata:
  name: lab-markdown-sample-user1
spec:
  environment:
    name: lab-markdown-sample
  session:
    id: user1
    config:
      passwordSecretRef:
        name: lab-markdown-sample-user1-config-password
```

The workshop instance is not created until the ``Secret`` exists and holds
the password. To have the ``Secret`` deleted along with the workshop session,
give it an owner reference to the ``WorkshopSession``.

You can instead set the config password itself in the
``session.config.password`` field. Anyone permitted to read ``WorkshopSession``
resources can then read it, and with it the configuration of the workshop
instance, so prefer a ``Secret``. If both fields are set, the ``Secret`` is
used. If neither is set, a random config password is generated.

Inside the workshop container, the config password is available in the
``CONFIG_PASSWORD`` environment variable, and in workshop instructions as the
``config_password`` variable.

Specifying the ingress domain
-----------------------------

In order to be able to access the workshop instance using a public URL, you will need to specify an ingress domain. If an ingress domain isn't specified, the default ingress domain that the Educates operator has been configured with will be used.

When setting a custom domain, DNS must have been configured with a wildcard domain to forward all requests for sub domains of the custom domain, to the ingress router of the Kubernetes cluster.

To provide the ingress domain, you can set the ``session.ingress.domain`` field.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: WorkshopSession
metadata:
  name: lab-markdown-sample
spec:
  environment:
    name: lab-markdown-sample-user1
  session:
    ingress:
      domain: training.educates.dev
```

A full hostname for the session will be created by prefixing the ingress domain with a hostname constructed from the name of the workshop environment and the session ID.

If overriding the domain, by default, the workshop session will be exposed using a HTTP connection. If you require a secure HTTPS connection, you will need to have access to a wildcard SSL certificate for the domain. A secret of type ``tls`` should be created for the certificate in the Educates operator namespace. The name of that secret should then be set in the ``session.ingress.secret`` field.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: WorkshopSession
metadata:
  name: lab-markdown-sample
spec:
  environment:
    name: lab-markdown-sample-user1
  session:
    ingress:
      domain: training.educates.dev
      secret: training.educates.dev-tls
```

If HTTPS connections are being terminated using an external load balancer and not by specificying a secret for ingresses managed by the Kubernetes ingress controller, with traffic then routed into the Kubernetes cluster as HTTP connections, you can override the ingress protocol without specifying an ingress secret by setting the ``session.ingress.protocol`` field.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: WorkshopSession
metadata:
  name: lab-markdown-sample
spec:
  environment:
    name: lab-markdown-sample-user1
  session:
    ingress:
      domain: training.educates.dev
      protocol: https
```

If you need to override or set the ingress class, which dictates which ingress router is used when more than one option is available, you can add ``session.ingress.class``.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: WorkshopSession
metadata:
  name: lab-markdown-sample
spec:
  environment:
    name: lab-markdown-sample-user1
  session:
    ingress:
      domain: training.educates.dev
      secret: training.educates.dev-tls
      class: nginx
```

Setting the environment variables
---------------------------------

If you want to set the environment variables for the workshop instance, you can provide the environment variables in the ``session.env`` field.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: WorkshopSession
metadata:
  name: lab-markdown-sample
spec:
  environment:
    name: lab-markdown-sample
  session:
    id: user1
    env:
    - name: REPOSITORY_URL
      value: https://github.com/educates/lab-markdown-sample
```

Values of fields in the list of resource objects can reference a number of pre-defined parameters. The available parameters are:

* ``session_id`` - A unique ID for the workshop instance within the workshop environment.
* ``session_namespace`` - The namespace created for and bound to the workshop instance. This is the namespace unique to the session and where a workshop can create their own resources.
* ``environment_name`` - The name of the workshop environment. For now this is the same as the name of the namespace for the workshop environment. Don't rely on them being the same, and use the most appropriate to cope with any future change.
* ``workshop_namespace`` - The namespace for the workshop environment. This is the namespace where all deployments of the workshop instances are created, and where the service account that the workshop instance runs as exists.
* ``service_account`` - The name of the service account the workshop instance runs as, and which has access to the namespace created for that workshop instance.
* ``ingress_domain`` - The host domain under which hostnames can be created when creating ingress routes.
* ``ingress_protocol`` - The protocol (http/https) that is used for ingress routes which are created for workshops.
* ``services_password`` - A unique random password value for use with arbitrary services deployed with a workshop.
* ``ssh_private_key`` - The private part of a unique SSH key pair generated for the workshop session.
* ``ssh_public_key`` - The public part of a unique SSH key pair generated for the workshop session.
* ``ssh_keys_secret`` - The name of the Kubernetes secret in the workshop namespace holding the SSH key pair for the workshop session.
* ``platform_arch`` - The CPU architecture the workshop container is running on, ``amd64`` or ``arm64``.

The syntax for referencing one of the parameters is ``$(parameter_name)``.

Note that if the workshop environment had specified a set of extra environment variables to be set for workshop instances, it is up to you to incorporate those in the set of environment variables you list under ``session.env``. That is, anything listed in ``session.env`` of the ``WorkshopEnvironment`` custom resource of the workshop environment is ignored.
