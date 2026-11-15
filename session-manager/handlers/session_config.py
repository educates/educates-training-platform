"""How a workshop session gets its config password, which unlocks the
session's configuration from the workshop gateway, such as its kubeconfig
and SSH private key.

A training portal passes the password in a Secret in the workshop
environment's namespace, which the WorkshopSession names in
`spec.session.config.passwordSecretRef.name`, under the key `password`, so
that reading WorkshopSessions reveals no session credential. The session
Deployment takes it from that Secret too. Until the Secret exists and holds
the password, session-manager retries the session.

A WorkshopSession made by hand may instead give the password itself in
`spec.session.config.password`, and when it gives neither, session-manager
generates one. The session Deployment then carries it as a literal value. A
reference takes precedence over a password given in the spec.
"""

import base64

import kopf

CONFIG_PASSWORD_ENV = "CONFIG_PASSWORD"

CONFIG_PASSWORD_KEY = "password"

# A training portal creates the Secret right after the WorkshopSession, so
# session-manager can see the session first. It retries soon after.

CONFIG_PASSWORD_RETRY_DELAY = 5


def session_config_password(session, read_secret, generate_password):
    """Returns the config password of the workshop session whose
    `spec.session` is given, and the session Deployment's environment entry
    for it.

    `read_secret(name)` returns the Secret of that name in the workshop
    environment's namespace, as the API returns it, or None when there is
    none. `generate_password()` returns a new random password.

    """

    config = session.get("config", {})

    secret_name = config.get("passwordSecretRef", {}).get("name", "")

    if secret_name:
        secret = read_secret(secret_name)

        if secret is None:
            raise kopf.TemporaryError(
                f"Config password Secret {secret_name} not yet available.",
                delay=CONFIG_PASSWORD_RETRY_DELAY,
            )

        encoded = (secret.get("data") or {}).get(CONFIG_PASSWORD_KEY, "")

        password = base64.b64decode(encoded).decode("utf-8")

        if not password:
            raise kopf.TemporaryError(
                f"Config password Secret {secret_name} has no {CONFIG_PASSWORD_KEY}.",
                delay=CONFIG_PASSWORD_RETRY_DELAY,
            )

        return password, {
            "name": CONFIG_PASSWORD_ENV,
            "valueFrom": {
                "secretKeyRef": {"name": secret_name, "key": CONFIG_PASSWORD_KEY}
            },
        }

    password = config.get("password", "") or generate_password()

    return password, {"name": CONFIG_PASSWORD_ENV, "value": password}
