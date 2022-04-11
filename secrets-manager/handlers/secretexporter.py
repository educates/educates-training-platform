import kopf

from .helpers import global_logger

from .functions_copier import global_configs, reconcile_config

from .config import OPERATOR_API_GROUP


@kopf.on.create(f"secrets.{OPERATOR_API_GROUP}", "v1alpha1", "secretexporters")
def secretexporter_create(name, body, logger, **_):
    global_configs[(name, body["kind"])] = body

    with global_logger(logger):
        reconcile_config(name, body)


@kopf.on.resume(f"secrets.{OPERATOR_API_GROUP}", "v1alpha1", "secretexporters")
def secretexporter_resume(name, body, logger, **_):
    global_configs[(name, body["kind"])] = body

    with global_logger(logger):
        reconcile_config(name, body)


@kopf.on.update(f"secrets.{OPERATOR_API_GROUP}", "v1alpha1", "secretexporters")
def secretexporter_update(name, body, logger, **_):
    global_configs[(name, body["kind"])] = body

    with global_logger(logger):
        reconcile_config(name, body)


@kopf.timer(
    f"secrets.{OPERATOR_API_GROUP}", "v1alpha1", "secretexporters", interval=60.0
)
def secretexporter_reconcile(name, body, logger, **_):
    global_configs[(name, body["kind"])] = body

    with global_logger(logger):
        reconcile_config(name, body)


@kopf.on.delete(f"secrets.{OPERATOR_API_GROUP}", "v1alpha1", "secretexporters")
def secretexporter_delete(name, body, **_):
    try:
        del global_configs[(name, body["kind"])]
    except KeyError:
        pass
