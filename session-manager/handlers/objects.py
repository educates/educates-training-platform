import logging

import pykube
from pykube import object_factory, HTTPClient, KubeConfig
from pykube.objects import APIObject, NamespacedAPIObject

from .helpers import add_image_pull_secrets
from .operator_config import IMAGE_PULL_SECRETS

logger = logging.getLogger("educates")

api = HTTPClient(KubeConfig.from_env())


def Resource(api, body):
    return object_factory(api, body["apiVersion"], body["kind"])(api, body)


class Workshop(APIObject):
    version = "training.educates.dev/v1beta1"
    endpoint = "workshops"
    kind = "Workshop"


class WorkshopEnvironment(APIObject):
    version = "training.educates.dev/v1beta1"
    endpoint = "workshopenvironments"
    kind = "WorkshopEnvironment"


class WorkshopSession(APIObject):
    version = "training.educates.dev/v1beta1"
    endpoint = "workshopsessions"
    kind = "WorkshopSession"


class WorkshopRequest(NamespacedAPIObject):
    version = "training.educates.dev/v1beta1"
    endpoint = "workshoprequests"
    kind = "WorkshopRequest"


class TrainingPortal(APIObject):
    version = "training.educates.dev/v1beta1"
    endpoint = "trainingportals"
    kind = "TrainingPortal"


class SecretCopier(APIObject):
    version = "secrets.educates.dev/v1beta1"
    endpoint = "secretcopiers"
    kind = "SecretCopier"


def name_image_pull_secrets(body):
    """Names the platform's image pull Secrets on a ServiceAccount body
    about to be created, so that every pod created under it can pull with
    them, and returns the body. A pod takes its ServiceAccount's image
    pull secrets only when it is created, which can be before the
    SecretInjector reaches a new ServiceAccount. Bodies of any other kind
    are returned as they are.

    """

    add_image_pull_secrets(body, IMAGE_PULL_SECRETS)
    return body


def ensure_default_service_account(namespace):
    """Names the platform's image pull Secrets on the default
    ServiceAccount of a namespace the session manager has just created,
    before it creates anything there that runs pods. It creates the
    ServiceAccount itself, or names them on the one Kubernetes created
    first, retrying while another writer, such as the SecretInjector,
    updates it too. Failures are logged rather than raised: the
    SecretInjector still names the Secrets on the ServiceAccount later.

    """

    if not IMAGE_PULL_SECRETS:
        return

    body = name_image_pull_secrets(
        {
            "apiVersion": "v1",
            "kind": "ServiceAccount",
            "metadata": {"name": "default", "namespace": namespace},
        }
    )

    try:
        try:
            pykube.ServiceAccount(api, body).create()
            return

        except pykube.exceptions.HTTPError as exc:
            if exc.code != 409:
                raise

        for _ in range(5):
            service_account = pykube.ServiceAccount.objects(
                api, namespace=namespace
            ).get(name="default")

            if not add_image_pull_secrets(service_account.obj, IMAGE_PULL_SECRETS):
                return

            try:
                service_account.update()
                return

            except pykube.exceptions.HTTPError as exc:
                if exc.code != 409:
                    raise

        logger.warning(
            "Gave up naming image pull secrets on the default service account of namespace %s after repeated conflicts.",
            namespace,
        )

    except Exception:
        logger.exception(
            "Unable to name image pull secrets on the default service account of namespace %s.",
            namespace,
        )


def create_from_dict(body):
    name_image_pull_secrets(body)
    resource = Resource(api, body)
    resource.create()
    if body["apiVersion"] == "v1" and body["kind"].lower() == "namespace":
        ensure_default_service_account(body["metadata"]["name"])
