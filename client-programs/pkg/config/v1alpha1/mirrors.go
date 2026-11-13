package v1alpha1

const KindEducatesLocalMirrors = "EducatesLocalMirrors"

// EducatesLocalMirrors is the registry mirrors file of the CLI data home,
// <data-home>/mirrors.yaml: the registry mirrors every local cluster
// needs, declared once for all of them. Local cluster create and start
// create a declared mirror whose container is missing. Local mirror
// deploy records a mirror in the file, creating it, and local mirror
// delete removes it; the file can also be edited by hand.
type EducatesLocalMirrors struct {
	TypeMeta `yaml:",inline"`

	RegistryMirrors []RegistryMirror `yaml:"registryMirrors,omitempty"`
}

// RegistryMirror is the user-declared pull-through cache surface. The
// always-on localhost:5001 mirror is implicit and not represented here.
type RegistryMirror struct {
	Mirror   string `yaml:"mirror"`
	URL      string `yaml:"url,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Port     string `yaml:"port,omitempty"`
	BindIP   string `yaml:"bindIP,omitempty"`
}
