"""Unit tests for the objects session-manager creates in a session
namespace so that the session's service account, and the console running
as it, can reach what they need there whatever the session's role.

Attendees reach their own namespace through a RoleBinding in it, which
grants ``get`` on that namespace and never list. The console is given its
settings up front, since without namespace list it cannot work out which
namespaces to offer, and its Secrets with their keys, so that it only reads
them and works under a role which cannot write Secrets.
"""

import base64
import json

from handlers.session_access import (
    console_csrf_secret,
    console_key_holder_secret,
    console_role_binding,
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


def test_binds_the_session_service_account_to_read_the_console_objects():
    body = console_role_binding(
        session_namespace="lab-w01-s001",
        service_account="lab-w01-s001",
        workshop_namespace="lab-w01",
        labels=LABELS,
    )

    assert body == {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": "educates-session-console",
            "namespace": "lab-w01-s001",
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/session.name": "lab-w01-s001",
            },
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": "educates:session:console",
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


# The console reads its CSRF key from the csrf key of the
# kubernetes-dashboard-csrf Secret, and writes a key of its own back into
# the Secret when the key is empty, which needs update on Secrets.


def test_console_csrf_secret_holds_a_key_so_the_console_never_writes_one():
    body = console_csrf_secret(session_namespace="lab-w01-s001", labels=LABELS)

    assert body["apiVersion"] == "v1"
    assert body["kind"] == "Secret"
    assert body["metadata"] == {
        "name": "kubernetes-dashboard-csrf",
        "namespace": "lab-w01-s001",
        "labels": {
            "training.educates.dev/component": "session",
            "training.educates.dev/session.name": "lab-w01-s001",
        },
    }
    assert set(body["data"]) == {"csrf"}
    assert base64.b64decode(body["data"]["csrf"], validate=True) != b""


def test_console_csrf_secret_holds_a_different_key_for_each_session():
    first = console_csrf_secret(session_namespace="lab-w01-s001", labels=LABELS)
    second = console_csrf_secret(session_namespace="lab-w01-s001", labels=LABELS)

    assert first["data"]["csrf"] != second["data"]["csrf"]


# The console keeps the key it encrypts its tokens with in the
# kubernetes-dashboard-key-holder Secret, and creates the Secret when it is
# missing, which needs create on Secrets. Kubernetes dashboard v2.7.0 reads
# priv as a PKCS#1 RSAPrivateKey and pub as a PKIX SubjectPublicKeyInfo, each
# from the first PEM block whatever its label. The structures checked below
# are those of RFC 8017 appendix A.1 and RFC 5280 section 4.1.

DER_INTEGER = 0x02
DER_BIT_STRING = 0x03
DER_NULL = 0x05
DER_OBJECT_IDENTIFIER = 0x06
DER_SEQUENCE = 0x30

# The DER contents of the rsaEncryption object identifier, 1.2.840.113549.1.1.1.

RSA_ENCRYPTION = bytes.fromhex("2a864886f70d010101")


def der_elements(der):
    """Returns the tag and contents of each DER element in der, in order."""

    elements = []
    while der:
        tag, length, der = der[0], der[1], der[2:]
        if length & 0x80:
            size = length & 0x7F
            length, der = int.from_bytes(der[:size], "big"), der[size:]
        elements.append((tag, der[:length]))
        der = der[length:]
    return elements


def pem_block(data):
    """Returns the bytes of the single PEM block in a Secret's base64 data
    value, whatever the block's label."""

    lines = base64.b64decode(data, validate=True).decode("ascii").strip().splitlines()
    assert lines[0].startswith("-----BEGIN ")
    assert lines[-1].startswith("-----END ")
    return base64.b64decode("".join(lines[1:-1]), validate=True)


def test_console_key_holder_secret_holds_a_private_and_a_public_key():
    body = console_key_holder_secret(session_namespace="lab-w01-s001", labels=LABELS)

    assert body["apiVersion"] == "v1"
    assert body["kind"] == "Secret"
    assert body["metadata"] == {
        "name": "kubernetes-dashboard-key-holder",
        "namespace": "lab-w01-s001",
        "labels": {
            "training.educates.dev/component": "session",
            "training.educates.dev/session.name": "lab-w01-s001",
        },
    }
    assert set(body["data"]) == {"priv", "pub"}


def test_console_key_holder_private_key_is_an_rsa_2048_key_as_pkcs1():
    body = console_key_holder_secret(session_namespace="lab-w01-s001", labels=LABELS)

    [(tag, private_key)] = der_elements(pem_block(body["data"]["priv"]))
    fields = der_elements(private_key)

    # RSAPrivateKey: version, 0 for two primes, modulus, public exponent,
    # private exponent, both primes, both exponents and the coefficient.
    assert tag == DER_SEQUENCE
    assert [tag for tag, _ in fields] == [DER_INTEGER] * 9
    assert fields[0][1] == b"\x00"
    assert int.from_bytes(fields[1][1], "big").bit_length() == 2048


def test_console_key_holder_public_key_is_an_rsa_key_as_pkix():
    body = console_key_holder_secret(session_namespace="lab-w01-s001", labels=LABELS)

    [(tag, public_key_info)] = der_elements(pem_block(body["data"]["pub"]))
    algorithm, (key_tag, key_bits) = der_elements(public_key_info)

    # SubjectPublicKeyInfo: the algorithm, rsaEncryption with no parameters,
    # then a bit string with no unused bits holding the RSAPublicKey, the
    # modulus and public exponent.
    assert tag == DER_SEQUENCE
    assert algorithm == (
        DER_SEQUENCE,
        bytes([DER_OBJECT_IDENTIFIER, len(RSA_ENCRYPTION)])
        + RSA_ENCRYPTION
        + bytes([DER_NULL, 0]),
    )
    assert key_tag == DER_BIT_STRING
    assert key_bits[0] == 0
    [(tag, public_key)] = der_elements(key_bits[1:])
    assert tag == DER_SEQUENCE
    assert [tag for tag, _ in der_elements(public_key)] == [DER_INTEGER] * 2


def test_console_key_holder_public_key_is_the_private_keys_own():
    body = console_key_holder_secret(session_namespace="lab-w01-s001", labels=LABELS)

    [(_, private_key)] = der_elements(pem_block(body["data"]["priv"]))
    [_, (_, key_bits)] = der_elements(der_elements(pem_block(body["data"]["pub"]))[0][1])
    [(_, public_key)] = der_elements(key_bits[1:])

    # The modulus and public exponent, the second and third integers of the
    # private key, are the public key.
    assert der_elements(public_key) == der_elements(private_key)[1:3]


def test_console_key_holder_holds_a_new_key_for_each_session():
    first = console_key_holder_secret(session_namespace="lab-w01-s001", labels=LABELS)
    second = console_key_holder_secret(session_namespace="lab-w01-s001", labels=LABELS)

    assert first["data"]["priv"] != second["data"]["priv"]
