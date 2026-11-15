"""Unit tests for how session-manager picks a workshop session's config
password, which unlocks the session's configuration from the workshop
gateway, and the session Deployment's environment entry for it.

A training portal passes the password in a Secret in the workshop
environment's namespace, which the WorkshopSession names in
``spec.session.config.passwordSecretRef.name``, under the key ``password``.
A WorkshopSession made by hand may give the password itself in
``spec.session.config.password``. When it gives neither, session-manager
generates one.
"""

import base64

import kopf
import pytest

from handlers.session_config import session_config_password


def _secret(name, data):
    return {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {"name": name, "namespace": "event-w01"},
        "data": {
            key: base64.b64encode(value.encode("utf-8")).decode("ascii")
            for key, value in data.items()
        },
    }


def _secrets(*bodies):
    by_name = {body["metadata"]["name"]: body for body in bodies}

    return by_name.get


def _generate():
    return "generated-password"


def test_reads_a_referenced_password_and_passes_it_by_secret_key_reference():
    session = {
        "id": "s001",
        "config": {"passwordSecretRef": {"name": "event-w01-s001-config-password"}},
    }
    read_secret = _secrets(
        _secret("event-w01-s001-config-password", {"password": "from-the-secret"})
    )

    password, env = session_config_password(session, read_secret, _generate)

    assert password == "from-the-secret"
    assert env == {
        "name": "CONFIG_PASSWORD",
        "valueFrom": {
            "secretKeyRef": {
                "name": "event-w01-s001-config-password",
                "key": "password",
            }
        },
    }


def test_prefers_a_reference_to_a_password_given_in_the_spec():
    session = {
        "id": "s001",
        "config": {
            "password": "given-in-the-spec",
            "passwordSecretRef": {"name": "event-w01-s001-config-password"},
        },
    }
    read_secret = _secrets(
        _secret("event-w01-s001-config-password", {"password": "from-the-secret"})
    )

    password, env = session_config_password(session, read_secret, _generate)

    assert password == "from-the-secret"
    assert env["valueFrom"]["secretKeyRef"] == {
        "name": "event-w01-s001-config-password",
        "key": "password",
    }


def test_uses_a_password_given_in_the_spec_as_a_literal_value():
    session = {"id": "s001", "config": {"password": "given-in-the-spec"}}

    password, env = session_config_password(session, _secrets(), _generate)

    assert password == "given-in-the-spec"
    assert env == {"name": "CONFIG_PASSWORD", "value": "given-in-the-spec"}


def test_generates_a_password_when_the_session_gives_none():
    for session in ({"id": "s001"}, {"id": "s001", "config": {"password": ""}}):
        password, env = session_config_password(session, _secrets(), _generate)

        assert password == "generated-password"
        assert env == {"name": "CONFIG_PASSWORD", "value": "generated-password"}


def test_retries_while_the_referenced_secret_does_not_exist():
    session = {
        "id": "s001",
        "config": {"passwordSecretRef": {"name": "event-w01-s001-config-password"}},
    }

    with pytest.raises(kopf.TemporaryError):
        session_config_password(session, _secrets(), _generate)


def test_retries_while_the_referenced_secret_holds_no_password():
    session = {
        "id": "s001",
        "config": {"passwordSecretRef": {"name": "event-w01-s001-config-password"}},
    }

    for data in ({}, {"password": ""}, {"other": "value"}):
        read_secret = _secrets(_secret("event-w01-s001-config-password", data))

        with pytest.raises(kopf.TemporaryError):
            session_config_password(session, read_secret, _generate)
