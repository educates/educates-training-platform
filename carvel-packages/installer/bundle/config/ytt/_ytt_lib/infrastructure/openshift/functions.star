load("@ytt:data", "data")
load("defaults.star", "enabledByDefaultPackagesList")

def isClusterPackageEnableByDefault(package):
  return package in enabledByDefaultPackagesList
end

def isClusterPackageEnabled(package):
  if hasattr(data.values, "clusterPackages") and hasattr(data.values.clusterPackages, package) and hasattr(data.values.clusterPackages[package], "enabled"):
    return data.values.clusterPackages[package].enabled
  else:
    return package in enabledByDefaultPackagesList
  end
end

def isClusterPackageExplicitDisabled(package):
  return not isClusterPackageEnabled(package)
end

def xgetattr(object, path, default=None):
  def _lookup(object, key, default=None):
    keys = key.split(".")
    value = default
    for key in keys:
      value = getattr(object, key, None)
      if value == None:
        return default
      end
      object = value
    end
    return value
  end

  return _lookup(object, path, default)
end