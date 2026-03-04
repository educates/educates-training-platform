import logging

import kopf

from .secretinjector_funcs import reconcile_config


logger = logging.getLogger("educates")


@kopf.index("secrets.educates.dev", "v1beta1", "secretinjectors")
def secretinjector_index(name, meta, body, **_):
    generation = meta["generation"]

    logger.debug("Add secretinjector %s with generation %s to cache.", name, generation)

    return {(None, name): body}


@kopf.on.resume("secrets.educates.dev", "v1beta1", "secretinjectors")
def secretinjector_reconcile_resume(name, meta, body, **_):
    generation = meta["generation"]

    logger.info("Secretinjector %s exists with generation %s.", name, generation)

    reconcile_config(name, body)


@kopf.on.create("secrets.educates.dev", "v1beta1", "secretinjectors")
@kopf.on.update("secrets.educates.dev", "v1beta1", "secretinjectors")
def secretinjector_reconcile_update(name, meta, body, reason, **_):
    generation = meta["generation"]

    logger.info(
        "Secretinjector %s %sd with generation %s.", name, reason.lower(), generation
    )

    reconcile_config(name, body)


@kopf.timer(
    "secrets.educates.dev",
    "v1beta1",
    "secretinjectors",
    initial_delay=30.0,
    interval=60.0,
)
def secretinjector_reconcile_timer(name, meta, body, **_):
    generation = meta["generation"]

    logger.debug("Reconcile secretinjector %s with generation %s.", name, generation)

    reconcile_config(name, body)


@kopf.on.delete(
    "secrets.educates.dev", "v1beta1", "secretinjectors", optional=True
)
def secretinjector_delete(name, meta, **_):
    generation = meta["generation"]

    logger.info("Secretinjector %s with generation %s deleted.", name, generation)
