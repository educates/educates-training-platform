import logging

import requests
from django.conf import settings
from django.utils import timezone

from .operator import background_task

logger = logging.getLogger("educates")


@background_task
def send_event_to_webhook(url, message):
    try:
        response = requests.post(url, json=message, timeout=2.5)
        if response.status_code >= 400:
            logger.error(
                "Failed to report event to %s: %s (status code %s)",
                url,
                message,
                response.status_code,
            )
    except Exception:
        logger.exception("Unable to report event to %s: %s", url, message)


def report_analytics_event(entity, event, data={}):
    message = None

    analytics_url = getattr(entity, "analytics_url", None)

    if event.startswith("User/"):
        user = entity

        message = {
            "portal": {
                "name": settings.TRAINING_PORTAL,
                "url": f"{settings.INGRESS_PROTOCOL}://{settings.PORTAL_HOSTNAME}",
            },
            "event": {
                "name": event,
                "timestamp": timezone.now().isoformat(),
                # Not sure why in this case need to convert to string.
                "user": str(user.get_username()),
                "data": data,
            },
        }

    elif event.startswith("Environment/"):
        environment = entity

        portal = environment.portal

        message = {
            "portal": {
                "name": settings.TRAINING_PORTAL,
                "uid": portal.uid,
                "generation": portal.generation,
                "url": f"{settings.INGRESS_PROTOCOL}://{settings.PORTAL_HOSTNAME}",
            },
            "event": {
                "name": event,
                "timestamp": timezone.now().isoformat(),
                "environment": environment.name,
                "workshop": environment.workshop_name,
                "data": data,
            },
        }

    else:
        session = entity

        portal = session.environment.portal

        message = {
            "portal": {
                "name": settings.TRAINING_PORTAL,
                "uid": portal.uid,
                "generation": portal.generation,
                "url": f"{settings.INGRESS_PROTOCOL}://{settings.PORTAL_HOSTNAME}",
            },
            "event": {
                "name": event,
                "timestamp": timezone.now().isoformat(),
                "user": session.owner and session.owner.get_username() or None,
                "session": session.name,
                "environment": session.environment_name(),
                "workshop": session.workshop_name(),
                "data": data,
            },
        }

    if message:
        logger.debug("Reporting analytics event %s as message %s.", event, message)

    if message:
        if settings.ANALYTICS_WEBHOOK_URL:
            send_event_to_webhook(settings.ANALYTICS_WEBHOOK_URL, message).schedule()

        if analytics_url:
            send_event_to_webhook(analytics_url, message).schedule()
