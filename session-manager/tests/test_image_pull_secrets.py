"""Unit tests for naming the platform's image pull Secrets on a
ServiceAccount before the session manager creates or updates it.

A pod takes its ServiceAccount's image pull secrets only when it is
created, so the session manager names them on the ServiceAccounts it
creates rather than leaving it to the SecretInjector, which may reach a
ServiceAccount only after a pod using it exists.
"""

from handlers.helpers import add_image_pull_secrets


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

