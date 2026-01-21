import os
import yaml
import copy

from wrapt import synchronized

from .helpers import xget

from .operator_config import OPERATOR_API_GROUP, OPERATOR_NAME_PREFIX

kyverno_policies = []

if os.path.exists("/opt/app-root/config/kyverno-policies.yaml"):
    with open("/opt/app-root/config/kyverno-policies.yaml") as fp:
        kyverno_policies = list(yaml.load_all(fp.read(), Loader=yaml.Loader))


@synchronized
def kyverno_environment_rules(workshop_spec, environment_name):
    action = xget(workshop_spec, "session.namespaces.security.rules.action", "Enforce")
    exclude = xget(workshop_spec, "session.namespaces.security.rules.exclude", [])

    rules = []

    for clusterpolicy in kyverno_policies:
        policy_name = xget(clusterpolicy, "metadata.name")
        policy_rules = xget(clusterpolicy, "spec.rules", [])

        for index, rule in enumerate(policy_rules, start=1):
            rule = copy.deepcopy(rule)

            if len(policy_rules) != 1:
                rule["name"] = f"{policy_name}/{index}"
            else:
                rule["name"] = policy_name

            # Add a namespace selector to each resource which is a target of the
            # rule. This is to ensure that the rule is only applied to the
            # namespaces which are created for a workshop session.

            resources = [condition.get("resources", {}) for condition in xget(rule, "match.all", [])]

            if not resources:
                resources = [condition.get("resources", {}) for condition in xget(rule, "match.any", [])]

            if not resources:
                resources = [xget(rule, "match.resources", {})]

            if not resources:
                continue

            for resource in resources:
                resource["namespaceSelector"] = {
                    "matchExpressions": [
                        {
                            "key": f"training.{OPERATOR_API_GROUP}/environment.name",
                            "operator": "In",
                            "values": [environment_name],
                        },
                        {
                            "key": f"training.{OPERATOR_API_GROUP}/component",
                            "operator": "In",
                            "values": ["session"],
                        },
                    ]
                }

            if policy_name not in exclude:
                rules.append(rule)

    if not rules:
        return []

    cluster_policy_body = {
        "apiVersion": "kyverno.io/v1",
        "kind": "ClusterPolicy",
        "metadata": {
            "name": f"{OPERATOR_NAME_PREFIX}-environment-{environment_name}",
        },
        "spec": {
            "validationFailureAction": action,
            "background": True,
            "rules": rules,
        },
    }

    return [cluster_policy_body]
