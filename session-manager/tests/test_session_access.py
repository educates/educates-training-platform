"""Unit tests for the objects session-manager creates in a session
namespace so that the session's service account, and the console running
as it, can reach what they need there whatever the session's role.

Attendees reach their own namespace through a RoleBinding in it, which
grants ``get`` on that namespace and never list. The console is given its
settings up front, since without namespace list it cannot work out which
namespaces to offer.
"""

import json

from handlers.session_access import (
    console_settings,
    console_settings_config_map,
    session_namespace_role_binding,
)

LABELS = {
    "training.educates.dev/component": "session",
    "training.educates.dev/session.name": "lab-w01-s001",
}


def test_binds_the_session_service_account_to_get_its_own_namespace():
    body = session_namespace_role_binding(
        session_namespace="lab-w01-s001",
        service_account="lab-w01-s001",
        workshop_namespace="lab-w01",
        labels=LABELS,
    )

    assert body == {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": "educates-session-namespace",
            "namespace": "lab-w01-s001",
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/session.name": "lab-w01-s001",
            },
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": "educates:session:namespace",
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": "lab-w01-s001",
                "namespace": "lab-w01",
            }
        ],
    }


# The console reads its settings from the kubernetes-dashboard-settings
# ConfigMap as a whole object: a field left out becomes zero, and an
# itemsPerPage of 0 empties every list. The eight fields, and the dashboard's
# own defaults for the first five, come from the Settings type of Kubernetes
# dashboard v2.7.0.

DASHBOARD_SETTINGS_FIELDS = {
    "clusterName",
    "itemsPerPage",
    "labelsLimit",
    "logsAutoRefreshTimeInterval",
    "resourceAutoRefreshTimeInterval",
    "disableAccessDeniedNotifications",
    "defaultNamespace",
    "namespaceFallbackList",
}


def test_console_settings_set_every_field_the_dashboard_reads():
    settings = console_settings("lab-w01-s001", [])

    assert set(settings) == DASHBOARD_SETTINGS_FIELDS


def test_console_settings_open_on_the_session_namespace_and_offer_its_namespaces():
    settings = console_settings(
        "lab-w01-s001", ["lab-w01-s001-apps", "lab-w01-s001-db"]
    )

    assert settings == {
        "clusterName": "",
        "itemsPerPage": 10,
        "labelsLimit": 3,
        "logsAutoRefreshTimeInterval": 5,
        "resourceAutoRefreshTimeInterval": 5,
        "disableAccessDeniedNotifications": True,
        "defaultNamespace": "lab-w01-s001",
        "namespaceFallbackList": [
            "lab-w01-s001",
            "lab-w01-s001-apps",
            "lab-w01-s001-db",
        ],
    }


def test_console_settings_offer_only_the_session_namespace_without_secondary_ones():
    settings = console_settings("lab-w01-s001", [])

    assert settings["defaultNamespace"] == "lab-w01-s001"
    assert settings["namespaceFallbackList"] == ["lab-w01-s001"]


def test_console_settings_config_map_holds_the_settings_as_the_dashboard_reads_them():
    body = console_settings_config_map(
        session_namespace="lab-w01-s001",
        secondary_namespaces=["lab-w01-s001-apps"],
        labels=LABELS,
    )

    assert body["apiVersion"] == "v1"
    assert body["kind"] == "ConfigMap"
    assert body["metadata"] == {
        "name": "kubernetes-dashboard-settings",
        "namespace": "lab-w01-s001",
        "labels": {
            "training.educates.dev/component": "session",
            "training.educates.dev/session.name": "lab-w01-s001",
        },
    }
    assert set(body["data"]) == {"_global"}
    assert json.loads(body["data"]["_global"]) == {
        "clusterName": "",
        "itemsPerPage": 10,
        "labelsLimit": 3,
        "logsAutoRefreshTimeInterval": 5,
        "resourceAutoRefreshTimeInterval": 5,
        "disableAccessDeniedNotifications": True,
        "defaultNamespace": "lab-w01-s001",
        "namespaceFallbackList": ["lab-w01-s001", "lab-w01-s001-apps"],
    }
