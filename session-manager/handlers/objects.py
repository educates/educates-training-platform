from pykube import object_factory, HTTPClient, KubeConfig
from pykube.objects import APIObject, NamespacedAPIObject


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


def create_from_dict(body):
    resource = Resource(api, body)
    resource.create()
