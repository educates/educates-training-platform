package localclusters

import "fmt"

// FlagName is the command-line flag that selects a local cluster.
const FlagName = "local-cluster"

// SelectionEnvVar is the environment variable that selects a local
// cluster for every command run without the flag.
const SelectionEnvVar = "EDUCATES_LOCAL_CLUSTER"

// Selection is what a command was told about which local cluster to act
// on, as plain values: the flag and the environment variable.
type Selection struct {
	// FlagSet reports whether the flag was given, even with an empty
	// value.
	FlagSet bool

	// Flag is the flag's value. It counts only when FlagSet is true.
	Flag string

	// Env is the value of the environment variable SelectionEnvVar.
	Env string
}

// Resolve returns the identity of the local cluster sel selects in the
// data home dataHome. The flag wins when it was given, and an explicitly
// empty flag means the default local cluster. Otherwise a non-empty
// environment variable names the cluster, and with neither the default
// local cluster is meant. An invalid name is an error that names where it
// came from and the naming rules.
func Resolve(sel Selection, dataHome string) (Identity, error) {
	name, source := sel.Env, SelectionEnvVar
	if sel.FlagSet {
		name, source = sel.Flag, "--"+FlagName
	}
	if name == "" {
		return Default(dataHome), nil
	}
	id, err := Named(name, dataHome)
	if err != nil {
		return Identity{}, fmt.Errorf("%s: %w", source, err)
	}
	return id, nil
}
