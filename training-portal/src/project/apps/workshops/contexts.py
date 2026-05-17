"""Context processors for setting variables available in templates.

"""

from django.conf import settings


def portal(request):
    """Adds context variables for portal configuration."""

    context = {}
    context["portal_title"] = settings.PORTAL_TITLE
    context["portal_logo"] = settings.PORTAL_LOGO
    context["portal_favicon"] = settings.PORTAL_FAVICON
    context["google_tracking_id"] = settings.GOOGLE_TRACKING_ID
    context["clarity_tracking_id"] = settings.CLARITY_TRACKING_ID
    context["amplitude_tracking_id"] = settings.AMPLITUDE_TRACKING_ID
    context["training_portal"] = settings.TRAINING_PORTAL
    context["ingress_domain"] = settings.INGRESS_DOMAIN
    context["ingress_protocol"] = settings.INGRESS_PROTOCOL
    return context
