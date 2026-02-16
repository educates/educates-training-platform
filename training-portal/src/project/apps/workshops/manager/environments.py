"""Defines basic functions for managing creation of workshop environments.

"""

import traceback
import logging
import hashlib
import copy

from datetime import timedelta
from operator import itemgetter

import kopf
import pykube

from django.db import transaction
from django.conf import settings
from django.utils import timezone

from .resources import ResourceBody
from .operator import background_task
from .locking import resources_lock
from .sessions import (
    update_session_status,
    setup_workshop_session,
    create_workshop_session,
)
from .analytics import report_analytics_event

from ..models import TrainingPortal, Environment, Workshop

logger = logging.getLogger("educates")

api = pykube.HTTPClient(pykube.KubeConfig.from_env())


def convert_duration_to_seconds(size):
    """Converts a specification of a time duration with units to seconds."""

    multipliers = {
        "s": 1,
        "m": 60,
        "h": 60 * 60,
    }

    size = str(size)

    for suffix in multipliers:
        if size.lower().endswith(suffix):
            return int(size[0 : -len(suffix)]) * multipliers[suffix]

    try:
        return int(size)
    except ValueError as exception:
        raise RuntimeError(
            '"%s" is not a time duration. Must be an integer or a string with suffix s, m or h.'
            % size
        ) from exception


def duration_as_timedelta(duration):
    """Converts a specification of a time duration with units to a timedelta."""

    return timedelta(seconds=max(0, convert_duration_to_seconds(duration)))


def update_environment_status(name, phase):
    """Update the status of the Kubernetes resource object for the workshop
    environment.

    """

    try:
        K8SWorkshopEnvironment = pykube.object_factory(
            api,
            "training.educates.dev/v1beta1",
            "WorkshopEnvironment",
        )

        resource = K8SWorkshopEnvironment.objects(api).get(name=name)

        # Can't update status if deployment had stalled due to the workshop
        # definition not existing.

        if (
            resource.obj.get("status", {})
            .get("educates", {})
            .get("phase")
        ):
            resource.obj["status"]["educates"]["phase"] = phase
            resource.update()

        logger.info("Updated status of workshop environment %s to %s.", name, phase)

    except pykube.exceptions.ObjectDoesNotExist:
        pass

    except pykube.exceptions.PyKubeError:
        logger.exception("Failed to update status of workshop environment %s.", name)


@background_task
@resources_lock
@transaction.atomic
def activate_workshop_environment(resource):
    """Updates workshop details of a workshop environment and marks the
    workshop environment as being running.

    """

    # Lookup with training portal the workshop environment is associated with.
    # In this case since starting with the workshop environment resource, need
    # to get that from the label added to the resource by the operator.

    try:
        portal = TrainingPortal.objects.get(
            name=resource.metadata.labels.get(
                "training.educates.dev/portal.name", ""
            )
        )
    except TrainingPortal.DoesNotExist:
        return

    # The name of the resource is the name of the workshop environment so see
    # if we have a record of such a workshop environment.

    environment = portal.workshop_environment(resource.name)

    if not environment:
        return

    # Double check that has matching uid and not somehow getting event
    # notification for different instance.

    if environment.uid != resource.metadata.uid:
        return

    # Validate that the record of the workshop environment is that it is
    # starting otherwise need to ignore the event.

    if not environment.is_starting():
        return

    # Retrieve the details for the workshop definition and ensure we have a
    # record of the current version of it.

    details = resource.status.get("educates.workshop")

    workshop, created = Workshop.objects.get_or_create(
        name=details.get("name"),
        uid=details.get("uid"),
        generation=details.get("generation"),
    )

    logger.info(
        "Activate workshop environment %s for workshop %s, uid %s, generation %s.",
        environment.name,
        workshop.name,
        workshop.uid,
        workshop.generation,
    )

    labels = {}

    for item in details.get("spec.labels", []).obj():
        labels[item["name"]] = item.get("value", "")

    if created:
        workshop.title = details.get("spec.title", "")
        workshop.description = details.get("spec.description", "")
        workshop.vendor = details.get("spec.vendor", "")
        workshop.authors = details.get("spec.authors", []).obj()
        workshop.difficulty = details.get("spec.difficulty", "")
        workshop.duration = details.get("spec.duration", "")
        workshop.tags = details.get("spec.tags", []).obj()
        workshop.labels = labels
        workshop.logo = details.get("spec.logo", "")
        workshop.url = details.get("spec.url", "")
        workshop.params = details.get("spec.request.parameters", []).obj()

        workshop.ingresses = details.get("spec.session.ingresses", []).obj()

    workshop.save()

    # Attach the record of the workshop details to the workshop environment
    # and mark the workshop environment as running. The call to save it is
    # redundant since done when mark it as running, but include it anyway for
    # clarity.

    environment.workshop = workshop

    environment.mark_as_running()

    environment.save()

    # Since this is a newly created workshop environment, we need to trigger
    # the creation of any initial reserved workshop sessions. We need to make
    # sure we don't go over any capacity cap for the training portal as a
    # whole.

    sessions = []

    maximum = portal.sessions_maximum

    if maximum == 0:
        maximum = environment.initial
    else:
        maximum -= portal.active_sessions_count()

    required = min(environment.initial, maximum)

    for _ in range(required):
        sessions.append(setup_workshop_session(environment))

    def _schedule_session_creation():
        if sessions:
            logger.info(
                "Schedule creation of %d new reserved workshop sessions for workshop environment %s.",
                len(sessions),
                environment.name,
            )

            for session, secret in sessions:
                create_workshop_session(session, secret)

    transaction.on_commit(_schedule_session_creation)


@kopf.on.event(
    "training.educates.dev",
    "v1beta1",
    "workshopenvironments",
    when=lambda event, labels, **_: event["type"] in (None, "ADDED", "MODIFIED")
    and labels.get("training.educates.dev/portal.name", "")
    == settings.PORTAL_NAME,
)
def workshop_environment_event(
    event, meta, body, **_
):  # pylint: disable=unused-argument
    """This is the entrypoint for handling event notifications for the
    WorkshopEnvironment resource. We watch for these so we know when the
    details of a workshop are added to the status of the workshop environment
    resource signalling that the workshop environment has been created. When
    this is seen, use that to progress the state of the workshop environment
    to running.

    """

    name = meta["name"]
    uid = meta["uid"]
    generation = meta["generation"]

    logger.info(
        "Processing workshop environment %s, uid %s, generation %s.",
        name,
        uid,
        generation,
    )

    # Wrap up body of the resource to make it easier to work with later.

    resource = ResourceBody(body)

    # Check whether the workshop environment status has been updated with the
    # workshop specification.

    if not resource.status.get("educates.workshop.uid"):
        logger.info("Workshop environment %s not yet ready to be used.", name)

        return

    # Activate the workshop environment, setting status to running if we can.

    activate_workshop_environment(resource).schedule()


def shutdown_workshop_environments(training_portal, workshops):
    """Mark for deletion any workshop environments which are no longer
    included in the list of workshops for the training portal.

    """

    # When looking for workshop environments to delete, we compare using a key
    # which is the workshop name and resource name. The resource name is always
    # the name of the corresponding Workshop resource. The workshop name is
    # either the name of the workshop or the alias if it has one. By checking a
    # combination of both we ensure we delete a workshop environment where the
    # linked Workshop resource name has changed but the alias hasn't.

    workshop_names = set()

    for workshop in workshops:
        workshop_name = workshop.get("alias", "") or workshop["name"]
        resource_name = workshop["name"]

        workshop_key = f"{workshop_name}:{resource_name}"

        workshop_names.add(workshop_key)

    environments = training_portal.active_environments()

    for environment in environments:
        workshop_key = f"{environment.workshop_name}:{environment.resource_name}"

        if workshop_key not in workshop_names:
            # Mark the workshop environment as stopping. Next mark as stopping
            # any workshop sessions which were being kept in reserve for the
            # workshop environment so that they are deleted. We mark the
            # workshop environment as stopping first so that capacity and
            # reserved counts are set to zero and replacements aren't created.
            # The actual workshop environment as a whole will only be deleted
            # when the number of active sessions reaches zero. If there were
            # allocated workshop sessions, that will only be when they expire.

            if environment.workshop:
                logger.info(
                    "Stopping workshop environment %s for workshop %s, uid %s, generation %s.",
                    environment.name,
                    environment.workshop.name,
                    environment.workshop.uid,
                    environment.workshop.generation,
                )
            else:
                logger.info("Stopping workshop environment %s.", environment.name)

            update_environment_status(environment.name, "Stopping")
            environment.mark_as_stopping()
            report_analytics_event(environment, "Environment/Terminate")

            for session in environment.available_sessions():
                update_session_status(session.name, "Stopping")
                session.mark_as_stopping()
                report_analytics_event(session, "Session/Terminate")


@background_task
@resources_lock
def delete_workshop_environment(environment):
    """Deletes a workshop environment. If this is called when there are still
    workshop sessions, they will be forcibly deleted.

    """

    K8SWorkshopEnvironment = pykube.object_factory(
        api, "training.educates.dev/v1beta1", "WorkshopEnvironment"
    )

    try:
        resource = K8SWorkshopEnvironment.objects(api).get(name=environment.name)
        resource.delete()

        logger.info("Deleted workshop environment %s.", environment.name)

    except pykube.exceptions.ObjectDoesNotExist:
        pass

    except pykube.exceptions.PyKubeError:
        logger.exception("Failed to delete workshop environment %s.", environment.name)


@background_task
@resources_lock
@transaction.atomic
def refresh_workshop_environments(training_portal):
    """Looks for workshop environments which have a refresh interval and if that
    interval has been exceeded shutdown the old workshop environment and create
    a new in it's place using the same workshop definition.

    """

    for environment in training_portal.running_environments():
        if environment.refresh.total_seconds() != 0:
            duration = timezone.now() - environment.created_at

            if duration.total_seconds() > environment.refresh.total_seconds():
                logger.info(
                    "Trigger periodic refresh of workshop environment %s.",
                    environment.name,
                )

                replace_workshop_environment(environment)


@background_task
@resources_lock
@transaction.atomic
def delete_workshop_environments(training_portal):
    """Looks for workshop environments which are marked as stopping and if
    the number of active workshop sessions has reached zero, the workshop
    environment can safely be deleted without interrupting any users.

    """

    for environment in training_portal.stopping_environments():
        if environment.active_sessions_count() == 0:
            logger.info(
                "Trigger deletion of workshop environment %s.", environment.name
            )

            delete_workshop_environment(environment).schedule()
            environment.mark_as_stopped()
            report_analytics_event(environment, "Environment/Deleted")


def update_workshop_environments(training_portal, workshops):
    """Updates configuration of any workshops which already exist."""

    for position, workshop in enumerate(workshops, 1):
        workshop_name = workshop.get("alias", "") or workshop["name"]

        environment = training_portal.environment_for_workshop(workshop_name)

        if environment:
            labels = {}

            for item in workshop["labels"]:
                labels[item["name"]] = item.get("value", "")

            environment.labels = labels

            environment.env = workshop["env"]

            environment.capacity = workshop["capacity"]
            environment.reserved = workshop["reserved"]

            environment.expires = duration_as_timedelta(workshop["expires"])
            environment.overtime = duration_as_timedelta(workshop["overtime"])
            environment.deadline = duration_as_timedelta(workshop["deadline"])
            environment.orphaned = duration_as_timedelta(workshop["orphaned"])
            environment.overdue = duration_as_timedelta(workshop["overdue"])
            environment.refresh = duration_as_timedelta(workshop["refresh"])

            # Only update initial reserved session count if the workshop
            # environment hasn't actually been provisioned yet.

            if environment.is_starting():
                environment.initial = workshop["initial"]

            environment.position = position

            environment.save()

            update_environment_status_details(
                environment.name, environment.capacity, environment.reserved
            )


@background_task
@resources_lock
@transaction.atomic
def process_workshop_environment(portal, workshop, position):
    """Creates the workshop environment if necessary, both in the database
    and in Kubernetes.

    """

    # First see if there is already a workshop environment for the workshop.
    # If there is we don't want to be creating a second one. The name used to
    # identify the workshop is the resource name by default, but if the workshop
    # has an alias, then that is used instead.

    workshop_name = workshop.get("alias", "") or workshop["name"]

    environment = portal.environment_for_workshop(workshop_name)

    if environment:
        return

    # Create initial record for the workshop environment in the database.

    environment_expires = duration_as_timedelta(workshop["expires"])
    environment_overtime = duration_as_timedelta(workshop["overtime"])
    environment_deadline = duration_as_timedelta(workshop["deadline"])
    environment_orphaned = duration_as_timedelta(workshop["orphaned"])
    environment_overdue = duration_as_timedelta(workshop["overdue"])
    environment_refresh = duration_as_timedelta(workshop["refresh"])

    if environment_deadline < environment_expires:
        environment_deadline = environment_expires

    labels = {}

    for item in workshop["labels"]:
        labels[item["name"]] = item.get("value", "")

    environment = Environment(
        portal=portal,
        workshop_name=workshop_name,
        resource_name=workshop["name"],
        position=position,
        capacity=workshop["capacity"],
        initial=workshop["initial"],
        reserved=workshop["reserved"],
        expires=environment_expires,
        overtime=environment_overtime,
        deadline=environment_deadline,
        orphaned=environment_orphaned,
        overdue=environment_overdue,
        refresh=environment_refresh,
        registry=workshop["registry"],
        env=workshop["env"],
        labels=labels,
    )

    # Save it so that the database record ID is allocated as we use that in
    # the name of the workshop environment. A further save will be done later.

    environment.save()

    environment.name = f"{portal.name}-w{environment.id:02}"

    logger.info(
        "Creating workshop environment %s for workshop %s.",
        environment.name,
        workshop["name"],
    )

    # Create the workshop environment resource to deploy it.

    environment_body = {
        "apiVersion": "training.educates.dev/v1beta1",
        "kind": "WorkshopEnvironment",
        "metadata": {
            "name": environment.name,
            "labels": {
                "training.educates.dev/portal.name": portal.name,
                "training.educates.dev/portal.uid": portal.uid,
            },
            "ownerReferences": [
                {
                    "apiVersion": "training.educates.dev/v1beta1",
                    "kind": "TrainingPortal",
                    "blockOwnerDeletion": True,
                    "controller": True,
                    "name": portal.name,
                    "uid": portal.uid,
                }
            ],
        },
        "spec": {
            "workshop": {"name": workshop["name"]},
            "request": {"enabled": False},
            "session": {
                "ingress": {
                    "domain": settings.INGRESS_DOMAIN,
                    "secret": settings.INGRESS_SECRET,
                    "class": settings.INGRESS_CLASS,
                },
            },
            "environment": {"objects": [], "secrets": []},
            "registry": environment.registry or None,
            "theme": {"name": settings.THEME_NAME},
            "cookies": {"domain": settings.SESSION_COOKIE_DOMAIN},
        },
        "status": {
            "educates": {
                "capacity": environment.capacity,
                "initial": environment.initial,
                "reserved": environment.reserved,
            },
        },
    }

    if settings.GOOGLE_TRACKING_ID is not None:
        environment_body["spec"]["analytics"] = {
            "google": {"trackingId": settings.GOOGLE_TRACKING_ID}
        }

    if settings.CLARITY_TRACKING_ID is not None:
        environment_body["spec"]["analytics"] = {
            "clarity": {"trackingId": settings.CLARITY_TRACKING_ID}
        }

    if settings.AMPLITUDE_TRACKING_ID is not None:
        environment_body["spec"]["analytics"] = {
            "amplitude": {"trackingId": settings.AMPLITUDE_TRACKING_ID}
        }

    # Query back the workshop environment resource so we can retrieve the uid
    # assigned to it by Kubernetes. We will use that to validate events which
    # are received for the workshop environment resource are in fact for the
    # instance we created.

    K8SWorkshopEnvironment = pykube.object_factory(
        api, "training.educates.dev/v1beta1", "WorkshopEnvironment"
    )

    resource = K8SWorkshopEnvironment(api, environment_body)
    resource.create()

    environment.uid = resource.metadata["uid"]

    logger.info(
        "Successfully created workshop environment %s for workshop %s with uid %s.",
        environment.name,
        workshop["name"],
        resource.metadata["uid"],
    )

    # Finally save the record again. The workshop environment is left in
    # starting state. It will only be progressed to running state when we
    # receive an event for the workshop environment being modified by the
    # operator with the workshop specification filled out in it.

    environment.save()

    report_analytics_event(environment, "Environment/Created")


def initiate_workshop_environments(portal, workshops):
    """Initiate creation of any workshop environments which don't already
    exist in the list of workshops for the training portal.

    """

    # Process each workshop separately as subsequent task so that errors
    # only affect the one workshop.

    for position, workshop in enumerate(workshops, 1):
        process_workshop_environment(portal, workshop, position).schedule()


def replace_workshop_environment(environment):
    # We need to fake up the workshop entry from the training portal based on
    # the existing workshop environment. We will use the same index position
    # for the workshop entry in the training portal. Do this before marking
    # the existing one as stopping as it clears various values.

    workshop = {
        "name": environment.resource_name,
        "alias": environment.workshop_name,
        "capacity": environment.capacity,
        "initial": environment.initial,
        "reserved": environment.reserved,
        "expires": int(environment.expires.total_seconds()),
        "overtime": int(environment.overtime.total_seconds()),
        "deadline": int(environment.deadline.total_seconds()),
        "orphaned": int(environment.orphaned.total_seconds()),
        "overdue": int(environment.overdue.total_seconds()),
        "refresh": int(environment.refresh.total_seconds()),
        "registry": environment.registry,
        "env": environment.env,
        "labels": environment.labels,
    }

    position = environment.position

    # Mark the workshop environment as stopping. Next mark as stopping
    # any workshop sessions which were being kept in reserve for the
    # workshop environment so that they are deleted. We mark the
    # workshop environment as stopping first so that capacity and
    # reserved counts are set to zero and replacements aren't created.
    # The actual workshop environment as a whole will only be deleted
    # when the number of active sessions reaches zero. If there were
    # allocated workshop sessions, that will only be when they expire.

    if environment.workshop:
        logger.info(
            "Stopping workshop environment %s for workshop %s, uid %s, generation %s.",
            environment.name,
            environment.workshop_name,
            environment.workshop.uid,
            environment.workshop.generation,
        )
    else:
        logger.info(
            "Stopping workshop environment %s for workshop %s.",
            environment.name,
            environment.workshop_name,
        )

    update_environment_status(environment.name, "Stopping")
    environment.mark_as_stopping()
    report_analytics_event(environment, "Environment/Terminate")

    for session in environment.available_sessions():
        update_session_status(session.name, "Stopping")
        session.mark_as_stopping()
        report_analytics_event(session, "Session/Terminate")

    # Now schedule creation of the replacement workshop session.

    process_workshop_environment(environment.portal, workshop, position).schedule()


def update_environment_status_details(name, capacity, reserved):
    """Update the capacity for the workshop environment recorded in the status."""

    try:
        K8SWorkshopEnvironment = pykube.object_factory(
            api,
            "training.educates.dev/v1beta1",
            "WorkshopEnvironment",
        )

        resource = K8SWorkshopEnvironment.objects(api).get(name=name)

        # The status may not exist as yet if not processed by the operator.

        status = resource.obj.setdefault("status", {}).setdefault(
            "educates", {}
        )

        status["capacity"] = capacity
        status["reserved"] = reserved

        resource.update()

        logger.info(
            "Updated status of workshop environment %s with capacity=%s and reserved=%s.",
            name,
            capacity,
            reserved,
        )

    except pykube.exceptions.ObjectDoesNotExist:
        pass

    except pykube.exceptions.PyKubeError:
        logger.exception(
            "Failed to update status details of workshop environment %s.", name
        )
