"""Unit tests for naming the platform's image pull Secrets on a
ServiceAccount before the session manager creates or updates it.

A pod takes its ServiceAccount's image pull secrets only when it is
created, so the session manager names them on the ServiceAccounts it
creates rather than leaving it to the SecretInjector, which may reach a
ServiceAccount only after a pod using it exists.
"""

from handlers.helpers import (
    add_image_pull_secrets,
    gets_image_pull_secrets,
    vcluster_namespace_names,
    without_vcluster_application_label,
)


def _service_account(**fields):
    body = {"apiVersion": "v1", "kind": "ServiceAccount", "metadata": {"name": "sa"}}
    body.update(fields)
    return body


def test_names_the_secrets_on_a_service_account():
    body = _service_account()

    assert add_image_pull_secrets(body, ["mirror"]) is True
    assert body["imagePullSecrets"] == [{"name": "mirror"}]


def test_keeps_the_service_accounts_own_secrets_and_adds_each_name_once():
    body = _service_account(imagePullSecrets=[{"name": "own"}, {"name": "mirror"}])

    assert add_image_pull_secrets(body, ["mirror", "second"]) is True
    assert body["imagePullSecrets"] == [
        {"name": "own"},
        {"name": "mirror"},
        {"name": "second"},
    ]


def test_reports_no_change_when_every_name_is_already_there():
    body = _service_account(imagePullSecrets=[{"name": "mirror"}])

    assert add_image_pull_secrets(body, ["mirror"]) is False
    assert body["imagePullSecrets"] == [{"name": "mirror"}]


def test_leaves_a_service_account_alone_when_no_names_are_configured():
    body = _service_account()

    assert add_image_pull_secrets(body, []) is False
    assert "imagePullSecrets" not in body


def test_leaves_other_kinds_alone():
    body = {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": "d"}}

    assert add_image_pull_secrets(body, ["mirror"]) is False
    assert "imagePullSecrets" not in body


# Which objects of a workshop session get the platform's image pull Secrets
# named on them: every one while session namespaces get the Secrets, and
# otherwise only the namespace of the session's vcluster and what is in it,
# which the SecretCopier still copies the Secrets into.


def _namespace(name, **labels):
    return {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": name, "labels": labels}}


def _in_namespace(namespace):
    return _service_account() | {"metadata": {"name": "sa", "namespace": namespace}}


VCLUSTER = {"training.educates.dev/application": "vcluster"}


def test_finds_the_vcluster_namespaces_among_the_application_objects():
    objects = [
        _namespace("lab-s001-vc", **VCLUSTER),
        _namespace("lab-s001-other"),
        _in_namespace("lab-s001-vc"),
    ]

    assert vcluster_namespace_names(objects) == {"lab-s001-vc"}


def test_every_session_object_gets_them_while_session_namespaces_do():
    assert gets_image_pull_secrets(_in_namespace("lab-s001"), set(), True) is True
    assert gets_image_pull_secrets(_namespace("lab-s001-extra"), set(), True) is True


def test_only_the_vcluster_namespace_and_its_objects_get_them_otherwise():
    vcluster = {"lab-s001-vc"}

    assert gets_image_pull_secrets(_namespace("lab-s001-vc", **VCLUSTER), vcluster, False) is True
    assert gets_image_pull_secrets(_in_namespace("lab-s001-vc"), vcluster, False) is True
    assert gets_image_pull_secrets(_namespace("lab-s001-extra"), vcluster, False) is False
    assert gets_image_pull_secrets(_in_namespace("lab-s001"), vcluster, False) is False


def test_a_workshop_namespace_cannot_claim_to_be_a_vcluster_namespace():
    # The SecretCopier selects vcluster namespaces by this label, so a
    # namespace a workshop provides must not carry it, or it would get
    # the Secrets while session namespaces do not.
    body = _namespace("lab-s001-extra", **VCLUSTER, team="a")

    without_vcluster_application_label(body)

    assert body["metadata"]["labels"] == {"team": "a"}


def test_leaves_other_application_labels_alone():
    body = _namespace("lab-s001-extra", **{"training.educates.dev/application": "workshop"})

    without_vcluster_application_label(body)

    assert body["metadata"]["labels"] == {"training.educates.dev/application": "workshop"}
