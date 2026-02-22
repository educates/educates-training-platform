"""Defines view handlers for working with environments via the web interface."""

__all__ = [
    "environment",
    "environment_create",
    "environment_status",
    "environment_request",
]

import copy
import uuid
import string
import random
import json

from django.shortcuts import redirect, reverse
from django.contrib.auth.decorators import login_required
from django.views.decorators.csrf import csrf_exempt
from django.views.decorators.http import require_http_methods
from django.contrib.auth.models import Group
from django.contrib.auth import get_user_model
from django.http import HttpResponseForbidden, HttpResponseBadRequest
from django.core.validators import validate_email
from django.core.exceptions import ValidationError
from django.utils.http import urlencode
from django.http import JsonResponse
from django.db import transaction
from django.contrib.auth import login
from django.conf import settings

from oauth2_provider.decorators import protected_resource

from ..manager.analytics import report_analytics_event
from ..manager.sessions import retrieve_session_for_user
from ..manager.locking import resources_lock
from ..models import TrainingPortal, Environment, EnvironmentState, SessionState
from .helpers import update_query_params


@login_required
@require_http_methods(["GET"])
@resources_lock
@transaction.atomic
def environment(request, name):
    """Initiate creation of a workshop session against the specific workshop
    environment.

    """

    index_url = request.GET.get("index_url", "").strip()

    # Ensure there is an environment with the specified name in existance.

    try:
        instance = Environment.objects.get(name=name)
    except Environment.DoesNotExist:
        if index_url:
            return redirect(
                update_query_params(index_url, {"notification": "workshop-invalid"})
            )

        if not request.user.is_staff and settings.PORTAL_INDEX:
            return redirect(
                update_query_params(
                    settings.PORTAL_INDEX, {"notification": "workshop-invalid"}
                )
            )

        return redirect(
            update_query_params(
                reverse("workshops_catalog"), {"notification": "workshop-invalid"}
            )
        )

    # Retrieve a session for the user for this workshop environment.

    session = retrieve_session_for_user(instance, request.user, index_url=index_url)

    if session:
        return redirect("workshops_session", name=session.name)

    if index_url:
        return redirect(
            update_query_params(index_url, {"notification": "session-unavailable"})
        )

    if not request.user.is_staff and settings.PORTAL_INDEX:
        return redirect(
            update_query_params(
                settings.PORTAL_INDEX, {"notification": "session-unavailable"}
            )
        )

    return redirect(
        update_query_params(
            reverse("workshops_catalog"), {"notification": "session-unavailable"}
        )
    )


@require_http_methods(["GET"])
@resources_lock
@transaction.atomic
def environment_create(request, name):
    """Direct URL that can be used to create workshop sessions. Will redirect
    to login page if necessary, otherwise redirects to the view handler that
    triggers the actual creation of the workshop session for the specified
    workshop environment.

    """

    index_url = request.GET.get("index_url", "").strip()

    # Where the user is already authenticated, redirect immediately to
    # endpoint which actually triggers creation of environment. It will
    # validate if it is a correct environment name.

    if request.user.is_authenticated:
        return redirect(
            update_query_params(
                reverse("workshops_environment", args=(name,)),
                {"index_url": index_url},
            )
        )

    # Where anonymous access is not enabled, need to redirect back to the
    # login page and they will need to first login.

    if (
        settings.ENABLE_REGISTRATION != "true"
        or settings.REGISTRATION_TYPE != "anonymous"
    ):
        return redirect("login")

    # Is anonymous access, so we can login the user automatically.

    created = False

    User = get_user_model()  # pylint: disable=invalid-name

    while not created:
        username = f"anon@educates:{uuid.uuid4()}"
        user, created = User.objects.get_or_create(username=username)

    group, _ = Group.objects.get_or_create(name="anonymous")

    user.groups.add(group)

    login(request, user, backend=settings.AUTHENTICATION_BACKENDS[0])

    report_analytics_event(user, "User/Create", {"group": "anonymous"})

    # Finally redirect to endpoint which actually triggers creation of
    # environment. It will validate if it is a correct environment name.

    return redirect(
        update_query_params(
            reverse("workshops_environment", args=(name,)),
            {"index_url": index_url},
        )
    )


@csrf_exempt
@protected_resource()
@require_http_methods(["GET"])
@resources_lock
@transaction.atomic
def environment_status(request, name):
    """Return the status of the workshop environment, including the number of
    workshop sessions currently running.

    """

    # Only allow user who is in the robots group to request session.

    token = request.auth

    if token and token.application and not token.user:
        pass
    elif request.user.groups.filter(name="robots").exists():
        pass
    else:
        return HttpResponseForbidden("Status requests not permitted")

    # XXX What if the portal configuration doesn't exist as process
    # hasn't been initialized yet. Should return error indicating the
    # service is not available.

    portal = TrainingPortal.objects.get(name=settings.TRAINING_PORTAL)

    # Ensure there is an environment which the specified name in existance.

    try:
        environment = Environment.objects.get(name=name)
    except Environment.DoesNotExist:
        return HttpResponseForbidden("Environment does not exist")

    # If user is authenticated and a robot account, allow for inclusion
    # of sessions to be included.

    include_sessions = False

    if request.user.is_authenticated:
        if request.user.groups.filter(name="robots").exists():
            include_sessions = request.GET.get("sessions", "").lower() in (
                "true",
                "1",
            )

    # Retrieve the environment details.

    labels = copy.deepcopy(portal.default_labels)
    labels.update(environment.workshop.labels)
    labels.update(environment.labels)

    details = {}

    details["name"] = environment.name
    details["state"] = EnvironmentState(environment.state).name

    details["workshop"] = {
        "name": environment.workshop.name,
        "title": environment.workshop.title,
        "description": environment.workshop.description,
        "vendor": environment.workshop.vendor,
        "authors": environment.workshop.authors,
        "difficulty": environment.workshop.difficulty,
        "duration": environment.workshop.duration,
        "tags": environment.workshop.tags,
        "labels": labels,
        "logo": environment.workshop.logo,
        "url": environment.workshop.url,
    }

    if include_sessions:
        sessions_data = []

        for session in environment.allocated_sessions():
            session_data = {
                "name": session.name,
                "state": SessionState(session.state).name,
                "namespace": session.name,
                "user": session.owner.username,
                "started": session.started,
            }

            if session.expires:
                session_data["expires"] = session.expires

            remaining = session.time_remaining()

            if remaining is not None:
                session_data["countdown"] = remaining
                session_data["extendable"] = session.is_extension_permitted()

            sessions_data.append(session_data)

        details["sessions"] = sessions_data

    details["duration"] = int(environment.expires.total_seconds())

    details["capacity"] = environment.capacity
    details["reserved"] = environment.reserved

    details["allocated"] = environment.allocated_sessions_count()
    details["available"] = environment.available_sessions_count()

    return JsonResponse(details)


@csrf_exempt
@protected_resource()
@require_http_methods(["GET", "POST"])
@resources_lock
@transaction.atomic
def environment_request(request, name):
    """URL for requesting creation of a workshop session against a specific
    workshop environment, via the REST API.

    """

    # Only allow user who is in the robots group to request session.

    token = request.auth

    if token and token.application and not token.user:
        pass
    elif request.user.groups.filter(name="robots").exists():
        pass
    else:
        return HttpResponseForbidden("Session requests not permitted")

    # Ensure there is an environment which the specified name in existance.

    try:
        instance = Environment.objects.get(name=name)
    except Environment.DoesNotExist:
        return HttpResponseForbidden("Environment does not exist")

    # Extract required parameters for creating the session. Check against
    # redirect_url, firstname and lastname is for backward compatibility
    # and will be removed in the future.

    index_url = request.GET.get("index_url", "").strip()

    if not index_url:
        index_url = request.GET.get("redirect_url", "").strip()

    if not index_url:
        return HttpResponseBadRequest("Need redirect URL for workshop index")

    username = request.GET.get("user", "").strip()

    if not username:
        username = uuid.uuid4()

    email = request.GET.get("email", "").strip()

    if email:
        try:
            validate_email(email)
        except ValidationError:
            return HttpResponseBadRequest("Invalid email address provided")

    first_name = request.GET.get("first_name", "").strip()
    last_name = request.GET.get("last_name", "").strip()

    if not first_name:
        first_name = request.GET.get("firstname", "").strip()

    if not last_name:
        last_name = request.GET.get("lastname", "").strip()

    user_details = {}

    if email:
        user_details["email"] = email

    if first_name:
        user_details["first_name"] = first_name

    if last_name:
        user_details["last_name"] = last_name

    analytics_url = request.GET.get("analytics_url", "").strip()

    session_name = request.GET.get("session")

    # The timeout here in seconds is how long the workshop session will be
    # retained while waiting for it to be activated as a result of the URL
    # returned by the REST API call being visited by a user. This technically
    # could be set much higher if for example a frontend portal didn't return
    # the URL to a user immediately, but instead waited to see if the workshop
    # session was actually ready by making requests against it to get the
    # configuration. Not that the URL should be visited before any startup
    # timeout for a workshop session expires otherwise would fail at that
    # point as the workshop session would have been deleted. As a result, if
    # it is known that a workshop session takes a long time to be ready, then
    # the startup timeout should be set a bit longer than this timeout.

    timeout = int(request.GET.get("timeout", "60").strip())

    # Extract any request parameters from the request body for using in late
    # binding of workshop session configuration.

    params = []

    if request.body:
        try:
            request_body = request.body.decode("utf-8")
            request_data = json.loads(request_body)

        except json.JSONDecodeError as e:
            return HttpResponseBadRequest("Invalid JSON request payload")

        # Do some rudimentary input validation so can respond with an error
        # straight away rather than internally failing later. Not that we do
        # not raise an error if get an unexpected input value though, they will
        # just be ignored later.

        if not isinstance(request_data, dict):
            return HttpResponseBadRequest("Malformed JSON request payload")

        request_params = request_data.get("parameters", [])

        if not isinstance(request_params, list):
            return HttpResponseBadRequest("Malformed JSON request payload")

        for item in request_params:
            key = item.get("name", "")
            value = item.get("value", "")

            if key:
                if not isinstance(key, str) or not isinstance(value, str):
                    return HttpResponseBadRequest("Malformed JSON request payload")

            else:
                return HttpResponseBadRequest("Malformed JSON request payload")

        params = request_params

    # Check whether a user already has an existing session allocated
    # to them, in which case return that rather than create a new one.

    session = None

    User = get_user_model()  # pylint: disable=invalid-name

    try:
        user = User.objects.get(username=username)
    except User.DoesNotExist:
        user = User.objects.create_user(username, **user_details)
        group, _ = Group.objects.get_or_create(name="anonymous")
        user.groups.add(group)
        user.save()

        report_analytics_event(user, "User/Create", {"group": "anonymous"})

    # Do not allow workshop requests for a target user which has staff status or
    # which is in the robots group.

    if user.is_staff or user.groups.filter(name="robots").exists():
        return HttpResponseBadRequest("Session requests not permitted for user")

    # Retrieve a session for the user for this workshop environment.

    characters = string.ascii_letters + string.digits
    token = "".join(random.sample(characters, 32))

    session = retrieve_session_for_user(
        instance, user, session_name, token, timeout, params, index_url, analytics_url
    )

    if not session:
        return JsonResponse({"error": "No session available"}, status=503)

    # If there is a session but it doesn't have a token associated with it then
    # it wasn't created via the REST API and so cannot be reacquired using the
    # REST API.

    if session and not session.token:
        return JsonResponse({"error": "Cannot be reacquired"}, status=503)

    # The "session" property was replaced by "name" and "session" deprecated.
    # Include "session" for now, but it will be removed in future update.

    details = {}

    details["name"] = session.name

    details["session"] = session.name

    details["user"] = user.get_username()

    details["url"] = (
        reverse("workshops_session_activate", args=(session.name,))
        + "?"
        + urlencode({"token": session.token, "index_url": index_url})
    )

    # The session namespace currently has the same name as the session. Return
    # it as a separate value in case it could be different in the future.

    details["namespace"] = session.name

    details["workshop"] = session.workshop_name()
    details["environment"] = session.environment_name()

    return JsonResponse(details)
