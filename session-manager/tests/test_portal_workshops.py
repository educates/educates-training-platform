"""Unit tests for which workshops a training portal may deploy.

A training portal labels each workshop environment it creates with its own
name, and an admission policy keeps it from labelling one with another
portal's name. session-manager deploys a portal-labelled environment only
for a workshop that portal's TrainingPortal lists in ``spec.workshops``.
"""

from handlers.portal_workshops import portal_workshop_refusal


def _environment(workshop, portal=None):
    labels = {}

    if portal is not None:
        labels["training.educates.dev/portal.name"] = portal

    return {
        "apiVersion": "training.educates.dev/v1beta1",
        "kind": "WorkshopEnvironment",
        "metadata": {"name": "event-w01", "labels": labels},
        "spec": {"workshop": {"name": workshop}},
    }


def _training_portal(name, *workshops):
    return {
        "apiVersion": "training.educates.dev/v1beta1",
        "kind": "TrainingPortal",
        "metadata": {"name": name},
        "spec": {"workshops": [{"name": workshop} for workshop in workshops]},
    }


def test_deploys_a_workshop_the_portal_lists():
    environment = _environment("lab-k8s-fundamentals", portal="event")
    portal = _training_portal("event", "lab-markdown-sample", "lab-k8s-fundamentals")

    assert portal_workshop_refusal(environment, portal) is None


def test_refuses_a_workshop_the_portal_does_not_list():
    environment = _environment("lab-cluster-admin", portal="event")
    portal = _training_portal("event", "lab-markdown-sample", "lab-k8s-fundamentals")

    assert (
        portal_workshop_refusal(environment, portal)
        == "Workshop lab-cluster-admin is not listed by training portal event."
    )


def test_refuses_any_workshop_for_a_portal_that_lists_none():
    environment = _environment("lab-k8s-fundamentals", portal="event")
    portal = _training_portal("event")
    del portal["spec"]["workshops"]

    assert (
        portal_workshop_refusal(environment, portal)
        == "Workshop lab-k8s-fundamentals is not listed by training portal event."
    )


def test_matches_the_workshop_by_name_not_by_alias():
    # A portal names the Workshop resource in its environments, and an alias
    # only changes how the portal itself refers to the workshop.

    portal = _training_portal("event", "lab-k8s-fundamentals")
    portal["spec"]["workshops"][0]["alias"] = "lab-cluster-admin"

    assert (
        portal_workshop_refusal(
            _environment("lab-k8s-fundamentals", portal="event"), portal
        )
        is None
    )
    assert (
        portal_workshop_refusal(
            _environment("lab-cluster-admin", portal="event"), portal
        )
        == "Workshop lab-cluster-admin is not listed by training portal event."
    )


def test_refuses_an_environment_whose_portal_does_not_exist():
    # With no TrainingPortal, the workshop is in no list.

    environment = _environment("lab-k8s-fundamentals", portal="event")

    assert portal_workshop_refusal(environment, None) == (
        "Workshop lab-k8s-fundamentals is not listed by training portal event,"
        " which does not exist."
    )


def test_deploys_an_environment_no_portal_created():
    # A workshop environment created by hand, with no portal label, is not a
    # portal's, so no portal's list applies to it.

    environment = _environment("lab-cluster-admin")

    assert portal_workshop_refusal(environment, None) is None


def test_deploys_an_environment_whose_portal_label_is_empty():
    # session-manager treats an empty portal label as no portal throughout,
    # and the admission policy refuses one from a portal.

    environment = _environment("lab-cluster-admin", portal="")

    assert portal_workshop_refusal(environment, None) is None
