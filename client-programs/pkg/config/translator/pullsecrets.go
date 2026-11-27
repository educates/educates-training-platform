package translator

// withImageRegistryPullSecrets names the image pull Secrets names in the
// spec.imageRegistry.pullSecrets of a Managed-mode EducatesClusterConfig
// spec, and returns the spec. The operator copies them from the operator
// namespace into every namespace it installs a chart into, and the
// session manager passes them on to workshop namespaces. Without names,
// the spec gets no imageRegistry.
func withImageRegistryPullSecrets(spec map[string]interface{}, names []string) map[string]interface{} {
	if len(names) > 0 {
		spec["imageRegistry"] = map[string]interface{}{"pullSecrets": secretNameRefs(names)}
	}
	return spec
}

// secretNameRefs returns a reference by name to each Secret of names.
func secretNameRefs(names []string) []interface{} {
	refs := make([]interface{}, len(names))
	for i, name := range names {
		refs[i] = map[string]interface{}{"name": name}
	}
	return refs
}

// mergeNames returns first followed by each name of second that first
// does not hold, keeping their order and repeating no name.
func mergeNames(first, second []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{first, second} {
		for _, name := range list {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}
