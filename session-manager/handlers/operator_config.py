import os
import yaml
import logging
import string
import random
import socket

from .helpers import xget

logger = logging.getLogger("educates")

config_values = {}

if os.path.exists("/opt/app-root/config/educates-operator-config.yaml"):
    with open("/opt/app-root/config/educates-operator-config.yaml") as fp:
        config_values = yaml.load(fp, Loader=yaml.Loader)

PLATFORM_ARCH = os.environ.get("PLATFORM_ARCH", "")

OPERATOR_NAMESPACE = xget(config_values, "operator.namespace", "educates")

if os.path.exists("/var/run/secrets/kubernetes.io/serviceaccount/namespace"):
    with open("/var/run/secrets/kubernetes.io/serviceaccount/namespace") as fp:
        OPERATOR_NAMESPACE = fp.read().strip()

OPERATOR_API_GROUP = xget(config_values, "operator.apiGroup", "educates.dev")

OPERATOR_STATUS_KEY = xget(config_values, "operator.statusKey", "educates")
OPERATOR_NAME_PREFIX = xget(config_values, "operator.namePrefix", "educates")

IMAGE_REGISTRY_HOST = xget(config_values, "imageRegistry.host")
IMAGE_REGISTRY_NAMESPACE = xget(config_values, "imageRegistry.namespace")

if IMAGE_REGISTRY_HOST:
    if IMAGE_REGISTRY_NAMESPACE:
        IMAGE_REPOSITORY = f"{IMAGE_REGISTRY_HOST}/{IMAGE_REGISTRY_NAMESPACE}"
    else:
        IMAGE_REPOSITORY = IMAGE_REGISTRY_HOST
else:
    IMAGE_REPOSITORY = "registry.default.svc.cluster.local"

RUNTIME_CLASS = xget(config_values, "clusterRuntime.class", "")

CLUSTER_DOMAIN = socket.getaddrinfo("kubernetes.default.svc", 0, flags=socket.AI_CANONNAME)[0][3]

if CLUSTER_DOMAIN.startswith("kubernetes.default.svc."):
    CLUSTER_DOMAIN = CLUSTER_DOMAIN.replace("kubernetes.default.svc.", "")
else:
    CLUSTER_DOMAIN = "cluster.local"

INGRESS_DOMAIN = xget(config_values, "clusterIngress.domain", "educates-local-dev.test")
INGRESS_CLASS = xget(config_values, "clusterIngress.class", "")

INGRESS_SECRET = xget(config_values, "clusterIngress.tlsCertificateRef.name")

if not INGRESS_SECRET:
    tls_certficate = xget(config_values, "clusterIngress.tlsCertificate", {})
    if (
        tls_certficate
        and tls_certficate.get("tls.crt")
        and tls_certficate.get("tls.key")
    ):
        INGRESS_SECRET = f"{INGRESS_DOMAIN}-tls"

INGRESS_CA_SECRET = xget(config_values, "clusterIngress.caCertificateRef.name")

if not INGRESS_CA_SECRET:
    ca_certficate = xget(config_values, "clusterIngress.caCertificate", {})
    if ca_certficate and ca_certficate.get("ca.crt"):
        INGRESS_CA_SECRET = f"{INGRESS_DOMAIN}-ca"

INGRESS_PROTOCOL = xget(config_values, "clusterIngress.protocol", "")

if not INGRESS_PROTOCOL:
    if INGRESS_SECRET:
        INGRESS_PROTOCOL = "https"
    else:
        INGRESS_PROTOCOL = "http"

SESSION_COOKIE_DOMAIN = xget(config_values, "sessionCookies.domain", "")

CLUSTER_STORAGE_CLASS = xget(config_values, "clusterStorage.class", "")
CLUSTER_STORAGE_USER = xget(config_values, "clusterStorage.user")
CLUSTER_STORAGE_GROUP = xget(config_values, "clusterStorage.group", 1)

CLUSTER_SECURITY_POLICY_ENGINE = xget(config_values, "clusterSecurity.policyEngine")
WORKSHOP_SECURITY_RULES_ENGINE = xget(config_values, "workshopSecurity.rulesEngine")

DOCKERD_MTU = xget(config_values, "dockerDaemon.networkMTU", 1400)
DOCKERD_MIRROR_REMOTE = xget(config_values, "dockerDaemon.proxyCache.remoteURL")
DOCKERD_MIRROR_USERNAME = xget(config_values, "dockerDaemon.proxyCache.username", "")
DOCKERD_MIRROR_PASSWORD = xget(config_values, "dockerDaemon.proxyCache.password", "")

NETWORK_BLOCKCIDRS = xget(config_values, "clusterNetwork.blockCIDRs", [])

DEFAULT_THEME_NAME = xget(config_values, "websiteStyling.defaultTheme", "")

FRAME_ANCESTORS = xget(config_values, "websiteStyling.frameAncestors", [])

GOOGLE_TRACKING_ID = xget(config_values, "workshopAnalytics.google.trackingId", "")
CLARITY_TRACKING_ID = xget(config_values, "workshopAnalytics.clarity.trackingId", "")
AMPLITUDE_TRACKING_ID = xget(config_values, "workshopAnalytics.amplitude.trackingId", "")

ANALYTICS_WEBHOOK_URL = xget(config_values, "workshopAnalytics.webhook.url", "")


def generate_password(length):
    characters = string.ascii_letters + string.digits
    return "".join(random.sample(characters, length))


PORTAL_ADMIN_USERNAME = xget(
    config_values, "trainingPortal.credentials.admin.username", "educates"
)
PORTAL_ADMIN_PASSWORD = xget(
    config_values, "trainingPortal.credentials.admin.password", generate_password(32)
)
PORTAL_ROBOT_USERNAME = xget(
    config_values, "trainingPortal.credentials.robot.username", "robot@educates"
)
PORTAL_ROBOT_PASSWORD = xget(
    config_values, "trainingPortal.credentials.robot.password", generate_password(32)
)
PORTAL_ROBOT_CLIENT_ID = xget(
    config_values, "trainingPortal.clients.robot.id", generate_password(32)
)
PORTAL_ROBOT_CLIENT_SECRET = xget(
    config_values, "trainingPortal.clients.robot.secret", generate_password(32)
)


def image_reference(name):
    version = xget(config_values, "version", "latest")
    image = f"{IMAGE_REPOSITORY}/educates-{name}:{version}"
    image_versions = xget(config_values, "imageVersions", [])
    for item in list(image_versions):
        if item["name"] == name:
            image = item["image"]
            break
    return image


TRAINING_PORTAL_IMAGE = image_reference("training-portal")
DOCKER_IN_DOCKER_IMAGE = image_reference("docker-in-docker")
DOCKER_REGISTRY_IMAGE = image_reference("docker-registry")
TUNNEL_MANAGER_IMAGE = image_reference("tunnel-manager")
IMAGE_CACHE_IMAGE = image_reference("image-cache")
ASSETS_SERVER_IMAGE = image_reference("assets-server")

BASE_ENVIRONMENT_IMAGE = image_reference("base-environment")
JDK8_ENVIRONMENT_IMAGE = image_reference("jdk8-environment")
JDK11_ENVIRONMENT_IMAGE = image_reference("jdk11-environment")
JDK17_ENVIRONMENT_IMAGE = image_reference("jdk17-environment")
JDK21_ENVIRONMENT_IMAGE = image_reference("jdk21-environment")
CONDA_ENVIRONMENT_IMAGE = image_reference("conda-environment")

workshop_images_table = {
    "base-environment:*": BASE_ENVIRONMENT_IMAGE,
    "jdk8-environment:*": JDK8_ENVIRONMENT_IMAGE,
    "jdk11-environment:*": JDK11_ENVIRONMENT_IMAGE,
    "jdk17-environment:*": JDK17_ENVIRONMENT_IMAGE,
    "jdk21-environment:*": JDK21_ENVIRONMENT_IMAGE,
    "conda-environment:*": CONDA_ENVIRONMENT_IMAGE,
}

LOFTSH_KUBERNETES_V1_31_IMAGE = image_reference("loftsh-kubernetes-v1.31")
LOFTSH_KUBERNETES_V1_32_IMAGE = image_reference("loftsh-kubernetes-v1.32")
LOFTSH_KUBERNETES_V1_33_IMAGE = image_reference("loftsh-kubernetes-v1.33")
LOFTSH_KUBERNETES_V1_34_IMAGE = image_reference("loftsh-kubernetes-v1.34")

LOFTSH_VCLUSTER_IMAGE = image_reference("loftsh-vcluster")

CONTOUR_BUNDLE_IMAGE = image_reference("contour-bundle")


def resolve_workshop_image(name):
    if name in workshop_images_table:
        return workshop_images_table[name]
    return name.replace("$(image_repository)", IMAGE_REPOSITORY)


for name, value in sorted(globals().items()):
    if name.isupper():
        logger.info(f"{name}: {repr(value)}")
