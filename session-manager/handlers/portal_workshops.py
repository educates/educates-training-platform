"""Which workshops a training portal may deploy.

A training portal labels each workshop environment it creates with its own
name. An admission policy keeps a portal from labelling one with another
portal's name, and a portal cannot change its own TrainingPortal, so
session-manager trusts the label and deploys a portal-labelled environment
only for a workshop that portal's TrainingPortal lists in `spec.workshops`.
"""

from .portal_labels import portal_label


def portal_workshop_refusal(environment, training_portal):
    """Returns why the workshop environment may not be deployed, or None
    when it may. The training portal is the TrainingPortal the environment's
    portal label names, or None when it does not exist. An environment
    without a portal label is no portal's, and may be deployed. The workshop
    is matched by the name of its Workshop resource, which is what an
    environment names, never by the alias a portal may give it.

    """

    portal_name = portal_label(environment)

    if not portal_name:
        return None

    workshop_name = environment["spec"]["workshop"]["name"]

    if training_portal is None:
        return (
            f"Workshop {workshop_name} is not listed by training portal"
            f" {portal_name}, which does not exist."
        )

    workshops = training_portal.get("spec", {}).get("workshops") or []

    listed = {workshop["name"] for workshop in workshops}

    if workshop_name in listed:
        return None

    return f"Workshop {workshop_name} is not listed by training portal {portal_name}."
