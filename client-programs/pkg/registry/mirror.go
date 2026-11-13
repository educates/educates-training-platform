package registry

// MirrorConfig is the focused input for registry-mirror container
// management. Decouples the registry package from any specific CLI
// config kind. Callers build one from the registry mirrors file (via
// local cluster create and start) or from individual command flags (via
// 'educates local mirror deploy/delete').
type MirrorConfig struct {
	Mirror   string
	URL      string
	Username string
	Password string
}
