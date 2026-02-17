import os
import yaml
import logging

from .helpers import lookup

logger = logging.getLogger("educates")

config_values = {}

if os.path.exists("/opt/app-root/config/educates-operator-config.yaml"):
    with open("/opt/app-root/config/educates-operator-config.yaml") as fp:
        config_values = yaml.load(fp, Loader=yaml.Loader)

OPERATOR_NAMESPACE = lookup(config_values, "operator.namespace", "educates")

if os.path.exists("/var/run/secrets/kubernetes.io/serviceaccount/namespace"):
    with open("/var/run/secrets/kubernetes.io/serviceaccount/namespace") as fp:
        OPERATOR_NAMESPACE = fp.read().strip()
