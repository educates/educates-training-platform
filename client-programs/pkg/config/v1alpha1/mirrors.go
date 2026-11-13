package v1alpha1

const KindEducatesLocalMirrors = "EducatesLocalMirrors"

// EducatesLocalMirrors is the registry mirrors file of the CLI data home,
// <data-home>/mirrors.yaml: the registry mirrors every local cluster
// needs, declared once for all of them. Local cluster create and start
// create a declared mirror whose container is missing. The CLI reads the
// file and never writes it, except the migration of a v3 values.yaml.
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
