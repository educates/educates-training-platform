"""Defines the view handler for the terminal page shown when a workshop session
has finished.

"""

__all__ = ["finished"]

from django.shortcuts import render
from django.views.decorators.http import require_http_methods
from django.contrib.auth import logout


@require_http_methods(["GET"])
def finished(request):
    """Terminal page reached when a workshop session has finished. Signs out the
    current user so that an anonymous user created for a workshop session is not
    inherited by a subsequent visit to the training portal, then renders a
    message indicating the session is over. This is intentionally a dead end and
    provides no link back into the portal.

    """

    if request.user.is_authenticated:
        logout(request)

    notification = request.GET.get("notification", "session-deleted").strip()

    return render(request, "workshops/finished.html", {"notification": notification})
