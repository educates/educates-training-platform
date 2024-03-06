import logging

import requests

from datetime import datetime, timezone

from .operator_config import ANALYTICS_WEBHOOK_URL


def current_time():
    dt = datetime.now(timezone.utc)
    tz_dt = dt.astimezone()
    return tz_dt.isoformat()


def send_event_to_webhook(url, message):
    try:
        requests.post(url, json=message, timeout=2.5)
    except Exception:
        logging.exception("Unable to report event to %s: %s", url, message)


def report_analytics_event(event, data={}):
    message = None

    logging.info("Reporting analytics event %s with data %s.", event, data)

    if not ANALYTICS_WEBHOOK_URL:
        return

    message = {
        "event": {
            "name": event,
            "timestamp": current_time(),
            "data": data,
        },
    }

    if message:
        send_event_to_webhook(ANALYTICS_WEBHOOK_URL, message)
