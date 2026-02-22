"""Defines view handlers for working with sessions via the web interface and
REST API.

"""

__all__ = [
    "session",
    "session_activate",
    "session_delete",
    "session_terminate",
    "session_authorize",
    "session_config",
    "session_schedule",
    "session_extend",
    "session_event",
]

import json

from django.shortcuts import render, redirect, reverse
from django.contrib.auth.decorators import login_required
from django.views.decorators.csrf import csrf_exempt
from django.http import (
    Http404,
    HttpResponseForbidden,
    HttpResponseBadRequest,
    HttpResponseServerError,
)
from django.views.decorators.http import require_http_methods
from django.db import transaction
from django.contrib.auth import login
from django.http import JsonResponse
from django.conf import settings

from oauth2_provider.decorators import protected_resource

from csp.decorators import csp_update

from ..manager.locking import resources_lock
from ..manager.cleanup import delete_workshop_session
from ..manager.sessions import update_session_status, create_request_resources
from ..manager.analytics import report_analytics_event
from ..models import TrainingPortal, SessionState
from .helpers import update_query_params


@login_required(login_url="/")
@require_http_methods(["GET"])
@resources_lock
@transaction.atomic
def session(request, name):
    """Renders the framed workshop session."""

    context = {}

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # First check is there is a stopped session for the user.

    instance = portal.stopped_session(name, request.user)

    if instance and instance.index_url:
        # If there is a stopped session and an index URL is set then redirect to
        # that URL. This can occur when session times out and the user attempts
        # to access the session afterwards.

        return redirect(
            update_query_params(instance.index_url, {"notification": "session-deleted"})
        )

    # Now check if there is allocated session for the user.

    instance = portal.allocated_session(name, request.user)

    if not instance:
        if not request.user.is_staff and settings.PORTAL_INDEX:
            return redirect(
                update_query_params(
                    settings.PORTAL_INDEX, {"notification": "session-invalid"}
                )
            )

        return redirect(
            update_query_params(
                reverse("workshops_catalog"), {"notification": "session-invalid"}
            )
        )

    context["session"] = instance
    context["session_owner"] = instance.owner and instance.owner.get_username() or ""

    context["session_url"] = (
        f"{settings.INGRESS_PROTOCOL}://{instance.name}.{settings.INGRESS_DOMAIN}"
    )

    portal_url = f"{settings.INGRESS_PROTOCOL}://{settings.PORTAL_HOSTNAME}"

    context["restart_url"] = (
        f"{portal_url}/workshops/session/{instance.name}/delete/?notification=startup-timeout"
    )
    context["startup_timeout"] = instance.environment.overdue.total_seconds()

    try:
        with open("/opt/app-root/static/theme/training-portal.html") as fp:
            context["portal_head_html"] = fp.read()
    except Exception:
        context["portal_head_html"] = ""

    response = render(request, "workshops/session.html", context)

    # This is abusing django-csp decorators in order to set a dynamic value
    # for connect-src and frame-src. Specifically, needs to be the hostname
    # of the users session.

    return csp_update(
        CONNECT_SRC=f"{instance.name}.{settings.INGRESS_DOMAIN}",
        FRAME_SRC=f"{instance.name}.{settings.INGRESS_DOMAIN}",
    )(lambda: response)()


@require_http_methods(["GET"])
def session_activate(request, name):
    """Activates the session spawned by an anonymous request via the
    web interface or REST API.

    """

    access_token = request.GET.get("token")

    if not access_token:
        return HttpResponseBadRequest("No access token supplied")

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # First check is there is a stopped session for the user.

    instance = portal.stopped_session(name, request.user)

    if instance and instance.index_url:
        # If there is a stopped session and an index URL is set then redirect to
        # that URL. This can occur when session times out and the user attempts
        # to activate the session afterwards.

        return redirect(
            update_query_params(instance.index_url, {"notification": "session-deleted"})
        )
    
    # Now check if there is allocated session for the user.

    instance = portal.allocated_session(name)

    if not instance:
        return HttpResponseBadRequest("Invalid session name supplied")

    if instance.token != access_token:
        return HttpResponseBadRequest("Invalid access token for session")

    if not instance.owner:
        return HttpResponseServerError("No owner defined for session")

    if not instance.owner.is_active:
        return HttpResponseServerError("Owner for session is not active")

    if not instance.is_running():
        update_session_status(instance.name, "Allocated", instance.owner)
        report_analytics_event(instance, "Session/Started")
        instance.mark_as_running()

        def _schedule_resource_creation():
            create_request_resources(instance)

        transaction.on_commit(_schedule_resource_creation)

    login(request, instance.owner, backend=settings.AUTHENTICATION_BACKENDS[0])

    return redirect("workshops_session", name=instance.name)


@protected_resource()
@require_http_methods(["GET"])
def session_terminate(request, name):
    """Triggers termination of a workshop session."""

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # Ensure that the session exists.

    instance = portal.allocated_session(name)

    if not instance:
        raise Http404("Session does not exist")

    # Check that session is allocated and in use.

    if not instance.is_allocated():
        return HttpResponseBadRequest("Session is not currently in use")

    token = request.auth

    if token and token.application and not token.user:
        pass
    elif request.user.groups.filter(name="robots").exists():
        pass
    else:
        if instance.owner != request.user:
            return HttpResponseForbidden("Access to session not permitted")

    instance.mark_as_stopping()

    report_analytics_event(instance, "Session/Stopping")

    transaction.on_commit(lambda: delete_workshop_session(instance).schedule())

    details = {}

    details["started"] = instance.started
    details["expires"] = instance.expires

    return JsonResponse(details)


@login_required(login_url="/")
@require_http_methods(["GET"])
@resources_lock
@transaction.atomic
def session_delete(request, name):
    """Triggers deletion of a workshop session."""

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # First check is there is a stopped session for the user.

    instance = portal.stopped_session(name, request.user)

    if instance and instance.index_url:
        # If there is a stopped session and an index URL is set then redirect to
        # that URL. This can occur when session times out and the user attempts
        # to terminate the session afterwards.

        return redirect(
            update_query_params(instance.index_url, {"notification": "session-deleted"})
        )

    # Now check if there is allocated session for the user.

    instance = portal.allocated_session(name, request.user)

    if not instance:
        if not request.user.is_staff and settings.PORTAL_INDEX:
            return redirect(
                update_query_params(
                    settings.PORTAL_INDEX, {"notification": "session-invalid"}
                )
            )

        return redirect(
            update_query_params(
                reverse("workshops_catalog"), {"notification": "session-invalid"}
            )
        )

    # Mark the instance as stopping now so that it will not be picked up
    # by the user again if they attempt to create a new session immediately.

    instance.mark_as_stopping()

    report_analytics_event(instance, "Session/Stopping")

    transaction.on_commit(lambda: delete_workshop_session(instance).schedule())

    notification = request.GET.get("notification", None)

    if not notification:
        notification = "session-deleted"

    index_url = instance.index_url

    if index_url:
        return redirect(update_query_params(index_url, {"notification": notification}))

    if not request.user.is_staff and settings.PORTAL_INDEX:
        return redirect(
            update_query_params(settings.PORTAL_INDEX, {"notification": notification})
        )

    return redirect(
        update_query_params(
            reverse("workshops_catalog"), {"notification": notification}
        )
    )


@protected_resource()
@require_http_methods(["GET"])
def session_authorize(request, name):
    """Verifies that the user accessing a workshop session is permitted."""

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # Ensure that the session exists.

    instance = portal.allocated_session(name)

    if not instance:
        raise Http404("Session does not exist")

    # Check that session is allocated and in use.

    if not instance.is_allocated():
        return HttpResponseBadRequest("Session is not currently in use")

    # Check that are owner of session, a robot account, or a staff member.

    token = request.auth

    if token and token.application and not token.user:
        pass
    elif request.user.groups.filter(name="robots").exists():
        pass
    else:
        if instance.owner != request.user:
            return HttpResponseForbidden("Access to session not permitted")

    return JsonResponse(
        {
            "owner": instance.owner.username,
            "user": request.user.get_username(),
            "staff": request.user.is_staff,
        }
    )


@protected_resource()
@require_http_methods(["GET"])
def session_config(request, name):
    """Returns details for accessing the workshop session config."""

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # Ensure that the session exists.

    instance = portal.allocated_session(name)

    if not instance:
        raise Http404("Session does not exist")

    # Check that are owner of session, a robot account, or a staff member.

    token = request.auth

    if token and token.application and not token.user:
        pass
    elif request.user.groups.filter(name="robots").exists():
        pass
    else:
        if instance.owner != request.user:
            return HttpResponseForbidden("Access to session not permitted")

    details = {}

    details["url"] = instance.url
    details["password"] = instance.password

    return JsonResponse(details)


@protected_resource()
@require_http_methods(["GET"])
def session_schedule(request, name):
    """Returns details about how long the workshop session is scheduled."""

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # Ensure that the session exists.

    instance = portal.allocated_session(name)

    if not instance:
        raise Http404("Session does not exist")

    # Check that are owner of session, a robot account, or a staff member.

    token = request.auth

    if token and token.application and not token.user:
        pass
    elif request.user.groups.filter(name="robots").exists():
        pass
    else:
        if instance.owner != request.user:
            return HttpResponseForbidden("Access to session not permitted")

    details = {}

    details["status"] = SessionState(instance.state).name

    details["started"] = instance.started
    details["expires"] = instance.expires

    details["expiring"] = instance.is_expiring()

    remaining = instance.time_remaining()

    if remaining is not None:
        details["countdown"] = remaining
        details["extendable"] = instance.is_extension_permitted()

    return JsonResponse(details)


@protected_resource()
@require_http_methods(["GET"])
def session_extend(request, name):
    """Extends the expiration time for the session where within the last
    period where extension is allowed.

    """

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # Ensure that the session exists.

    instance = portal.allocated_session(name)

    if not instance:
        raise Http404("Session does not exist")

    # Check that session is allocated and in use.

    if not instance.is_allocated():
        return HttpResponseBadRequest("Session is not currently in use")

    # Check that are owner of session, a robot account, or a staff member.

    token = request.auth

    if token and token.application and not token.user:
        pass
    elif request.user.groups.filter(name="robots").exists():
        pass
    else:
        if instance.owner != request.user:
            return HttpResponseForbidden("Access to session not permitted")

    # Session will only be extended if within the last period where extension
    # is allowed.

    time_extended = False

    old_time_remaining = instance.time_remaining()

    if instance.is_extension_permitted():
        time_extended = instance.extend_time_remaining()

    new_time_remaining = instance.time_remaining()

    if time_extended:
        report_analytics_event(instance, "Session/Extended")

    details = {}

    details["status"] = SessionState(instance.state).name

    details["started"] = instance.started
    details["expires"] = instance.expires

    details["extended"] = time_extended
    details["expiring"] = instance.is_expiring()

    if new_time_remaining is not None:
        details["countdown"] = new_time_remaining
        details["extendable"] = instance.is_extension_permitted()

    return JsonResponse(details)


@csrf_exempt
@protected_resource()
@require_http_methods(["POST"])
def session_event(request, name):
    """Report the workshop event to any analytics service via the analytics
    webhook URL.

    """

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # Ensure that the session exists.

    instance = portal.allocated_session(name)

    if not instance:
        raise Http404("Session does not exist")

    # Check that session had been allocated.

    if instance.owner is None:
        return HttpResponseBadRequest("Session was never allocated")

    # Check that are owner of session, a robot account, or a staff member.

    token = request.auth

    if token and token.application and not token.user:
        pass
    elif request.user.groups.filter(name="robots").exists():
        pass
    else:
        if instance.owner != request.user:
            return HttpResponseForbidden("Access to session not permitted")

    # Gather event data and report event.

    if request.content_type != "application/json":
        return HttpResponseBadRequest("No event data provided")

    message = json.loads(request.body)

    event = message.get("event", {}).get("name", "")
    data = message.get("event", {}).get("data", {})

    if not event:
        return HttpResponseBadRequest("No event data provided")

    report_analytics_event(instance, event, data)

    return JsonResponse({})
