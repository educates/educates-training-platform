"""The label naming the training portal an object belongs to.

A training portal labels each workshop environment it creates with its own
name, which an admission policy keeps it from setting to another portal's
name, so session-manager trusts the label to say which portal an environment
is for.
"""

PORTAL_NAME_LABEL = "training.educates.dev/portal.name"


def portal_label(obj):
    """Returns the name of the training portal the object's portal label
    names, or an empty string when the object has no portal label.

    """

    return obj["metadata"].get("labels", {}).get(PORTAL_NAME_LABEL, "")
