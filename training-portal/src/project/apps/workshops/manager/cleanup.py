"""Defines cleanup functions for deleting expired workshop sessions and
later deleting historical records of sessions and inactive anonymous users.

"""

import traceback
import logging

from datetime import timedelta

import pykube
import requests

from django.conf import settings
from django.db import transaction
from django.utils import timezone
from django.contrib.auth import get_user_model

from oauth2_provider.models import clear_expired

from ..models import SessionState, Session

from .sessions import replace_reserved_session
from .locking import resources_lock
from .operator import background_task
from .analytics import report_analytics_event

logger = logging.getLogger("educates")

api = pykube.HTTPClient(pykube.KubeConfig.from_env())


# Minimum age a workshop session record must reach before the reaper will treat
# a missing Kubernetes resource as a sign the session was deleted out of band.
# This avoids racing with a session whose resource is still being created.

VANISHED_SESSION_GRACE_PERIOD = timedelta(minutes=2)

# Minimum age before a workshop session record still in the starting state is
# treated as stranded, where the deployment task for it failed or was lost to
# a process restart. This is much longer than the vanished grace period as
# records legitimately sit in the starting state while queued deployment
# tasks drain, which on a cold start of a large training portal against a
# slow Kubernetes API can take some minutes.

STRANDED_SESSION_GRACE_PERIOD = timedelta(minutes=10)


def session_requires_existence_check(session, now):
    """Returns whether it needs to be verified that a deployed workshop
    session still exists for a workshop session record. Requires the session
    record to have existed for at least a grace period first, so a session
    whose Kubernetes resource is still being created is never mistaken for
    one that has vanished; it will be reconsidered on a later pass. A record
    still in the starting state is subject to a much longer grace period, as
    creation of the Kubernetes resource for it may still be waiting on a
    queued deployment task, but is checked eventually so that a record whose
    deployment task never completed doesn't hold a capacity slot forever.

    """

    if session.is_stopped():
        return False

    if session.created is None:
        return not session.is_starting()

    age = now - session.created

    if session.is_starting():
        return age >= STRANDED_SESSION_GRACE_PERIOD

    return age >= VANISHED_SESSION_GRACE_PERIOD


@background_task
def purge_vanished_workshop_sessions():
    """Look for workshop session records where the deployed workshop session
    no longer exists, meaning it was deleted out of band, and schedule
    cleanup of the workshop session records.

    """

    now = timezone.now()

    # Take a snapshot of the workshop session records which haven't stopped
    # yet and have passed the applicable grace period, then check whether a
    # deployed workshop session still exists for each. If one doesn't, it
    # means it was deleted out of band, or its deployment task never
    # completed, so trigger a task to clean it up (there will be no
    # deployment to delete, but the database record still needs to be marked
    # as deleted). The check is done without holding the global resources
    # lock or an open database transaction, so a slow Kubernetes API cannot
    # stall other portal activity for the duration. The actual cleanup is
    # handed off to delete_workshop_session() which manages its own lock and
    # transaction.

    sessions = [
        session
        for session in Session.objects.select_related("environment").all()
        if session_requires_existence_check(session, now)
    ]

    if not sessions:
        return

    # Query the set of deployed workshop sessions using a single LIST request
    # against the Kubernetes REST API, rather than a separate GET request for
    # each workshop session record. The query is constrained to workshop
    # sessions belonging to this instance of the training portal by matching
    # on both the portal name and uid labels. If the query fails, skip
    # detection of vanished workshop sessions and try again on a subsequent
    # pass.

    K8SWorkshopSession = pykube.object_factory(
        api, "training.educates.dev/v1beta1", "WorkshopSession"
    )

    try:
        deployed_sessions = {
            resource.name
            for resource in K8SWorkshopSession.objects(api).filter(
                selector={
                    "training.educates.dev/portal.name": settings.PORTAL_NAME,
                    "training.educates.dev/portal.uid": settings.PORTAL_UID,
                }
            )
        }

    except pykube.exceptions.PyKubeError:
        logger.exception("Failed to query deployed workshop sessions.")

        return

    for session in sessions:
        if session.name not in deployed_sessions:
            logger.info(
                "Schedule cleanup of vanished workshop session %s.",
                session.name,
            )

            report_analytics_event(session, "Session/Vanished")

            delete_workshop_session(session).schedule()


@background_task
def purge_expired_workshop_sessions():
    """Look for workshop sessions which have expired and delete them."""

    now = timezone.now()

    # Take a snapshot of the workshop session records up front, then perform
    # the per-session HTTP checks below without holding the global resources
    # lock or an open database transaction. Each check makes synchronous
    # network calls, and an unresponsive session pod must not be able to
    # stall other portal activity (session allocation, the reconcile loop,
    # operator events) for the duration. The actual deletions are handed off
    # to delete_workshop_session() which manages its own lock and
    # transaction.

    for session in list(Session.objects.select_related("environment").all()):
        if session.is_allocated() or session.is_stopping():
            # If the workshop session is in use, including where it has been
            # explicitly marked for expiration, if expiration time has been
            # reached we need to delete it. If expiration time hasn't been
            # reached and there is an inactivity timeout, check that it hasn't
            # been orhpaned.

            if session.expires and session.expires <= now:
                logger.info(
                    "Schedule deletion of expired workshop session %s.",
                    session.name,
                )

                report_analytics_event(session, "Session/Expired")

                delete_workshop_session(session).schedule()

            elif session.environment.orphaned and not session.is_pending():
                # Note that we exclude pending sessions which are not yet marked
                # as running and are waiting to be activated. If we don't ignore
                # these, a reserved session which has been running for a while,
                # will be incorrectly seen as orhpaned.

                try:
                    # Query the idle time from the workshop session instance.
                    # Use the internal Kubernetes service for accessing the
                    # workshop instance as will fail if use public ingress and
                    # using a self signed CA as not currently injected such a
                    # CA into the training portal pod.

                    # host = f"{session.name}.{settings.INGRESS_DOMAIN}"
                    # url = f"{settings.INGRESS_PROTOCOL}://{host}/session/activity"

                    url = f"http://{session.name}.{session.environment.name}/session/activity"

                    response = requests.get(url, timeout=2.5)

                    if response.status_code == 200:
                        # If got a response and we have exceeded the
                        # inactivity timeout then trigger deletion of the
                        # workshop session.

                        idle_time = timedelta(seconds=response.json()["idle-time"])
                        last_view = timedelta(seconds=response.json()["last-view"])

                        # The workshop session gateway measures idle time from
                        # when its process started, not from when the session
                        # was allocated to a user, and only a browser poll
                        # resets it. A reserved session therefore reports the
                        # whole time it sat unallocated in reserve as idle
                        # time, which for a long enough reserved period would
                        # exceed the inactivity timeout the moment the session
                        # is activated, before the user's browser has had a
                        # chance to poll. Clamp both reported values to the
                        # time the user has actually held the session.

                        if session.started:
                            allocated_time = now - session.started

                            idle_time = min(idle_time, allocated_time)
                            last_view = min(last_view, allocated_time)

                        if idle_time >= session.environment.orphaned:
                            logger.info(
                                "Schedule deletion of orphaned workshop session %s after period of %s seconds.",
                                session.name,
                                idle_time.total_seconds(),
                            )

                            report_analytics_event(session, "Session/Orphaned")

                            delete_workshop_session(session).schedule()

                        elif last_view >= (3 * session.environment.orphaned):
                            logger.info(
                                "Schedule deletion of inactive workshop session %s after period of %s seconds.",
                                session.name,
                                last_view.total_seconds(),
                            )

                            report_analytics_event(session, "Session/Inactive")

                            delete_workshop_session(session).schedule()

                    else:
                        # XXX If we don't get a valid response then not
                        # currently doing anything. Need a better method to
                        # determine if was running but has since failed in
                        # some way and become uncontactable. In that case
                        # right now will only be deleted when workshop timeout
                        # expires if there is one.

                        pass

                except (
                    requests.exceptions.ConnectionError,
                    requests.exceptions.Timeout,
                ):
                    # XXX This can just be because it is slow to start up, or
                    # the probe timed out. Need a better method to determine if
                    # was running but has since failed in some way and become
                    # uncontactable. In that case right now will only be deleted
                    # when workshop timeout expires if there is one.

                    logger.warning(
                        "Cannot connect to workshop session %s.", session.name
                    )

                except Exception:  # pylint: disable=broad-except
                    # Not aware of circumstances where would get an unexpected
                    # exception, but need to log and ignore it as we don't
                    # want to stop looping over all sessions.

                    logger.exception(
                        "Failed to query idle time for workshop session %s.",
                        session.name,
                    )


@background_task
@resources_lock
def delete_workshop_session(session):
    """Deletes a workshop session."""

    logger.info("Deleting workshop session %s.", session.name)

    # First attempt to delete the deployment of the workshop session. It
    # doesn't matter if it doesn't exist. That situation can arise where
    # the workshop session was deleted manually for some reason.

    K8SWorkshopSession = pykube.object_factory(
        api, "training.educates.dev/v1beta1", "WorkshopSession"
    )

    try:
        resource = K8SWorkshopSession.objects(api).get(name=session.name)
        resource.delete()

    except pykube.exceptions.ObjectDoesNotExist:
        pass

    except pykube.exceptions.PyKubeError:
        logger.exception("Failed to delete workshop session %s.", session.name)

    # Update the workshop session as stopped in the database, then see
    # whether a new workshop session needs to be created in its place as
    # a reserved session.

    with transaction.atomic():
        session.mark_as_stopped()
        if session.owner:
            report_analytics_event(session, "Session/Deleted")

        replace_reserved_session(session.environment)


@background_task
@transaction.atomic
def cleanup_old_sessions_and_users():
    """Delete records for any sessions older than a certain time, remove any
    anonymous user accounts that have no active sessions and which are older
    than a certain time, and clear expired OAuth tokens.

    """

    cutoff = timezone.now() - timedelta(hours=36)

    # Delete records of workshop sessions more than 36 hours old.

    Session.objects.filter(state=SessionState.STOPPED, expires__lte=cutoff).delete()

    # Delete anonymous users more than 36 hours old that no longer have any
    # workshop sessions associated with them. Filtering on session__isnull=True
    # restricts the delete to users with no sessions, so it is a single atomic
    # operation that never trips the PROTECT constraint on Session.owner.

    User = get_user_model()  # pylint: disable=invalid-name

    User.objects.filter(
        groups__name="anonymous",
        date_joined__lte=cutoff,
        session__isnull=True,
    ).delete()

    # Clear expired OAuth tokens.

    clear_expired()
