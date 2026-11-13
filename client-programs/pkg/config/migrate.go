package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// MaybeMigrateV3 attempts to translate a v3 values.yaml in dataHome
// into a v4 config.yaml in the same dir. Returns nil in three cases:
//
//  1. dataHome has no values.yaml — nothing to migrate (first-time user).
//  2. dataHome has config.yaml — already migrated (or never had v3).
//  3. dataHome has values.yaml + provider ∈ {"", "kind"} — migration
//     ran successfully (config.yaml written, the registry mirrors written
//     to mirrors.yaml unless it exists, values.yaml renamed to
//     values.yaml.v3-backup).
//
// Returns a user-actionable error when values.yaml is present without
// config.yaml AND the provider is anything else (gke, eks, openshift,
// etc.): the laptop translator only handles the kind case, so
// non-laptop installs need to be re-declared by hand against the v4
// kind ladder.
//
// Callers (render, deploy, cluster create) invoke this before falling
// through to MissingLocalConfigError so a successful migration is
// transparent — the user runs the same command they would have on v3,
// it just works on v4 going forward.
//
// The migration never prompts, but it is not invisible: it writes a
// notice to w, a line naming the files, then a line for each setting
// the migration carried over in a changed form or dropped. Production
// callers pass os.Stderr.
func MaybeMigrateV3(dataHome string, w io.Writer) error {
	v3Path := filepath.Join(dataHome, "values.yaml")
	v4Path := LocalConfigPath(dataHome)
	backupPath := filepath.Join(dataHome, "values.yaml.v3-backup")

	if _, err := os.Stat(v4Path); err == nil {
		return nil
	}
	if _, err := os.Stat(v3Path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", v3Path, err)
	}

	body, err := os.ReadFile(v3Path)
	if err != nil {
		return fmt.Errorf("read %s: %w", v3Path, err)
	}
	var v3raw map[string]interface{}
	if err := yaml.Unmarshal(body, &v3raw); err != nil {
		return fmt.Errorf("parse %s as v3 values: %w", v3Path, err)
	}

	provider := strV3Path(v3raw, "clusterInfrastructure", "provider")
	if provider != "" && provider != "kind" {
		return fmt.Errorf(`v3 values.yaml at %s has clusterInfrastructure.provider: %q.

The v4 CLI's silent migration only handles laptop-kind installs
(provider empty or "kind"). For non-laptop installs, declare a v4
config explicitly against one of the kinds in
cli.educates.dev/v1alpha1 and rerun with --config <file>:

  - EducatesLocalConfig    (laptop kind)
  - EducatesInlineConfig   (BYO cluster — any provider, pre-existing
                            cert-manager / ingress / policy engine)
  - EducatesGKEConfig      (GKE Managed: Workload Identity + ACME
                            CloudDNS + Contour + Kyverno)
  - EducatesEKSConfig      (EKS Managed: IRSA + ACME Route53 +
                            Contour + Kyverno)
  - EducatesConfig         (escape hatch, full CRD passthrough)

The original %s file is left untouched; you can keep it as a
reference while re-declaring.`, v3Path, provider, v3Path)
	}

	cfg, notes := translateV3ToV4(v3raw)
	// v4 declares registry mirrors for every local cluster in a file of
	// their own, written before the config, so a mirror is never lost: a
	// migration that fails here leaves the v3 values.yaml in place.
	mirrors, droppedNotes := translateV3RegistryMirrors(v3raw)
	mirrorsNote, err := migrateV3RegistryMirrors(dataHome, mirrors, backupPath)
	if err != nil {
		return err
	}
	if mirrorsNote != "" {
		notes = append(notes, mirrorsNote)
	}
	notes = append(notes, droppedNotes...)
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal v4 config: %w", err)
	}
	// Prepend the apiVersion/kind header line so the file reads
	// naturally even though yaml.v2's inline TypeMeta marshal works
	// fine — keeps the file consistent with what `local config init`
	// emits.
	if err := os.WriteFile(v4Path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", v4Path, err)
	}
	if err := os.Rename(v3Path, backupPath); err != nil {
		return fmt.Errorf("rename %s → %s: %w", v3Path, backupPath, err)
	}

	fmt.Fprintf(w, "migrated %s → %s; original saved as %s\n",
		v3Path, v4Path, backupPath)
	for _, note := range notes {
		fmt.Fprintln(w, note)
	}

	// A v3 cache holds the CA certificate as an Opaque Secret with only
	// ca.crt, which is a valid CA trust Secret, so it is kept as it is.
	// Tell the user how deploys use it: beside a cached wildcard
	// certificate it is that certificate's CA trust, and otherwise a root
	// CA is still needed.
	if domain := strV3Path(v3raw, "clusterIngress", "domain"); domain != "" {
		noteIfV3CACachePresent(w, filepath.Join(dataHome, "secrets"), domain)
	}
	return nil
}

// noteIfV3CACachePresent scans the secrets cache for any file shaped
// like the v3 CA cache (Opaque + ca.crt + matching domain annotation)
// and writes a one-time note to w on how deploys use it. Best-effort:
// failures (no secrets dir, unreadable files) silently no-op; a deploy
// that finds no certificate source gives its own remediation error.
//
// w is parameterised for tests; production callers pass os.Stderr.
func noteIfV3CACachePresent(w io.Writer, secretsDir, domain string) {
	entries, err := os.ReadDir(secretsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(secretsDir, e.Name()))
		if err != nil {
			continue
		}
		var raw map[string]interface{}
		if err := yaml.Unmarshal(body, &raw); err != nil {
			continue
		}
		ann := asMap(asMap(raw["metadata"])["annotations"])
		if ann == nil {
			continue
		}
		if d, _ := ann["training.educates.dev/domain"].(string); d != domain {
			continue
		}
		t, _ := raw["type"].(string)
		data := asMap(raw["data"])
		if (t == "Opaque" || t == "") && data["ca.crt"] != nil {
			fmt.Fprintf(w, `note: the cached v3 CA certificate

  %s

is a valid CA trust Secret and is kept as it is. When a wildcard TLS
certificate for %s is also cached, as the v3 mkcert setup left it,
deploys serve that certificate with the StaticCertificate provider, trust
this CA, and install no cert-manager. Otherwise a deploy needs a root CA,
generated with:

  educates local secrets add ca <name>

or registered with its private key, such as the root CA that issued the
v3 certificate:

  educates local secrets add ca <name> --cert ROOT.pem --key ROOT-KEY.pem
`, filepath.Join(secretsDir, e.Name()), "*."+domain)
			return
		}
	}
}

// translateV3ToV4 builds the v4 EducatesLocalConfig from a v3 values map
// parsed as map[string]interface{}. Missing fields stay zero — the v4
// schema defaults pick up the rest at load time. It also returns the
// lines of the migration notice that say what it carried over in a
// changed form or dropped.
func translateV3ToV4(v3 map[string]interface{}) (*v1alpha1.EducatesLocalConfig, []string) {
	var notes []string
	cfg := &v1alpha1.EducatesLocalConfig{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.KindEducatesLocalConfig,
		},
	}

	// ingress
	cfg.Ingress.Domain = strV3Path(v3, "clusterIngress", "domain")
	// v3 clusterIngress.protocol: http was a plain-HTTP install with no
	// TLS; v4 expresses that as ingress.insecure.
	if strV3Path(v3, "clusterIngress", "protocol") == "http" {
		cfg.Ingress.Insecure = true
	}

	// cluster
	var note string
	cfg.Cluster.ListenAddress, note = migrateV3ListenAddress(strV3Path(v3, "localKindCluster", "listenAddress"))
	if note != "" {
		notes = append(notes, note)
	}
	cfg.Cluster.ApiServer.Address = strV3Path(v3, "localKindCluster", "apiServer", "address")
	cfg.Cluster.ApiServer.Port = intV3Path(v3, "localKindCluster", "apiServer", "port")
	cfg.Cluster.Networking.ServiceSubnet = strV3Path(v3, "localKindCluster", "networking", "serviceSubnet")
	cfg.Cluster.Networking.PodSubnet = strV3Path(v3, "localKindCluster", "networking", "podSubnet")
	for _, m := range listV3Path(v3, "localKindCluster", "volumeMounts") {
		entry := asMap(m)
		vm := v1alpha1.VolumeMount{
			HostPath:      strMap(entry, "hostPath"),
			ContainerPath: strMap(entry, "containerPath"),
		}
		if v, ok := entry["readOnly"].(bool); ok {
			vm.ReadOnly = &v
		}
		cfg.Cluster.VolumeMounts = append(cfg.Cluster.VolumeMounts, vm)
	}
	// resolver. A local cluster's names now resolve to its listen address,
	// so targetAddress goes. Setting the listen address from it instead
	// would change where kind publishes the cluster's ports, so the notice
	// says how to do that.
	if target := strV3Path(v3, "localDNSResolver", "targetAddress"); target != "" {
		notes = append(notes, fmt.Sprintf("dropped localDNSResolver.targetAddress %[1]s: a local cluster's names "+
			"now resolve to its cluster.listenAddress; set cluster.listenAddress to %[1]s to keep resolving the "+
			"cluster there, or for other names add address=/<name>/%[1]s to a *.conf file in the dnsmasq.d "+
			"folder of the data home", target))
	}
	for _, d := range listV3Path(v3, "localDNSResolver", "extraDomains") {
		if s, ok := d.(string); ok {
			cfg.Resolver.ExtraDomains = append(cfg.Resolver.ExtraDomains, s)
		}
	}

	// imageVersions. The image pre-puller no longer runs a pause
	// container, and a pause container image cannot play the roles of
	// the image-puller helper that replaced it, so its override goes.
	for _, m := range listV3Path(v3, "imageVersions") {
		entry := asMap(m)
		name := strMap(entry, "name")
		if name == "pause-container" {
			notes = append(notes, "dropped imageVersions entry pause-container: the image pre-puller now runs its own image-puller helper, which a pause container image cannot replace")
			continue
		}
		cfg.ImageVersions = append(cfg.ImageVersions, v1alpha1.ImageVersion{
			Name:  name,
			Image: strMap(entry, "image"),
		})
	}

	// imagePuller
	notes = append(notes, translateV3ImagePuller(v3, cfg.ImageVersions, &cfg.ImagePrePuller)...)

	// websiteStyling (narrow subset only)
	cfg.WebsiteStyling.DefaultTheme = strV3Path(v3, "websiteStyling", "defaultTheme")
	for _, m := range listV3Path(v3, "websiteStyling", "themeDataRefs") {
		entry := asMap(m)
		cfg.WebsiteStyling.ThemeDataRefs = append(cfg.WebsiteStyling.ThemeDataRefs, v1alpha1.ThemeDataRef{
			Namespace: strMap(entry, "namespace"),
			Name:      strMap(entry, "name"),
		})
	}

	// secretPropagation
	for _, s := range listV3Path(v3, "secretPropagation", "imagePullSecretNames") {
		if name, ok := s.(string); ok {
			cfg.SecretPropagation.ImagePullSecretNames = append(cfg.SecretPropagation.ImagePullSecretNames, name)
		}
	}

	return cfg, notes
}

// translateV3RegistryMirrors returns the registry mirrors of a v3 values
// map, its localKindCluster.registryMirrors, which v4 declares for every
// local cluster in the registry mirrors file of the data home, with a
// line of the migration notice for each mirror whose port or bindIP it
// dropped, since they never had any effect.
func translateV3RegistryMirrors(v3 map[string]interface{}) ([]v1alpha1.RegistryMirror, []string) {
	var mirrors []v1alpha1.RegistryMirror
	var notes []string
	for _, m := range listV3Path(v3, "localKindCluster", "registryMirrors") {
		entry := asMap(m)
		mirror := v1alpha1.RegistryMirror{
			Mirror:   strMap(entry, "mirror"),
			URL:      strMap(entry, "url"),
			Username: strMap(entry, "username"),
			Password: strMap(entry, "password"),
		}
		mirrors = append(mirrors, mirror)
		var dropped []string
		for _, field := range ineffectiveMirrorFields {
			if _, set := entry[field]; set {
				dropped = append(dropped, field)
			}
		}
		if note := droppedMirrorFieldsNote(mirror.Mirror, dropped); note != "" {
			notes = append(notes, note)
		}
	}
	return mirrors, notes
}

// ineffectiveMirrorFields are the fields a registry mirror took in
// version 3, and in pre-releases of version 4, that never had any effect:
// a mirror's container is published on a port of the loopback address
// Docker picks.
var ineffectiveMirrorFields = []string{"port", "bindIP"}

// droppedMirrorFieldsNote says that fields, among ineffectiveMirrorFields,
// of the registry mirror mirror were dropped, since they never had any
// effect, or is empty when fields is.
func droppedMirrorFieldsNote(mirror string, fields []string) string {
	switch len(fields) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("%s of registry mirror %s was dropped: it never had any effect", fields[0], mirror)
	default:
		return fmt.Sprintf("%s of registry mirror %s were dropped: they never had any effect",
			strings.Join(fields, " and "), mirror)
	}
}

// migrateV3RegistryMirrors writes mirrors, the registry mirrors of a v3
// values.yaml, to the registry mirrors file of the data home dataHome,
// and returns the line of the migration notice that says so. A file that
// already exists is left as it is, and the line names the mirrors to add
// to it by hand, from backupPath, where the v3 values.yaml is kept. With
// no mirrors, it writes nothing and returns no line.
func migrateV3RegistryMirrors(dataHome string, mirrors []v1alpha1.RegistryMirror, backupPath string) (string, error) {
	if len(mirrors) == 0 {
		return "", nil
	}
	names := make([]string, 0, len(mirrors))
	for _, m := range mirrors {
		names = append(names, m.Mirror)
	}
	path := LocalMirrorsPath(dataHome)
	if _, err := os.Stat(path); err == nil {
		return fmt.Sprintf("left localKindCluster.registryMirrors out, since %s already exists and is left as it is: "+
			"add the registry mirrors %s to it by hand, from localKindCluster.registryMirrors in %s",
			path, strings.Join(names, ", "), backupPath), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check for %s: %w", path, err)
	}
	for _, m := range mirrors {
		if err := RecordLocalMirror(dataHome, m); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("moved localKindCluster.registryMirrors to %s, where the registry mirrors every local "+
		"cluster needs are declared: %s", path, strings.Join(names, ", ")), nil
}

// migrateV3ListenAddress returns the cluster.listenAddress of the
// migrated config for the v3 localKindCluster.listenAddress listenAddress,
// with the line of the migration notice that says why it changed, empty
// when it did not.
//
// v3 left a missing listen address to kind, which publishes the cluster's
// ports on every host address, as 0.0.0.0 does, and every local cluster
// now publishes them on a specific IP address of the host. Either becomes
// the host's IP address, and with none detected, stays unset for the user
// to set. Any other address is kept as it is.
func migrateV3ListenAddress(listenAddress string) (string, string) {
	if listenAddress != "" && listenAddress != "0.0.0.0" {
		return listenAddress, ""
	}
	was := "was not set"
	if listenAddress != "" {
		was = "was " + listenAddress
	}
	why := fmt.Sprintf("localKindCluster.listenAddress %s, so the cluster published its ports 80 and 443 on every "+
		"host address, and every local cluster now publishes them on a specific IP address of the host", was)
	hostIP, err := hostinfo.DetectHostIP()
	if err != nil || hostIP == "" {
		return "", fmt.Sprintf("left cluster.listenAddress unset: %s, and no IP address of this host was detected. "+
			"Set it with 'educates local config set cluster.listenAddress <ip>' before using the cluster", why)
	}
	return hostIP, fmt.Sprintf("set cluster.listenAddress to %s, this host's IP address: %s. An alias such as "+
		"10.10.10.1, added to a network interface, survives network changes, where the host's IP address may not",
		hostIP, why)
}

// translateV3ImagePuller fills the image pre-puller block from a v3
// imagePuller block, so the pre-puller keeps the v3 switch and pulls the
// same images. v3 pulled the training portal and then each prePullImages
// name, prePullImages defaulting to base-environment. A list holding
// base-environment, or no list, is the v4 default images plus the other
// names; any other list drops the default images and names the training
// portal itself. A v3 file without the block leaves the pre-puller unset.
//
// v3 resolved any name, where v4 fails the deploy on a name its image
// inventory does not hold, so a name of v3OnlyImageNames is dropped,
// unless imageVersions, already migrated, defines it under a name that
// reaches the inventory. It returns the notice lines saying what it
// carried over and what it dropped.
func translateV3ImagePuller(v3 map[string]interface{}, imageVersions []v1alpha1.ImageVersion, out *v1alpha1.ImagePrePullerConfig) []string {
	imagePuller := asMap(v3["imagePuller"])
	if imagePuller == nil {
		return nil
	}
	if enabled, isBool := imagePuller["enabled"].(bool); isBool {
		out.Enabled = &enabled
	}

	// Only a missing list means the v3 default; an empty one pulled the
	// training portal alone.
	var dropped []string
	if prePullImages, isList := imagePuller["prePullImages"].([]interface{}); isList {
		dropped = translateV3PrePullImages(prePullImages, imageVersions, out)
	}

	notes := []string{carriedImagePullerNote(*out)}
	if len(dropped) > 0 {
		notes = append(notes, fmt.Sprintf("dropped imagePuller.prePullImages names the v4 image inventory does not hold: %s; to pull one, add its full reference to imagePrePuller.images as an image entry",
			strings.Join(dropped, ", ")))
	}
	return notes
}

// translateV3PrePullImages fills the images of the pre-puller block from
// a v3 prePullImages list and returns the names it dropped.
func translateV3PrePullImages(prePullImages []interface{}, imageVersions []v1alpha1.ImageVersion, out *v1alpha1.ImagePrePullerConfig) []string {
	inInventory := map[string]bool{}
	for _, iv := range imageVersions {
		if reachesImageInventory(iv) {
			inInventory[iv.Name] = true
		}
	}
	var names, dropped []string
	keepDefaults := false
	for _, item := range prePullImages {
		name, _ := item.(string)
		switch {
		case name == "":
		case name == "base-environment":
			keepDefaults = true
		case v3OnlyImageNames[name] && !inInventory[name]:
			dropped = append(dropped, name)
		default:
			names = append(names, name)
		}
	}
	if !keepDefaults {
		f := false
		out.IncludeDefaultImages = &f
		names = append([]string{"training-portal"}, names...)
	}
	for _, name := range names {
		out.Images = append(out.Images, v1alpha1.PrePulledImage{Name: name})
	}
	return dropped
}

// carriedImagePullerNote is the notice line listing the fields of the
// pre-puller block the migration set.
func carriedImagePullerNote(c v1alpha1.ImagePrePullerConfig) string {
	var fields []string
	if c.Enabled != nil {
		fields = append(fields, fmt.Sprintf("enabled: %t", *c.Enabled))
	}
	if c.IncludeDefaultImages != nil {
		fields = append(fields, fmt.Sprintf("includeDefaultImages: %t", *c.IncludeDefaultImages))
	}
	if len(c.Images) > 0 {
		names := make([]string, 0, len(c.Images))
		for _, e := range c.Images {
			names = append(names, e.Name)
		}
		fields = append(fields, "images: "+strings.Join(names, ", "))
	}
	if len(fields) == 0 {
		return "carried imagePuller over: it matches the imagePrePuller defaults, so imagePrePuller is left unset"
	}
	return "carried imagePuller over to imagePrePuller: " + strings.Join(fields, ", ")
}

// v3OnlyImageNames are the names of the v3 image inventory that the v4
// image inventory does not hold. The v3 inventory no longer changes, so
// the list is fixed.
var v3OnlyImageNames = map[string]bool{
	"loftsh-kubernetes-v1.31": true,
	"loftsh-kubernetes-v1.32": true,
	"session-manager":         true,
	"secrets-manager":         true,
	"lookup-service":          true,
	"node-ca-injector":        true,
	"pause-container":         true,
}

// operatorRoutedImageNames are the image override names the operator
// routes to dedicated chart values instead of the session-manager chart's
// image inventory: session-manager to that chart's own image, image-puller
// to its imagePrePuller.image, and node-ca-injector to the node-ca-injector
// chart's image (applySMImageValues and renderNodeCAInjectorValues in
// installer/operator/internal/controller/platform).
var operatorRoutedImageNames = map[string]bool{
	"session-manager":  true,
	"node-ca-injector": true,
	"image-puller":     true,
}

// reachesImageInventory reports whether an imageVersions entry adds its
// name to the session-manager chart's image inventory: any entry but one
// the CLI routes to a component custom resource of its own, or one whose
// name the operator routes to a dedicated chart value.
func reachesImageInventory(iv v1alpha1.ImageVersion) bool {
	return !iv.RoutesToComponentResource() && !operatorRoutedImageNames[iv.Name]
}

// strV3Path walks v3raw by string keys and returns the leaf as string,
// or "" when any segment is missing / wrong type.
func strV3Path(v3 map[string]interface{}, path ...string) string {
	v, ok := walkV3(v3, path...)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func intV3Path(v3 map[string]interface{}, path ...string) int {
	v, ok := walkV3(v3, path...)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func listV3Path(v3 map[string]interface{}, path ...string) []interface{} {
	v, ok := walkV3(v3, path...)
	if !ok {
		return nil
	}
	list, _ := v.([]interface{})
	return list
}

func walkV3(v3 map[string]interface{}, path ...string) (interface{}, bool) {
	var cur interface{} = v3
	for _, p := range path {
		m := asMap(cur)
		if m == nil {
			return nil, false
		}
		v, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

func asMap(v interface{}) map[string]interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		return x
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = val
		}
		return out
	}
	return nil
}

func strMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}
