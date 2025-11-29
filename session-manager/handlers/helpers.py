import base64

def xget(obj, key, default=None):
    """Looks up a property within an object using a dotted path as key.
    If the property isn't found, then return the default value.

    """

    keys = key.split(".")
    value = default

    for key in keys:
        value = obj.get(key)
        if value is None:
            return default

        obj = value

    return value


def image_pull_policy(image):
    if (
        image.endswith(":main")
        or image.endswith(":master")
        or image.endswith(":develop")
        or image.endswith(":latest")
        or ":" not in image
    ):
        return "Always"
    else:
        return "IfNotPresent"


def resource_owned_by(child, parent):
    api_version = xget(parent, "apiVersion")
    kind = xget(parent, "kind")
    name = xget(parent, "metadata.name")
    uid = xget(parent, "metadata.uid")

    for reference in xget(child, "metadata.ownerReferences", []):
        if (
            xget(reference, "apiVersion") == api_version
            and xget(reference, "kind") == kind
            and xget(reference, "name") == name
            and xget(reference, "uid") == uid
        ):
            return True

    return False


def substitute_variables(obj, variables, encode=True, recurse=6):
    if isinstance(obj, str):
        original_obj = obj
        for _ in range(recurse):
            if "$(" not in obj:
                break
            for k, v in variables.items():
                obj = obj.replace(f"$({k})", v)
            if obj == original_obj:
                break
        if encode and obj.startswith("$(base64(") and obj.endswith("))"):
            obj = base64.b64encode(obj[9:-2].encode("utf-8")).decode("ascii").strip()
        return obj
    elif isinstance(obj, dict):
        return {k: substitute_variables(v, variables, encode, recurse) for k, v in obj.items()}
    elif isinstance(obj, list):
        return [substitute_variables(v, variables, encode, recurse) for v in obj]
    elif callable(obj):
        return obj(variables)
    else:
        return obj


def smart_overlay_merge(target, patch, attr="name"):
    if isinstance(patch, dict):
        for key, value in patch.items():
            if key not in target:
                target[key] = value
            elif type(target[key]) != type(value):
                target[key] = value
            elif isinstance(value, (dict, list)):
                smart_overlay_merge(target[key], value, attr)
            else:
                target[key] = value
    elif isinstance(patch, list):
        appended_items = []
        for patch_item in patch:
            if isinstance(patch_item, dict) and attr in patch_item:
                for i, target_item in enumerate(target):
                    if (
                        isinstance(target_item, dict)
                        and target_item.get(attr) == patch_item[attr]
                        and patch_item[attr] not in appended_items
                    ):
                        smart_overlay_merge(target[i], patch_item, attr)
                        break
                else:
                    if patch_item[attr] not in appended_items:
                        appended_items.append(patch_item[attr])
                    target.append(patch_item)
            else:
                target.append(patch_item)


class Applications:
    defaults = {
        "console": False,
        "docker": False,
        "editor": False,
        "files": False,
        "git": False,
        "examiner": False,
        "registry": False,
        "slides": False,
        "terminal": True,
        "uploads": False,
        "vcluster": False,
        "workshop": True,
        "webdav": False,
    }

    def __init__(self, configuration):
        self.configuration = configuration

    def names(self):
        return self.defaults.keys()

    def __iter__(self):
        return iter(self.defaults)

    def is_enabled(self, name):
        return self.configuration.get(name, {}).get(
            "enabled", self.defaults.get(name, False)
        )

    def properties(self, name):
        return self.configuration.setdefault(name, {})

    def property(self, name, key, default=None):
        properties = self.properties(name)
        keys = key.split(".")
        value = default
        for key in keys:
            value = properties.get(key)
            if value is None:
                return default
            properties = value
        return value
