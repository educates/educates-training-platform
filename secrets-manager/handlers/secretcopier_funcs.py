import copy
import fnmatch
import logging

import pykube

from .helpers import lookup


logger = logging.getLogger("educates")


def matches_target_namespace(namespace_name, namespace_obj, configs):
    """Returns all rules which match the namespace passed as argument."""

    for config_obj in configs:
        rules = lookup(config_obj, "spec.rules", [])

        def bound_rule(rule, index):
            rule_snapshot = copy.deepcopy(dict(rule))

            owner_kind = lookup(config_obj, "kind").lower()
            owner_name = lookup(config_obj, "metadata.name")

            rule_snapshot["ownerSource"] = f"{owner_kind}/{owner_name}"

            rule_snapshot["ruleNumber"] = index

            # If this is a secret exporter there is no source secret property in
            # the rule and instead need to populate it using the details from
            # the resource itself. If there is not copy authorization, we need
            # to set it to the uid of the secret copier. We don't set the owner
            # reference when it is a secret exporter as the owner will be the
            # secret importer in the target namespace.

            if lookup(config_obj, "kind") == "SecretExporter":
                rule_snapshot["sourceSecret"] = {
                    "name": lookup(config_obj, "metadata.name"),
                    "namespace": lookup(config_obj, "metadata.namespace"),
                }

                if not lookup(rule, "copyAuthorization"):
                    rule_snapshot["copyAuthorization"] = {
                        "sharedSecret": lookup(config_obj, "metadata.uid")
                    }

            else:
                rule_snapshot["ownerReference"] = {
                    "apiVersion": lookup(config_obj, "apiVersion"),
                    "kind": lookup(config_obj, "kind"),
                    "name": lookup(config_obj, "metadata.name"),
                    "uid": lookup(config_obj, "metadata.uid"),
                    "blockOwnerDeletion": True,
                    "controller": True,
                }

            return rule_snapshot

        for index, rule in enumerate(rules, start=1):
            # Note that as soon as one selector fails where a condition was
            # stipulated, further checks are not done and the namespace is
            # ignored. In other words all conditions much match if more than one
            # is supplied.

            # Check for where name selector is provided and ensure that the
            # namespace is in the list, or is not excluded by a negated entry.
            # If a list is supplied but it isn't in the list, or is prohibited
            # by negation, then we skip to the next one. If no list is supplied
            # a default list of negated entries is used which blocks Kubernetes
            # system namespaces. Matching of names allows for glob wildcards.

            match_names = lookup(rule, "targetNamespaces.nameSelector.matchNames")

            if match_names is None:
                match_names = ["!kube-*"]

            match_exclude_names = list(
                map(lambda _: _[1:], filter(lambda _: _.startswith("!"), match_names))
            )
            match_include_names = list(
                filter(lambda _: not _.startswith("!"), match_names)
            )

            def glob_match_name(name, items):
                for item in items:
                    if fnmatch.fnmatch(name, item):
                        return True
                return False

            if match_include_names and not glob_match_name(
                namespace_name, match_include_names
            ):
                continue

            if match_exclude_names and glob_match_name(
                namespace_name, match_exclude_names
            ):
                continue

            # Check if object uid selector is provided and ensure that uid of
            # the target namespace is in the list. If a list is supplied but it
            # isn't in the list, then skip to the next one.

            match_uids = lookup(rule, "targetNamespaces.uidSelector.matchUIDs", [])

            if match_uids:
                namespace_uid = lookup(namespace_obj, "metadata.uid", "")
                if namespace_uid not in match_uids:
                    continue

            # Check if object owner references are provided and ensure that
            # namespace owner if there is one is in the list, skipping to next
            # one if no match.

            match_owners = lookup(
                rule, "targetNamespaces.ownerSelector.matchOwners", []
            )

            if match_owners:
                owner_references = lookup(namespace_obj, "metadata.ownerReferences", [])
                for owner_reference in owner_references:
                    owner_details = {
                        "apiVersion": owner_reference["apiVersion"],
                        "kind": owner_reference["kind"],
                        "name": owner_reference["name"],
                        "uid": owner_reference["uid"],
                    }
                    if owner_details in match_owners:
                        break
                else:
                    continue

            # Check for where label selector is provided and ensure that all the
            # labels to be matched exist on the target namespace.

            match_labels = lookup(
                rule, "targetNamespaces.labelSelector.matchLabels", {}
            )

            if match_labels:
                matches = True
                namespace_labels = lookup(namespace_obj, "metadata.labels", {})
                for key, value in match_labels.items():
                    if namespace_labels.get(key) != value:
                        matches = False
                        break
                if not matches:
                    continue

            # Check for where label selector is provided but with more general
            # match expressions. Ensure that all the expressions match the
            # labels on the target namespace.

            match_expressions = lookup(
                rule, "targetNamespaces.labelSelector.matchExpressions", []
            )

            if match_expressions:
                matches = True
                namespace_labels = lookup(namespace_obj, "metadata.labels", {})
                for item in match_expressions:
                    key = item["key"]
                    operator = item["operator"]
                    values = item.get("values", [])
                    value = namespace_labels.get(key)
                    if operator == "In":
                        if values:
                            if value is None or value not in values:
                                matches = False
                                break
                    elif operator == "NotIn":
                        if values:
                            if value is not None and value in values:
                                matches = False
                                break
                    elif operator == "Exists":
                        if key not in namespace_labels:
                            matches = False
                            break
                    elif operator == "DoesNotExist":
                        if key in namespace_labels:
                            matches = False
                            break
                if not matches:
                    continue

            yield bound_rule(rule, index)


def matches_source_secret(secret_name, secret_namespace, configs):
    """Returns all configs which match the secret passed as argument."""

    for config_obj in configs:
        rules = lookup(config_obj, "spec.rules", [])

        for rule in rules:
            source_secret_name = lookup(rule, "sourceSecret.name")
            source_secret_namespace = lookup(rule, "sourceSecret.namespace")

            if (
                secret_name == source_secret_name
                and secret_namespace == source_secret_namespace
            ):
                yield config_obj
                continue


def reconcile_namespace(namespace_name, namespace_obj, configs):
    """Perform reconciliation of the specified namespace."""

    if lookup(namespace_obj, "status.phase") != "Active":
        return

    rules = list(matches_target_namespace(namespace_name, namespace_obj, configs))

    if rules:
        update_secrets(namespace_name, rules)


def reconcile_config(config_name, config_obj):
    """Perform reconciliation for the specified config."""

    api = pykube.HTTPClient(pykube.KubeConfig.from_env())

    namespace_query = pykube.Namespace.objects(api)

    for namespace_item in namespace_query:
        if lookup(namespace_item.obj, "status.phase") != "Active":
            continue

        rules = list(
            matches_target_namespace(
                namespace_item.name, namespace_item.obj, [config_obj]
            )
        )

        if rules:
            update_secrets(namespace_item.name, rules)


def reconcile_secret(secret_name, secret_namespace, secret_obj, configs):
    """Perform reconciliation for the specified secret."""

    matched_configs = list(
        matches_source_secret(secret_name, secret_namespace, configs)
    )

    for config_obj in matched_configs:
        reconcile_config(config_obj["metadata"]["name"], config_obj)


def update_secret(namespace_name, rule):
    """Updates a single secret in the specified namespace."""

    api = pykube.HTTPClient(pykube.KubeConfig.from_env())

    owner_source = lookup(rule, "ownerSource")
    rule_number = lookup(rule, "ruleNumber")

    logger.debug(
        f"Processing rule {rule_number} from {owner_source} against namespace {namespace_name}."
    )

    # Read the source secret to be copied or to be used for update. If it
    # doesn't exist, we will fail for just this update. We don't raise an
    # exception as it will break any reconcilation loop being applied at larger
    # context. Even if the target secret name is different, don't copy the
    # secret back to the same namespace.

    source_secret_name = lookup(rule, "sourceSecret.name")
    source_secret_namespace = lookup(rule, "sourceSecret.namespace")

    target_secret_name = lookup(rule, "targetSecret.name", source_secret_name)
    target_secret_namespace = namespace_name

    if source_secret_namespace == target_secret_namespace:
        return

    try:
        source_secret_item = pykube.Secret.objects(
            api, namespace=source_secret_namespace
        ).get(name=source_secret_name)

    except pykube.exceptions.ObjectDoesNotExist:
        logger.debug(f"Secret {source_secret_name} in namespace {source_secret_namespace} does not exist, skipping.")
        return

    except pykube.exceptions.KubernetesError:
        logger.exception(
            f"Secret {source_secret_name} in namespace {source_secret_namespace} cannot be read."
        )
        return

    source_secret_namespace = source_secret_item.obj["metadata"]["namespace"]
    source_secret_name = source_secret_item.obj["metadata"]["name"]

    # Check whether a secret importer has been defined with the same name as the
    # target secret and check any conditions it declares. If a copy
    # authorization is required the secret importer custom resource must exist
    # and the shared secret it holds must match.

    secret_importer_obj = None

    try:
        SecretImporter = pykube.object_factory(
            api, "secrets.educates.dev/v1beta1", "SecretImporter"
        )

        secret_importer_obj = SecretImporter.objects(
            api, namespace=target_secret_namespace
        ).get(name=target_secret_name)

    except pykube.exceptions.ObjectDoesNotExist:
        pass

    except pykube.exceptions.PyKubeError:
        logger.exception(
            f"SecretImporter {target_secret_name} in namespace {target_secret_namespace} cannot be read."
        )
        return

    shared_secret = lookup(rule, "copyAuthorization.sharedSecret")

    if shared_secret is not None:
        if secret_importer_obj is None:
            return

        if (
            secret_importer_obj.obj["spec"]
            .get("copyAuthorization", {})
            .get("sharedSecret")
            != shared_secret
        ):
            logger.warning(
                f"SecretImporter {target_secret_name} in namespace {target_secret_namespace} doesn't match."
            )
            return

    if secret_importer_obj is not None:
        # Check for where name selector is provided and ensure that the source
        # namespace is in the list, or is not excluded by a negated entry. If a
        # list is supplied but it isn't in the list, or is prohibited by
        # negation, then we ignore it. Matching of names allows for glob
        # wildcards.

        match_names = lookup(
            secret_importer_obj.obj["spec"], "sourceNamespaces.nameSelector.matchNames"
        )

        if match_names is None:
            match_names = ["*"]

        match_exclude_names = list(
            map(lambda _: _[1:], filter(lambda _: _.startswith("!"), match_names))
        )
        match_include_names = list(filter(lambda _: not _.startswith("!"), match_names))

        def glob_match_name(name, items):
            for item in items:
                if fnmatch.fnmatch(name, item):
                    return True
            return False

        if match_include_names and not glob_match_name(
            source_secret_namespace, match_include_names
        ):
            return

        if match_exclude_names and glob_match_name(
            source_secret_namespace, match_exclude_names
        ):
            return

    # Now check whether the target secret already exists in the target
    # namespace. If it doesn't exist we just need to copy it, apply any labels
    # and we are done. Fail outright if get any errors besides not being able to
    # find the resource as that indicates a bigger problem.

    reclaim_policy = lookup(rule, "reclaimPolicy", "Delete")

    owner_reference = lookup(rule, "ownerReference")

    if owner_reference is None:
        # If there was no owner reference then it must be a secret exporter and
        # the secret importer needs to be made the owner. It should always exist
        # since for secret exporter a requirement is forced that there is an
        # access token for copy authorization with that being the secret export
        # uid if none was supplied.

        owner_reference = {
            "apiVersion": lookup(secret_importer_obj.obj, "apiVersion"),
            "kind": lookup(secret_importer_obj.obj, "kind"),
            "name": lookup(secret_importer_obj.obj, "metadata.name"),
            "uid": lookup(secret_importer_obj.obj, "metadata.uid"),
            "blockOwnerDeletion": True,
            "controller": True,
        }

        reclaim_policy = "Delete"

    owner_api_group = owner_reference["apiVersion"].split("/")[0]

    target_secret_item = None

    try:
        target_secret_item = pykube.Secret.objects(
            api, namespace=target_secret_namespace
        ).get(name=target_secret_name)

    except pykube.exceptions.ObjectDoesNotExist:
        pass

    if target_secret_item is None:
        target_secret_obj = {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {
                "name": target_secret_name,
                "namespace": target_secret_namespace,
                "annotations": {
                    f"{owner_api_group}/copier-rule": owner_source,
                    f"{owner_api_group}/secret-name": f"{source_secret_namespace}/{source_secret_name}",
                },
            },
        }

        target_secret_labels = lookup(rule, "targetSecret.labels", {})

        target_secret_obj["metadata"]["labels"] = target_secret_labels

        target_secret_obj["type"] = source_secret_item.obj["type"]
        target_secret_obj["data"] = source_secret_item.obj.get("data", {})

        # Requirement to automatically delete secret can come from the reclaim
        # policy set by the secret copier, or forced by the use of a secret
        # exporter.

        if reclaim_policy == "Delete":
            target_secret_obj["metadata"]["ownerReferences"] = [owner_reference]

        try:
            pykube.Secret(api, target_secret_obj).create()

        except pykube.exceptions.HTTPError as exc:
            if exc.code == 409:
                logger.warning(
                    f"Secret {target_secret_name} in namespace {target_secret_namespace} already exists."
                )
                return

            logger.exception(
                f"Failed to copy secret {source_secret_name} from namespace {source_secret_namespace} to target namespace {target_secret_namespace} as {target_secret_name}."
            )

            return

        logger.info(
            f"Copied secret {source_secret_name} from namespace {source_secret_namespace} to target namespace {target_secret_namespace} as {target_secret_name}, according to rule {rule_number} from {owner_source}."
        )

        return

    # If the secret already existed, we need to first determine if it may have
    # been placed there from a different source, in which case we do not
    # overwrite it to avoid getting in a cycle of continually replacing it. Next
    # we determine if the original secret had changed and if it had, update the
    # secret in the namespace. We compare by looking at the labels, secret type
    # and data.

    target_secret_annotations = target_secret_item.obj["metadata"].setdefault(
        "annotations", {}
    )
    target_secret_owner_source = target_secret_annotations.get(
        f"{owner_api_group}/copier-rule"
    )
    target_secret_owner_secret = target_secret_annotations.get(
        f"{owner_api_group}/secret-name"
    )

    if (target_secret_owner_source and target_secret_owner_source != owner_source) or (
        target_secret_owner_secret
        and target_secret_owner_secret
        != f"{source_secret_namespace}/{source_secret_name}"
    ):
        logger.warning(
            f"Secret {target_secret_name} in namespace {target_secret_namespace} subject of multiple rules, {target_secret_owner_source} and {owner_source}, ignoring."
        )
        return

    labels = lookup(rule, "targetSecret.labels", {})

    source_secret_labels = source_secret_item.labels
    source_secret_labels.update(labels)

    target_secret_labels = target_secret_item.labels

    if (
        source_secret_item.obj["type"] == target_secret_item.obj["type"]
        and source_secret_item.obj.get("data", {})
        == target_secret_item.obj.get("data", {})
        and source_secret_labels == target_secret_labels
    ):
        return

    target_secret_item.obj["type"] = source_secret_item.obj["type"]
    target_secret_item.obj["data"] = source_secret_item.obj.get("data", {})

    target_secret_item.obj["metadata"]["labels"] = source_secret_labels

    target_secret_item.obj["metadata"]["annotations"].update(
        {
            f"{owner_api_group}/copier-rule": owner_source,
            f"{owner_api_group}/secret-name": f"{source_secret_namespace}/{source_secret_name}",
        }
    )

    target_secret_item.update()

    logger.info(
        f"Updated secret {target_secret_name} in namespace {target_secret_namespace} from secret {source_secret_name} in namespace {source_secret_namespace}, according to rule {rule_number} from {owner_source}."
    )


def update_secrets(name, rules):
    """Update the specified secrets in the namespace."""

    for rule in rules:
        update_secret(name, rule)
