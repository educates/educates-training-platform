"""Defines how the training portal builds its clients for the Kubernetes
REST API.

"""

import pykube


def kubernetes_api():
    """Returns a new client for the Kubernetes REST API. The client reads the
    portal's service account token when built and keeps it for its life. The
    kubelet replaces the projected token before it expires, so build a client
    for each use and do not keep one, so that every use reads the current
    token.

    """

    return pykube.HTTPClient(pykube.KubeConfig.from_env())
