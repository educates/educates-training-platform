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

from ..models import SessionState, Session

from .sessions import replace_reserved_session
from .locking import resources_lock
from .operator import background_task
from .analytics import report_analytics_event

logger = logging.getLogger("educates")

api = pykube.HTTPClient(pykube.KubeConfig.from_env())


@background_task
@resources_lock
@transaction.atomic
def purge_expired_workshop_sessions():
    """Look for workshop sessions which have expired and delete them."""

    now = timezone.now()

    # Loop over all records of workshop sessions in the database and check
    # whether any should be deleted and/or marked as stopped.

    K8SWorkshopSession = pykube.object_factory(
        api, "training.educates.dev/v1beta1", "WorkshopSession"
    )

    for session in Session.objects.all():
        if not session.is_starting() and not session.is_stopped():
            # If the workshop session isn't still starting, and hasn't stopped
            # yet, check to see whether there is a deployed workshop session.
            # If there isn't, it means it was deleted manually. In this case
            # trigger a task to clean up the workshop session. In this case
            # there will be no deployment to delete, but still have to mark
            # the workshop session as deleted in the database.

            try:
                K8SWorkshopSession.objects(api).get(name=session.name)

            except pykube.exceptions.ObjectDoesNotExist:
                logger.info(
                    "Schedule cleanup of vanished workshop session %s.",
                    session.name,
                )

                report_analytics_event(session, "Session/Vanished")

                delete_workshop_session(session).schedule()

                continue

            except pykube.exceptions.PyKubeError:
                pass

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

                    response = requests.get(url)

                    if response.status_code == 200:
                        # If got a response and we have exceeded the
                        # inactivity timeout then trigger deletion of the
                        # workshop session.

                        idle_time = timedelta(seconds=response.json()["idle-time"])
                        last_view = timedelta(seconds=response.json()["last-view"])

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

                except requests.exceptions.ConnectionError:
                    # XXX This can just be because it is slow to start up.
                    # Need a better method to determine if was running but has
                    # since failed in some way and become uncontactable. In
                    # that case right now will only be deleted when workshop
                    # timeout expires if there is one.

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
@resources_lock
def cleanup_old_sessions_and_users():
    """Delete records for any sessions older than a certain time, and then
    remove any anonymous user accounts that have no active sessions and which
    are older than a certain time.

    """

    with transaction.atomic():
        # Delete record of workshop sessions more than 36 hours old.

        cutoff = timezone.now() - timedelta(hours=36)

        sessions = Session.objects.filter(
            state=SessionState.STOPPED, expires__lte=cutoff
        )

        for session in sessions:
            logger.info("Cleanup old workshop session %s.", session.name)
            session.delete()

        # Delete any anonymous users older than 36 hours old, which
        # now don't have any workshop sessions associated with them.

        User = get_user_model()  # pylint: disable=invalid-name

        users = User.objects.filter(groups__name="anonymous", date_joined__lte=cutoff)

        for user in users:
            sessions = Session.objects.filter(owner=user)

            if not sessions:
                logger.info("Deleting anonymous user %s.", user.get_username())
                report_analytics_event(user, "User/Delete", {"group": "anonymous"})
                user.delete()
