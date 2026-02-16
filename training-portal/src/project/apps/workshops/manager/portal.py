"""Defines handlers for the TrainingPortal resource type. The whole structure
of the application is actually setup to be able to handle multiple training
portal instances, but we gate on just the name of the one that this specific
process instance is for.

"""

import copy
import logging

import kopf

from django.db import transaction
from django.conf import settings
from django.contrib.auth.models import Group
from django.contrib.auth import get_user_model

from oauth2_provider.models import Application, clear_expired

from ..models import TrainingPortal

from .resources import ResourceBody
from .operator import background_task, initialize_kopf
from .locking import resources_lock
from .environments import (
    update_workshop_environments,
    initiate_workshop_environments,
    shutdown_workshop_environments,
    delete_workshop_environments,
    refresh_workshop_environments,
    update_environment_status,
    process_workshop_environment,
    replace_workshop_environment,
)
from .sessions import (
    initiate_reserved_sessions,
    terminate_reserved_sessions,
    update_session_status,
)
from .cleanup import cleanup_old_sessions_and_users, purge_expired_workshop_sessions
from .analytics import report_analytics_event

logger = logging.getLogger("educates")


# XXX Disabled here as being handled in parent training_portal_event().
# @resources_lock
# @transaction.atomic
def initialize_robot_account(resource):
    """Initialize the robot user account if this is the first time the
    training portal has been started up.

    """

    status = resource.status.get("educates")

    robot_username = status.get("credentials.robot.username")
    robot_password = status.get("credentials.robot.password")

    robot_client_id = status.get("clients.robot.id")
    robot_client_secret = status.get("clients.robot.secret")

    # Check whether the OAuth application for the robot user already
    # exists. If it exists the robot user has already been created and we
    # don't need to do anything.

    try:
        Application.objects.get(name="robot@educates")
    except Application.DoesNotExist:
        pass
    else:
        return

    # Create the robot user and add it to a robots group.

    User = get_user_model()  # pylint: disable=invalid-name

    user = User.objects.create_user(robot_username, password=robot_password)

    group, _ = Group.objects.get_or_create(name="robots")
    user.groups.add(group)
    user.save()

    # Create the application object for OAuth access.

    Application.objects.get_or_create(
        name="robot@educates",
        client_id=robot_client_id,
        user=user,
        client_type="public",
        authorization_grant_type="password",
        client_secret=robot_client_secret,
    )


def workshop_configuration(portal, workshop):
    """Returns santized configuration for single workshop from the training
    portal resource definition. Any fields which were not set will be filled
    out with defaults.

    """

    workshop = copy.deepcopy(workshop)

    if workshop.get("capacity") is None:
        workshop["capacity"] = portal.default_capacity

    if workshop.get("reserved") is None:
        if portal.default_reserved is not None:
            workshop["reserved"] = portal.default_reserved
        else:
            workshop["reserved"] = 1

    if workshop.get("initial") is None:
        if portal.default_initial is not None:
            workshop["initial"] = portal.default_initial
        else:
            workshop["initial"] = workshop["reserved"]

    workshop["capacity"] = max(0, workshop["capacity"])
    workshop["reserved"] = max(0, min(workshop["reserved"], workshop["capacity"]))

    workshop["initial"] = max(0, min(workshop["initial"], workshop["capacity"]))

    # Initial sessions count of zero is special and results in any reserved
    # sessions only being created once the first session is requested. So if
    # set to zero leave it alone.

    if workshop["initial"] != 0:
        # Note that it is okay that initial is greater than reserved. In this
        # case application of reserved limit will only be applied after those
        # initial sessions has been used up.

        workshop["initial"] = max(workshop["initial"], workshop["reserved"])

    workshop.setdefault("expires", portal.default_expires)
    workshop.setdefault("overtime", portal.default_overtime)
    workshop.setdefault("deadline", portal.default_deadline)
    workshop.setdefault("orphaned", portal.default_orphaned)
    workshop.setdefault("overdue", portal.default_overdue)
    workshop.setdefault("refresh", portal.default_refresh)

    if workshop["deadline"] == "0":
        workshop["deadline"] = workshop["expires"]

    workshop.setdefault("registry", portal.default_registry)

    workshop.setdefault("labels", {})

    # Need to merge environment settings and can't just let workshop specific
    # list override the default list of environment variables.

    env_variables = {}

    workshop.setdefault("env", [])

    for item in workshop["env"]:
        env_variables[item["name"]] = item.get("value", None)

    for item in portal.default_env:
        if item["name"] not in env_variables:
            workshop["env"].append(
                {"name": item["name"], "value": item.get("value", "")}
            )

    return workshop


def workshops_configuration(portal, resource):
    """Returns santized configuration for the workshops from the training
    portal resource definition. Any fields which were not set will be filled
    out with defaults.

    """

    workshops = []

    if resource.spec.get("workshops"):
        for workshop in resource.spec.get("workshops").obj():
            workshops.append(workshop_configuration(portal, workshop))

    return workshops


# XXX Disabled here as being handled in parent training_portal_event().
# @resources_lock
# @transaction.atomic
def process_training_portal(resource):
    """Process the training portal configuration, creating or updating the set
    of workshop environments, and creating or deleting reserved sessions.

    """

    # Grab the configuration for the training portal of the same name,
    # creating it if this is the first time it has been seen.

    metadata = resource.metadata

    portal, created = TrainingPortal.objects.get_or_create(name=metadata.name)

    # If the uid does not match then return straight away. This should never
    # occur in practice because the instance of the training portal should be
    # deleted automatically if the custom resource for it is deleted as it
    # will be a child of the custom resource.

    if not created and portal.uid != metadata.uid:
        return

    # Save a copy of the labels. We need to massage them into a dictionary.

    spec = resource.spec

    labels = {}

    for item in spec.get("portal.labels", []).obj():
        labels[item["name"]] = item.get("value", "")

    portal.labels = labels

    # Ensure that the record of the uid and generation fields are up to date.

    portal.uid = metadata.uid
    portal.generation = metadata.generation

    # Update the database record for this version of the global configuration
    # settings. Note that the global defaults only come into play when a new
    # workshop environment is first created. They are not applied
    # restrospectively to existing workshop environments which had relied on
    # the global defaults when they were first created.

    sessions_maximum = spec.get("portal.sessions.maximum", 0)
    sessions_registered = spec.get("portal.sessions.registered", 0)
    sessions_anonymous = spec.get("portal.sessions.anonymous", sessions_registered)

    portal.sessions_maximum = sessions_maximum
    portal.sessions_registered = sessions_registered
    portal.sessions_anonymous = sessions_anonymous

    # Workshop default settings now scoped under portal.workshop.defaults
    # and old settings deprecated. Give precedence to new settings.

    default_labels = {}

    default_capacity = sessions_maximum
    default_reserved = None
    default_initial = None
    default_expires = "0"
    default_overtime = "0"
    default_deadline = "0"
    default_orphaned = "0"
    default_overdue = "0"
    default_refresh = "0"

    default_capacity = spec.get("portal.capacity", default_capacity)
    default_reserved = spec.get("portal.reserved", default_reserved)
    default_initial = spec.get("portal.initial", default_initial)
    default_expires = spec.get("portal.expires", default_expires)
    default_orphaned = spec.get("portal.orphaned", default_orphaned)
    default_overdue = spec.get("portal.overdue", default_overdue)
    default_refresh = spec.get("portal.refresh", default_refresh)

    for item in spec.get("portal.workshop.defaults.labels", []).obj():
        default_labels[item["name"]] = item.get("value", "")

    default_capacity = spec.get("portal.workshop.defaults.capacity", default_capacity)
    default_reserved = spec.get("portal.workshop.defaults.reserved", default_reserved)
    default_initial = spec.get("portal.workshop.defaults.initial", default_initial)
    default_expires = spec.get("portal.workshop.defaults.expires", default_expires)
    default_overtime = spec.get("portal.workshop.defaults.overtime", default_overtime)
    default_deadline = spec.get("portal.workshop.defaults.deadline", default_deadline)
    default_orphaned = spec.get("portal.workshop.defaults.orphaned", default_orphaned)
    default_overdue = spec.get("portal.workshop.defaults.overdue", default_overdue)
    default_refresh = spec.get("portal.workshop.defaults.refresh", default_refresh)

    portal.default_labels = default_labels

    portal.default_capacity = default_capacity
    portal.default_reserved = default_reserved
    portal.default_initial = default_initial
    portal.default_expires = default_expires
    portal.default_overtime = default_overtime
    portal.default_deadline = default_deadline
    portal.default_orphaned = default_orphaned
    portal.default_overdue = default_overdue
    portal.default_refresh = default_refresh

    portal.default_registry = dict(spec.get("portal.workshop.defaults.registry", {}))

    env_variables = []

    for item in spec.get("portal.workshop.defaults.env", []):
        env_variables.append({"name": item["name"], "value": item.get("value", "")})

    portal.default_env = env_variables

    update_workshop = spec.get("portal.updates.workshop", False)

    portal.update_workshop = update_workshop

    portal.save()

    # Calculate the list of workshops, filling in any configuration defaults.

    workshops = workshops_configuration(portal, resource)

    # Mark for deletion any workshop environments which are no longer included
    # in the list of workshops for the training portal.

    shutdown_workshop_environments(portal, workshops)

    # Update configuration of any workshop environments which already exist.

    update_workshop_environments(portal, workshops)

    # Initiate creation of any workshop environments which don't already
    # exist.

    initiate_workshop_environments(portal, workshops)


@background_task(delay=60*60, repeat=True)
@resources_lock
@transaction.atomic
def start_hourly_cleanup_task():
    """Hourly cleanup job."""

    # Clear expired access tokens for OAuth.

    clear_expired()


@background_task(delay=15.0, repeat=True)
@resources_lock
@transaction.atomic
def start_reconciliation_task(name):
    """Periodic reconcilliation task which ensures current deployments of
    workshop environments and workshop sessions matches desired configuration.

    """

    # Need to guard against the training portal configuration not having been
    # read in as yet. This should only arise if there is a serious issues with
    # updates to resources not being prompt.

    try:
        portal = TrainingPortal.objects.get(name=name)
    except TrainingPortal.DoesNotExist:
        return

    # Queue further task to look for workshop environments that need to be
    # deleted as removed from training portal workshop list.

    delete_workshop_environments(portal).schedule()

    # Queue further task to look for reserved workshop sessions that need to
    # be deleted as required reserved sessions or capacity of workshop
    # environment or training portal was changed.

    terminate_reserved_sessions(portal).schedule()

    # Queue further task to look for workshop environments that should be
    # retired and replaced with a new one.

    refresh_workshop_environments(portal).schedule()

    # Queue further task to look for where additional workshop sessions need
    # to be created in reserved as required reserved sessions or capacity of
    # workshop environment or training portal was changed.

    initiate_reserved_sessions(portal).schedule()

    purge_expired_workshop_sessions().schedule()

    cleanup_old_sessions_and_users().schedule()


@kopf.on.event(
    "training.educates.dev",
    "v1beta1",
    "trainingportals",
    when=lambda event, name, uid, annotations, **_: name == settings.PORTAL_NAME
    and uid == settings.PORTAL_UID
    and event["type"] in (None, "ADDED", "MODIFIED"),
)
@resources_lock
@transaction.atomic
def training_portal_event(event, name, body, **_):
    """This is the key entry point for handling any changes to the
    TrainingPortal resource for the instance of the training portal. It will
    be invoked when the process starts or if the resource is modified. It
    technically should not be invoked when the training portal instance is
    being deleted since the training portal instance in that case should be
    getting shutdown as it is owned by the TrainingPortal resource it
    corresponds to. Note that is gated on specific name of the training
    portal assigned to this process instance.

    """

    # Event type will be None in case that the process has just started up as
    # the training portal resource will always exist at that point. For this
    # case start a background task for performing various reconciliation tasks
    # for the training portal. Technically we should not ever see an "ADDED"
    # event type as the training portal resource should always exist when the
    # process starts up. Just in case, we also handle it here.

    if event["type"] in (None, "ADDED"):
        logger.info("Starting up training portal background tasks.")

        start_reconciliation_task(name).schedule()
        start_hourly_cleanup_task().schedule()

    # Wrap up body of the resource to make it easier to work with later.

    resource = ResourceBody(body)

    # Ensure that status has been filled out and we can start processing. If
    # it isn't, we should get a subsequent event with the changes.

    if not resource.status.get("educates"):
        return

    # If this is the first time the training portal has been started, we
    # need to setup the access for the robot account. Access for the admin
    # user was already done when the database was intialized.

    initialize_robot_account(resource)

    # Process the training portal configuration, creating or updating the set
    # of workshop environments, and creating or deleting reserved sessions.

    process_training_portal(resource)


@kopf.on.event(
    "training.educates.dev",
    "v1beta1",
    "workshops",
    when=lambda event, labels, **_: event["type"] in (None, "ADDED", "MODIFIED"),
)
@resources_lock
@transaction.atomic
def workshop_event(event, body, **_):  # pylint: disable=unused-argument
    """This entry point is for monitoring if Workshop definitions used by any
    workshop environment change. If automatic updates are enabled for changes
    to workshop definition, the existing workshop environment will be stopped
    and replaced with a new workshop environment using the new workshop
    definition. Note that removal of a workshop definition does not result in
    a workshop environment being removed.

    """

    # Wrap up body of the resource to make it easier to work with later.

    resource = ResourceBody(body)

    # Look up the training portal. During startup it may not be ready yet
    # so retry the operation after a delay.

    try:
        portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)
    except TrainingPortal.DoesNotExist:
        raise kopf.TemporaryError("Training portal is not yet ready.", delay=30)

    # Check whether updating workshop environments on changes to a workshop
    # definition is enabled.

    if not portal.update_workshop:
        return

    # Find any workshop environments which is starting up or running which
    # uses the workshop definition. Note that the workshop may not yet
    # have been linked to the environment so need to also check for that.

    # environment = portal.environment_for_workshop(resource.name)

    environments = portal.environments_for_resource(resource.name)

    for environment in environments:
        if not environment or not environment.workshop:
            return

        # If the workshop definition identity and generation are the same we do
        # not need to do anything.

        if (
            environment.workshop.uid == resource.metadata.uid
            and environment.workshop.generation == resource.metadata.generation
        ):
            return

        # Trigger replacement of the workshop environment with a new one.

        logger.info("Trigger replacement of workshop environment %s.", environment.name)

        replace_workshop_environment(environment)


def initialize_portal():
    initialize_kopf()
