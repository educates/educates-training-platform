"""Defines basic functions for managing creation of workshop sessions."""

import string
import random
import logging
import base64

from itertools import islice

import pykube
import rstr

from django.conf import settings
from django.contrib.auth import get_user_model
from django.db import transaction
from django.utils import timezone

from oauth2_provider.models import Application

from ..models import Session

from .operator import background_task
from .locking import resources_lock
from .analytics import report_analytics_event

logger = logging.getLogger("educates")

api = pykube.HTTPClient(pykube.KubeConfig.from_env())


def resolve_request_params(workshop, params):
    default_params = {}

    def default_value(param):
        if "value" in param:
            return param["value"]
        elif "generate" in param and param["generate"] == "expression":
            return rstr.xeger(param.get("from", ""))
        else:
            return ""

    for item in workshop.params:
        name = item.get("name", "")

        if name:
            default_params[name] = default_value(item)

    final_params = dict(default_params)

    for item in params:
        name = item.get("name", "")
        value = item.get("value", "")

        if name and name in final_params:
            final_params[name] = value

    return final_params


def create_request_resources(session):
    secret_body = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": f"{session.name}-user",
            "namespace": session.environment.name,
            "labels": {
                "training.educates.dev/component": "request",
                "training.educates.dev/component.group": "variables",
                "training.educates.dev/workshop.name": session.environment.workshop.name,
                "training.educates.dev/portal.name": settings.PORTAL_NAME,
                "training.educates.dev/portal.uid": settings.PORTAL_UID,
                "training.educates.dev/environment.name": session.environment.name,
                "training.educates.dev/session.name": session.name,
            },
            "ownerReferences": [
                {
                    "apiVersion": "training.educates.dev/v1beta1",
                    "kind": "WorkshopSession",
                    "blockOwnerDeletion": True,
                    "controller": True,
                    "name": session.name,
                    "uid": session.uid,
                }
            ],
        },
        "data": {},
    }

    for key, value in (
        ("username", session.owner.username),
        ("first_name", session.owner.first_name),
        ("last_name", session.owner.last_name),
        ("email", session.owner.email),
    ):
        secret_body["data"][key] = base64.b64encode(value.encode("UTF-8")).decode(
            "UTF-8"
        )

    pykube.Secret(api, secret_body).create()

    secret_body = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": f"{session.name}-request",
            "namespace": session.environment.name,
            "labels": {
                "training.educates.dev/component": "request",
                "training.educates.dev/component.group": "variables",
                "training.educates.dev/workshop.name": session.environment.workshop.name,
                "training.educates.dev/portal.name": settings.PORTAL_NAME,
                "training.educates.dev/portal.uid": settings.PORTAL_UID,
                "training.educates.dev/environment.name": session.environment.name,
                "training.educates.dev/session.name": session.name,
            },
            "ownerReferences": [
                {
                    "apiVersion": "training.educates.dev/v1beta1",
                    "kind": "WorkshopSession",
                    "blockOwnerDeletion": True,
                    "controller": True,
                    "name": session.name,
                    "uid": session.uid,
                }
            ],
        },
        "data": {},
    }

    for key, value in session.params.items():
        secret_body["data"][key] = base64.b64encode(value.encode("UTF-8")).decode(
            "UTF-8"
        )

    pykube.Secret(api, secret_body).create()

    K8SWorkshopAllocation = pykube.object_factory(
        api, "training.educates.dev/v1beta1", "WorkshopAllocation"
    )

    allocation_body = {
        "apiVersion": "training.educates.dev/v1beta1",
        "kind": "WorkshopAllocation",
        "metadata": {
            "name": f"{session.name}",
            "labels": {
                "training.educates.dev/component": "request",
                "training.educates.dev/workshop.name": session.environment.workshop.name,
                "training.educates.dev/portal.name": settings.PORTAL_NAME,
                "training.educates.dev/portal.uid": settings.PORTAL_UID,
                "training.educates.dev/environment.name": session.environment.name,
                "training.educates.dev/environment.uid": session.environment.uid,
                "training.educates.dev/session.name": session.name,
            },
            "ownerReferences": [
                {
                    "apiVersion": "training.educates.dev/v1beta1",
                    "kind": "WorkshopSession",
                    "blockOwnerDeletion": True,
                    "controller": True,
                    "name": session.name,
                    "uid": session.uid,
                }
            ],
        },
        "spec": {
            "environment": {"name": session.environment.name},
            "session": {"name": session.name, "user": session.owner.username},
        },
    }

    resource = K8SWorkshopAllocation(api, allocation_body)
    resource.create()

    logger.info(
        "Created workshop allocation request for workshop session %s.", session.name
    )


def update_session_status(name, phase, user=None):
    """Update the status of the Kubernetes resource object for the workshop
    session.

    """

    try:
        K8SWorkshopSession = pykube.object_factory(
            api, "training.educates.dev/v1beta1", "WorkshopSession"
        )

        resource = K8SWorkshopSession.objects(api).get(name=name)

        # The status may not exist as yet if not processed by the operator.
        # In this case fill it in and operator will preserve the value when
        # sees associated with a training portal.

        status = resource.obj.setdefault("status", {}).setdefault(
            "educates", {}
        )

        status["phase"] = phase

        if user:
            status["user"] = str(user.username)

        resource.update()

        if user:
            logger.info(
                "Updated status of workshop session %s to %s for user %s.",
                name,
                phase,
                user.username,
            )
        else:
            logger.info("Updated status of workshop session %s to %s.", name, phase)

    except pykube.exceptions.ObjectDoesNotExist:
        pass

    except pykube.exceptions.PyKubeError:
        logger.exception("Failed to update status of workshop session %s.", name)


def create_workshop_session(session, secret):
    """Triggers the deployment of a new workshop session to the cluster."""

    logger.info(
        "Deploying new workshop session %s for workshop environment %s.",
        session.name,
        session.environment.name,
    )

    # Calculate the additional set of environment variables to configure the
    # workshop session for use with the training portal. These are on top of
    # any environment variables the workshop itself uses.

    environment = session.environment

    session_env = list(environment.env)

    session_env.append({"name": "PORTAL_CLIENT_ID", "value": session.name})
    session_env.append({"name": "PORTAL_CLIENT_SECRET", "value": secret})

    portal_url = f"{settings.INGRESS_PROTOCOL}://{settings.PORTAL_HOSTNAME}"
    portal_api_url = f"http://training-portal.{settings.PORTAL_NAMESPACE}"

    session_env.append({"name": "PORTAL_URL", "value": portal_url})
    session_env.append({"name": "PORTAL_API_URL", "value": portal_api_url})
    session_env.append({"name": "SESSION_NAME", "value": session.name})
    session_env.append({"name": "TRAINING_PORTAL", "value": settings.PORTAL_NAME})
    session_env.append({"name": "FRAME_ANCESTORS", "value": settings.FRAME_ANCESTORS})

    if environment.expires or environment.orphaned or environment.overdue:
        restart_url = f"{portal_url}/workshops/session/{session.name}/delete/"
    else:
        restart_url = f"{portal_url}/workshops/catalog/"

    if environment.expires:
        session_env.append({"name": "ENABLE_COUNTDOWN", "value": "true"})

    session_env.append({"name": "RESTART_URL", "value": restart_url})

    # Prepare the body of the resource describing the workshop session.

    characters = string.ascii_letters + string.digits
    config_password = "".join(random.sample(characters, 32))

    session_body = {
        "apiVersion": "training.educates.dev/v1beta1",
        "kind": "WorkshopSession",
        "metadata": {
            "name": session.name,
            "labels": {
                "training.educates.dev/portal.name": settings.PORTAL_NAME,
                "training.educates.dev/portal.uid": settings.PORTAL_UID,
                "training.educates.dev/environment.name": session.environment.name,
                "training.educates.dev/environment.uid": session.environment.uid,
            },
            "ownerReferences": [
                {
                    "apiVersion": "training.educates.dev/v1beta1",
                    "kind": "WorkshopEnvironment",
                    "blockOwnerDeletion": True,
                    "controller": True,
                    "name": session.environment.name,
                    "uid": session.environment.uid,
                }
            ],
        },
        "spec": {
            "workshop": {
                "name": session.environment.workshop_name,
            },
            "portal": {
                "name": settings.PORTAL_NAME,
                "url": portal_url,
            },
            "environment": {"name": session.environment.name},
            "session": {
                "id": session.id,
                "username": "",
                "password": "",
                "config": {
                    "password": config_password,
                },
                "ingress": {
                    "domain": settings.INGRESS_DOMAIN,
                    "secret": settings.INGRESS_SECRET,
                },
                "env": session_env,
            },
        },
    }

    # If Google or Clarity analytics tracking ID is provided, this needs to be
    # patched into the resource.

    if settings.GOOGLE_TRACKING_ID is not None:
        session_body["spec"]["analytics"] = {
            "google": {"trackingId": settings.GOOGLE_TRACKING_ID}
        }

    if settings.CLARITY_TRACKING_ID is not None:
        session_body["spec"]["analytics"] = {
            "clarity": {"trackingId": settings.CLARITY_TRACKING_ID}
        }

    if settings.AMPLITUDE_TRACKING_ID is not None:
        session_body["spec"]["analytics"] = {
            "amplitude": {"trackingId": settings.AMPLITUDE_TRACKING_ID}
        }

    # Create the Kubernetes resource for the workshop session.

    K8SWorkshopSession = pykube.object_factory(
        api, "training.educates.dev/v1beta1", "WorkshopSession"
    )

    resource = K8SWorkshopSession(api, session_body)
    resource.create()

    logger.info("Deployed workshop session %s.", session.name)

    session.uid = resource.obj["metadata"]["uid"]
    session.password = config_password

    # Update and save the state of the workshop session database record to
    # indicate it is running or waiting for confirmation on being activated if
    # this session was created via the REST API.

    session.url = (
        f"{settings.INGRESS_PROTOCOL}://{session.name}.{settings.INGRESS_DOMAIN}"
    )

    report_analytics_event(session, "Session/Created")

    if session.owner:
        update_session_status(session.name, "Allocated", session.owner)
        report_analytics_event(session, "Session/Started")
        if session.token:
            session.mark_as_waiting()
        else:
            session.mark_as_running()

            def _schedule_resource_creation():
                create_request_resources(session)

            transaction.on_commit(_schedule_resource_creation)
    else:
        update_session_status(session.name, "Available")
        session.mark_as_waiting()


def setup_workshop_session(environment, **session_kwargs):
    """Setup database objects pertaining to a new workshop session."""

    # Increase tally for number of workshop sessions created for the workshop
    # environment and calculate session name. Ensure changed value for tally
    # is saved.

    tally = environment.tally = environment.tally + 1

    session_id = f"s{tally:03}"
    session_name = f"{environment.name}-{session_id}"

    environment.save()

    # Calculate the set of redirect URIs that the OAuth provider application
    # needs to trust. Needs to be enumerated as can't use a wildcard, As such,
    # need a redirect URI for the main workshop URL, one for each embedded
    # application such as the console, plus one for each ingress as they are
    # proxied via the workshop gateway and so are also covered by OAuth.

    def redirect_uri_for_oauth_callback(name=None):
        def build_url(host):
            fqdn = f"{host}.{settings.INGRESS_DOMAIN}"
            return f"{settings.INGRESS_PROTOCOL}://{fqdn}/oauth_callback"

        urls = []

        host = session_name

        if name:
            urls.append(build_url(f"{name}-{host}"))
            # Note that suffix use is deprecated, use prefix instead.
            urls.append(build_url(f"{host}-{name}"))
        else:
            urls.append(build_url(host))

        return urls

    redirect_uris = []

    redirect_uris.extend(redirect_uri_for_oauth_callback())
    redirect_uris.extend(redirect_uri_for_oauth_callback("console"))
    redirect_uris.extend(redirect_uri_for_oauth_callback("editor"))
    redirect_uris.extend(redirect_uri_for_oauth_callback("slides"))
    redirect_uris.extend(redirect_uri_for_oauth_callback("terminal"))

    ingresses = environment.workshop.ingresses

    for ingress in ingresses:
        redirect_uris.extend(redirect_uri_for_oauth_callback(ingress["name"]))

    # Create the OAuth provider application record. Each workshop session
    # has a unique application record tied to the URLs for that specific
    # workshop session.

    User = get_user_model()  # pylint: disable=invalid-name

    admin_user = User.objects.get(username=settings.ADMIN_USERNAME)

    characters = string.ascii_letters + string.digits
    secret = "".join(random.sample(characters, 32))

    application, _ = Application.objects.get_or_create(
        name=session_name,
        client_id=session_name,
        user=admin_user,
        redirect_uris=" ".join(redirect_uris),
        client_type="public",
        authorization_grant_type="authorization-code",
        client_secret=secret,
        skip_authorization=True,
    )

    # Create the database record for the workshop session, linking it to
    # the OAuth provider application record.

    session = Session.objects.create(
        name=session_name,
        id=session_id,
        application=application,
        created=session_kwargs.get("started", timezone.now()),
        environment=environment,
        **session_kwargs,
    )

    return session, secret


def create_new_session(environment):
    """Setup a record for the workshop session in the database and schedule
    a task to deploy the workshop session in the cluster.

    """

    session, secret = setup_workshop_session(environment)

    def _schedule_session_creation():
        create_workshop_session(session, secret)

    transaction.on_commit(_schedule_session_creation)

    return session


def replace_reserved_session(environment):
    """If required to have reserved workshop instances, unless we have reached
    capacity for the workshop environment, or overall maximum number of
    allowed sessions across all workshops, initiate creation of a new workshop
    session. Note that this should only be called in circumstance where just
    deleted, or allocated a workshop session for the workshop environment. In
    other words, replacing it.

    """

    # Bail out straight away if no reserved sessions.

    if not environment.reserved:
        return

    # Check that haven't already reached limit on number of reserved sessions.

    if environment.available_sessions_count() >= environment.reserved:
        return

    # Also check that haven't reached capacity. This counts both allocated and
    # reserved sessions.

    if environment.active_sessions_count() >= environment.capacity:
        return

    # Finally need to make sure that haven't reached capacity for the training
    # portal as a whole.

    portal = environment.portal

    if portal.sessions_maximum:
        if portal.active_sessions_count() >= portal.sessions_maximum:
            return

    # Safe to create a new workshop session in reserve.

    logger.info(
        "Schedule creation of new reserved workshop session for workshop environment %s.",
        environment.name,
    )

    create_new_session(environment)


@background_task
@resources_lock
@transaction.atomic
def terminate_reserved_sessions(portal):
    """Terminate any reserved workshop sessions which put a workshop
    environment over the count for how many reserved sessions they are
    allowed.

    """

    # First kill of reserved sessions for each workshop environment where
    # they are over what is allowed for that workshop environment.

    for environment in portal.running_environments():
        # If initial number of sessions is greater than reserved sessions then
        # don't reconcile until after number of sessions would fall below the
        # required reserved number. Note that this doesn't really deal properly
        # with where sessions are purged after some time and so count of all
        # sessions drops back to zero. Should really look at number of sessions
        # created over time, rather than how many exist right now.

        if environment.initial > environment.reserved:
            excess = environment.initial - environment.reserved
            if environment.all_sessions_count() < excess:
                continue
            if environment.available_sessions_count() > environment.reserved:
                continue

        excess = max(0, environment.available_sessions_count() - environment.reserved)

        for session in islice(environment.available_sessions(), 0, excess):
            logger.info("Terminating reserved workshop session %s.", session.name)

            update_session_status(session.name, "Stopping")
            session.mark_as_stopping()
            report_analytics_event(session, "Session/Terminate")

    # Also check that not exceed capacity for the whole training portal. If
    # we are, try and kill of oldest reserved sessions associated with any
    # workshop environment.

    if portal.sessions_maximum != 0:
        excess = max(0, portal.active_sessions_count() - portal.sessions_maximum)

        for session in islice(
            portal.available_sessions().order_by("created"), 0, excess
        ):
            logger.info("Terminating reserved workshop session %s.", session.name)

            update_session_status(session.name, "Stopping")
            session.mark_as_stopping()
            report_analytics_event(session, "Session/Terminate")


@background_task
@resources_lock
@transaction.atomic
def initiate_reserved_sessions(portal):
    """Create additional reserved sessions if necessary to satisfy stated
    reserved count for a workshop environment. Don't create a reserved session
    if this would put the workshop environment of the training portal over any
    maximum capacity.

    """

    sessions = []

    # Need a different approach when maximum number of sessions defined for
    # the whole training portal. Even in that case, still additionally need to
    # consider capacity of individual workshop environments.

    if portal.sessions_maximum == 0:
        # No global maximum on number of sessions for training portal. In
        # this case can check easch workshop environment independently.

        for environment in portal.running_environments():
            # If reserved sessions not required, skip to next one.

            if environment.reserved == 0:
                continue

            # If initial number of sessions is 0 and no workshop sessions
            # have yet been created, then skip to next one, as will only go
            # on to create reserved sessions when the first request for a
            # session arrives.

            if environment.initial == 0:
                if environment.all_sessions_count() == 0:
                    continue

            # If already at capacity, skip to next one.

            spare_capacity = environment.capacity - environment.active_sessions_count()

            if spare_capacity <= 0:
                continue

            # If already have required number of reserved sessons, skip to
            # next one.

            spare_reserved = (
                environment.reserved - environment.available_sessions_count()
            )

            if spare_reserved <= 0:
                continue

            # Create required number of reserved sessions ensuring we do not
            # go over capacity for the workshop environment.

            spare_sessions = min(spare_reserved, spare_capacity)

            for _ in range(spare_sessions):
                sessions.append(setup_workshop_session(environment))

    else:
        # Maximum number of sessions for training portal in force, so work out
        # how much spare capacity we currently have for the training portal.
        # If no capacity we can bail out straight away.

        spare_capacity = portal.sessions_maximum - portal.active_sessions_count()

        if spare_capacity <= 0:
            return

        # Now check each separate workshop environment.

        for environment in portal.running_environments():
            # If reserved sessions not required, skip to next one.

            if environment.reserved == 0:
                continue

            # If initial number of sessions is 0 and no workshop sessions
            # have yet been created, then skip to next one, as will only go
            # on to create reserved sessions when the first request for a
            # session arrives.

            if environment.initial == 0:
                if environment.all_sessions_count() == 0:
                    continue

            # If already have required number of reserved sessons, skip to
            # next one.

            spare_reserved = (
                environment.reserved - environment.available_sessions_count()
            )

            if spare_reserved <= 0:
                continue

            # Create required number of reserved sessions ensuring we do not
            # go over capacity for the workshop environment, but also that
            # this will not put us over capacity for whole training portal.

            spare_sessions = min(
                spare_reserved,
                environment.capacity - environment.active_sessions_count(),
                spare_capacity,
            )

            for _ in range(spare_sessions):
                sessions.append(setup_workshop_session(environment))

                # Reduce count of how much capacity still have for the
                # training portal as a whole.

                spare_capacity -= 1

            # Bail out if reached capacity for the whole training portal.

            if spare_capacity <= 0:
                break

    # Schedule the actual creation of the reserved sessions.

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


def allocate_session_for_user(
    environment,
    user,
    token,
    timeout=None,
    params={},
    index_url=None,
    analytics_url=None,
):
    """Allocate a workshop session to the user for the specified workshop
    environment from any reserved workshop sessions. Replace now allocated
    workshop session with a new reserved session if required.

    """

    session = environment.available_session()

    if not session:
        return

    # We will have a token when requested via the REST API. The owner and
    # token is updated in this case but left in pending state until activation
    # of the workshop session is subsequently confirmed. The extra
    # confirmation is needed so can reclaim a session which was abandoned
    # immediately due to not being accessed.

    session.params = resolve_request_params(session.environment.workshop, params)

    session.index_url = index_url
    session.analytics_url = analytics_url

    if token:
        update_session_status(session.name, "Allocating", user)
        report_analytics_event(session, "Session/Pending")
        session.mark_as_pending(user, token, timeout)
    else:
        update_session_status(session.name, "Allocated", user)
        report_analytics_event(session, "Session/Started")
        session.mark_as_running(user)

        def _schedule_resource_creation():
            create_request_resources(session)

        transaction.on_commit(_schedule_resource_creation)

    # See if we need to create a new reserved session to replace the one which
    # was just allocated.

    replace_reserved_session(environment)

    return session


def create_session_for_user(
    environment,
    user,
    token,
    timeout=None,
    params={},
    index_url=None,
    analytics_url=None,
):
    """Create a new workshop session in case there was no existing reserved
    workshop sessions for the specified workshop environment.

    """

    # Check first if not exceeding the capacity of the workshop environment.
    # Using the active session count here, which includes workshop sessions
    # which are in reserve, but we would only usually be called in situation
    # where there weren't any reserved sessions in the first place.

    if environment.active_sessions_count() >= environment.capacity:
        return

    # Next see if there is a maximum number of workshop sessions allowed
    # across the whole training portal. If there isn't we are good to create a
    # new workshop session.

    portal = environment.portal

    if portal.sessions_maximum == 0:
        session = create_new_session(environment)

        session.params = resolve_request_params(session.environment.workshop, params)

        session.index_url = index_url
        session.analytics_url = analytics_url

        if token:
            update_session_status(session.name, "Allocating", user)
        else:
            update_session_status(session.name, "Allocated", user)

        session.mark_as_pending(user, token, timeout)

        if not token:
            report_analytics_event(session, "Session/Started")
        else:
            report_analytics_event(session, "Session/Pending")

        return session

    # Check the number of allocated workshop sessions for the whole training
    # portal and see if we can still have any more workshops sessions.

    if portal.allocated_sessions_count() >= portal.sessions_maximum:
        return

    # Now see if we can create a new workshop session without needing to kill
    # off a reserved session for a different workshop. The active sessions
    # count includes reserved sessions as well as allocated sessions.

    if portal.active_sessions_count() < portal.sessions_maximum:
        session = create_new_session(environment)

        session.params = resolve_request_params(session.environment.workshop, params)

        session.index_url = index_url
        session.analytics_url = analytics_url

        if token:
            update_session_status(session.name, "Allocating", user)
        else:
            update_session_status(session.name, "Allocated", user)

        session.mark_as_pending(user, token, timeout)

        if not token:
            report_analytics_event(session, "Session/Started")
        else:
            report_analytics_event(session, "Session/Pending")

        return session

    # No choice but to first kill off a reserved session for a different
    # workshop. This should target the least active workshop but we are not
    # tracking any statistics yet to do that with certainty, so kill off the
    # oldest session. We kill it off by expiring it immediately and then
    # letting the session reaper kick in and delete it. Double check that
    # there is at least one reserved session.

    sessions = portal.available_sessions().order_by("created")

    if sessions:
        session = sessions[0]
        update_session_status(session.name, "Stopping")
        session.mark_as_stopping()
        report_analytics_event(session, "Session/Terminate")

    # Now create the new workshop session for the required workshop
    # environment.

    session = create_new_session(environment)

    session.params = resolve_request_params(session.environment.workshop, params)

    session.index_url = index_url
    session.analytics_url = analytics_url

    if token:
        update_session_status(session.name, "Allocating", user)
    else:
        update_session_status(session.name, "Allocated", user)

    session.mark_as_pending(user, token, timeout)

    if not token:
        report_analytics_event(session, "Session/Started")
    else:
        report_analytics_event(session, "Session/Pending")

    return session


def retrieve_session_for_user(
    environment,
    user,
    session_name=None,
    token=None,
    timeout=None,
    params={},
    index_url=None,
    analytics_url=None,
):
    """Determine if there is already an allocated session for this workshop
    environment which the user is an owner of. If there is return it. Note
    that if we have a token because this is being requested via the REST API,
    it will not overwrite any existing token as we want to reuse the existing
    one and not generate a new one. if we can't find an existing session, we
    will create a new one if there is available capacity. If there is no
    available capacity, no session will be returned.

    """

    # Note that we assume that if the session is marked as stopping we
    # should not use it since it should be in the process of being deleted.
    # In that case we will create a new one even though it means user will
    # have more than one session for a brief period of time.

    session = environment.allocated_session_for_user(user)

    if session and not session.is_stopping():
        if token and session.is_pending():
            session.mark_as_pending(user, token, timeout)

        # If a session name was provided then any existing session found for the
        # user must have that name. This is so that it is possible to reacquire
        # a session that was previously created via the REST API and not create
        # a new one if it couldn't be found.

        if session_name and session.name != session_name:
            return

        return session

    # A session name was provided but we didn't find an existing session so
    # we do not create a new one.

    if session_name:
        return

    # Determine if the user is permitted to create a workshop session.

    portal = environment.portal

    if not portal.session_permitted_for_user(user):
        return

    # Attempt to allocate a session to the user for the workshop environment
    # from any set of reserved sessions.

    session = allocate_session_for_user(
        environment, user, token, timeout, params, index_url, analytics_url
    )

    if session:
        return session

    # There are no reserved sessions, so we need to trigger the creation
    # of a new session if there is available capacity. If there is no
    # available capacity, no session will be returned.

    return create_session_for_user(
        environment, user, token, timeout, params, index_url, analytics_url
    )
