import random
import string
import base64

from .operator_config import INGRESS_DOMAIN, INGRESS_PROTOCOL
from .helpers import substitute_variables


def git_workshop_spec_patches(workshop_spec, application_properties):
    characters = string.ascii_letters + string.digits

    git_host = f"git-$(session_name).{INGRESS_DOMAIN}"
    git_username = "$(session_name)"
    git_password = "$(services_password)"

    git_auth_token = "$(base64($(git_username):$(git_password)))"

    return {
        "spec": {
            "session": {
                "ingresses": [
                    {"name": "git", "port": 10087, "authentication": {"type": "none"}}
                ],
                "variables": [
                    {
                        "name": "git_protocol",
                        "value": INGRESS_PROTOCOL,
                    },
                    {
                        "name": "git_host",
                        "value": git_host,
                    },
                    {
                        "name": "git_username",
                        "value": git_username,
                    },
                    {
                        "name": "git_password",
                        "value": git_password,
                    },
                    {
                        "name": "git_auth_token",
                        "value": git_auth_token,
                    },
                ],
                "env": [
                    {
                        "name": "GIT_PROTOCOL",
                        "value": INGRESS_PROTOCOL,
                    },
                    {
                        "name": "GIT_HOST",
                        "value": git_host,
                    },
                    {
                        "name": "GIT_USERNAME",
                        "value": git_username,
                    },
                    {
                        "name": "GIT_PASSWORD",
                        "value": git_password,
                    },
                    {
                        "name": "GIT_AUTH_TOKEN",
                        "value": git_auth_token,
                    },
                ],
            }
        }
    }


def git_environment_objects_list(workshop_spec, application_properties):
    return []


def git_session_objects_list(workshop_spec, application_properties):
    return []


def git_pod_template_spec_patches(workshop_spec, application_properties):
    return {}
