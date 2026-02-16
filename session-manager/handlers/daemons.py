import logging
import asyncio

import pykube

from datetime import datetime, timedelta, timezone
from functools import lru_cache

api = pykube.HTTPClient(pykube.KubeConfig.from_env())

_polling_interval = 60
_resource_timeout = 90

logger = logging.getLogger("educates")


def get_overdue_terminating_namespaces(timeout=_resource_timeout):
    label_set = [
        "training.educates.dev/component",
        "training.educates.dev/portal.name",
    ]

    selector = ",".join(label_set)

    now = datetime.now(timezone.utc)

    for namespace_item in pykube.Namespace.objects(api).filter(selector=selector):
        if namespace_item.obj["status"]["phase"] == "Terminating":
            if namespace_item.metadata.get("deletionTimestamp"):
                timestamp = namespace_item.metadata["deletionTimestamp"]
                when = datetime.strptime(timestamp, "%Y-%m-%dT%H:%M:%S%z")

                if now - when > timedelta(seconds=timeout):
                    yield namespace_item.name


def api_groups():
    return api.session.get(url=api.url + "/apis").json()["groups"]


def get_all_api_versions():
    # Need to first yield up standard Kubernetes api group.

    yield "v1"

    # Now yield up all additional api groups.

    for group in api_groups():
        name = group["name"]
        for version in group["versions"]:
            yield version["groupVersion"]


@lru_cache(1)
def get_all_namespaced_resources():
    resource_objects = {}

    for api_version in get_all_api_versions():
        # We need to catch an error here as a possibly bogus custom resource
        # definition may result in the Kubernetes API not actually existing.
        # For example "404 Client Error: Not Found for url:
        # https://A.B.C.D:443/apis/data.packaging.carvel.dev/v1alpha1/".
        # Not sure of the root cause just yet. So just ignore an error here
        # and move on to the next API rather than failing completely.

        try:
            resources = api.resource_list(api_version)
        except Exception as e:
            logger.warning(f"Cannot query resources for {api_version} {e}.")
            continue

        api_version = resources["groupVersion"]

        for resource in resources["resources"]:
            if (
                resource["namespaced"]
                and "get" in resource["verbs"]
                and "/" not in resource["name"]
            ):
                kind = resource["kind"]
                resource_type = pykube.object_factory(api, api_version, kind)
                resource_objects[(api_version, kind)] = resource_type

    return resource_objects


def purge_terminated_resources(namespace):
    logger.info(f"Attempting to purge namespace {namespace}.")
    for resource_type in get_all_namespaced_resources().values():
        for resource in resource_type.objects(api, namespace=namespace).all():
            if resource.metadata.get("deletionTimestamp"):
                if resource.metadata.get("finalizers"):
                    try:
                        logger.info(f"Forcibly deleting finalizers on {resource.obj}.")
                        resource.metadata["finalizers"] = None
                        resource.update()
                    except pykube.exceptions.KubernetesError as exc:
                        if exc.code != 404:
                            logger.error(
                                f"Could not delete finalizers on {resource.obj}."
                            )


async def purge_namespaces():
    while True:
        try:
            logger.debug("Checking whether namespaces need purging.")
            for namespace in get_overdue_terminating_namespaces():
                purge_terminated_resources(namespace)

        except Exception:
            logger.exception("Unexpected error occurred when purging namespaces.")

        await asyncio.sleep(_polling_interval)
