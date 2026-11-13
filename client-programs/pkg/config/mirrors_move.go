package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// MoveClusterRegistryMirrors moves the registry mirrors that the config
// at path still declares in its cluster block, cluster.registryMirrors of
// an EducatesLocalConfig or target.cluster.registryMirrors of an
// EducatesConfig, to the registry mirrors file of the data home dataHome,
// where every local cluster's registry mirrors are declared now, and
// writes a one-line note saying so to w.
//
// Only a config in the data home moves: the default local cluster's,
// <data-home>/config.yaml, or a named local cluster's,
// <data-home>/clusters/<name>/config.yaml. Any other file, a missing one
// and one without the field are left as they are, with no note; the
// loader's error for another file that declares the field shows the
// commands that record its mirrors.
//
// The move first records the config's mirrors in the registry mirrors
// file, through RecordLocalMirror, skipping a mirror the file records
// with the same URL and username, so the file's entry, password included,
// stays. A mirror the file records with another URL or username stops the
// move before anything is written, with an error naming it. Then the
// config's previous content is kept beside it, as <file>.bak, or as
// <file>.<timestamp>.bak when that exists, readable only by the user
// since it holds the mirrors' passwords, and the field is removed from
// the config, keeping the rest of the file, comments included.
func MoveClusterRegistryMirrors(dataHome, path string, w io.Writer) error {
	if !IsDataHomeConfig(dataHome, path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	// A file that doesn't parse, or isn't a config of ours, is left for
	// the loader to report.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if apiVersion := yamlMappingValue(root, "apiVersion"); apiVersion == nil || apiVersion.Value != v1alpha1.APIVersion {
		return nil
	}
	kind := yamlMappingValue(root, "kind")
	if kind == nil {
		return nil
	}
	clusterPath := clusterBlockPath(kind.Value)
	if clusterPath == nil {
		return nil
	}
	cluster := root
	for _, key := range clusterPath {
		cluster = yamlMappingValue(cluster, key)
	}
	list := yamlMappingValue(cluster, "registryMirrors")
	if list == nil {
		return nil
	}

	field := strings.Join(append(clusterPath, "registryMirrors"), ".")
	mirrorsPath := LocalMirrorsPath(dataHome)
	mirrors, dropped, err := clusterRegistryMirrors(list, path+": "+field)
	if err != nil {
		return fmt.Errorf("%s: %s was removed, and its registry mirrors can't be moved to %s: %w",
			path, field, mirrorsPath, err)
	}
	toRecord, alreadyRecorded, err := mirrorsToMove(dataHome, mirrors)
	if err != nil {
		return fmt.Errorf("%s: %s was removed: registry mirrors are now declared once, for every local cluster, "+
			"in %s. The config's registry mirrors can't be moved there: %w. The config is left as it is",
			path, field, mirrorsPath, err)
	}
	for _, m := range toRecord {
		if err := RecordLocalMirror(dataHome, m); err != nil {
			return err
		}
	}

	backup, err := backUpConfig(path, data)
	if err != nil {
		return err
	}
	removeYAMLMappingKey(cluster, "registryMirrors")
	out, err := encodeYAMLDocument(&doc)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	perm := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	if err := writeFileAtomically(path, out, perm); err != nil {
		return err
	}

	note := moveNote(path, field, mirrorsPath, backup, mirrors, alreadyRecorded)
	for _, d := range dropped {
		note += " " + d + "."
	}
	fmt.Fprintln(w, note)
	return nil
}

// clusterBlockPath is the path of the cluster block in a config of kind
// kind, which declared registry mirrors before they moved to the registry
// mirrors file, or nil for a kind without one.
func clusterBlockPath(kind string) []string {
	switch kind {
	case v1alpha1.KindEducatesLocalConfig:
		return []string{"cluster"}
	case v1alpha1.KindEducatesConfig:
		return []string{"target", "cluster"}
	default:
		return nil
	}
}

// clusterRegistryMirrors returns the registry mirrors list, the
// registryMirrors node of a config's cluster block named source, declares,
// and a sentence for each mirror whose port or bindIP it drops from list,
// since they never had any effect. The rest loads by the rules of the
// registry mirrors file, so an entry with another field the file's kind
// lacks, or a mirror declared twice, is an error.
func clusterRegistryMirrors(list *yaml.Node, source string) ([]v1alpha1.RegistryMirror, []string, error) {
	if list.Kind == yaml.ScalarNode && list.Tag == "!!null" {
		return nil, nil, nil
	}
	var dropped []string
	if list.Kind == yaml.SequenceNode {
		for _, entry := range list.Content {
			var fields []string
			for _, field := range ineffectiveMirrorFields {
				if removeYAMLMappingKey(entry, field) {
					fields = append(fields, field)
				}
			}
			if note := droppedMirrorFieldsNote(mirrorName(entry), fields); note != "" {
				dropped = append(dropped, note)
			}
		}
	}
	doc := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		yamlScalar("apiVersion"), yamlScalar(v1alpha1.APIVersion),
		yamlScalar("kind"), yamlScalar(v1alpha1.KindEducatesLocalMirrors),
		yamlScalar("registryMirrors"), list,
	}}
	data, err := encodeYAMLDocument(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", source, err)
	}
	mirrors, err := loadLocalMirrorsBytes(data, source)
	if err != nil {
		return nil, nil, err
	}
	return mirrors.RegistryMirrors, dropped, nil
}

// mirrorsToMove returns which of mirrors, the registry mirrors a config
// declares, the move records in the registry mirrors file of the data
// home dataHome, and the names of those it skips, since the file records
// them with the same URL and username. A mirror the file records with
// another URL or username is an error naming each such mirror.
func mirrorsToMove(dataHome string, mirrors []v1alpha1.RegistryMirror) ([]v1alpha1.RegistryMirror, []string, error) {
	recorded, err := LoadLocalMirrors(dataHome)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]v1alpha1.RegistryMirror{}
	for _, r := range recorded {
		byName[r.Mirror] = r
	}
	var toRecord []v1alpha1.RegistryMirror
	var skipped, conflicts []string
	for _, m := range mirrors {
		r, isRecorded := byName[m.Mirror]
		if !isRecorded {
			toRecord = append(toRecord, m)
			continue
		}
		if recordedValues, configValues := mirrorDifferences(r, m); recordedValues != "" {
			conflicts = append(conflicts, fmt.Sprintf("%s records registry mirror %s with %s, and the config "+
				"declares it with %s", LocalMirrorsPath(dataHome), m.Mirror, recordedValues, configValues))
			continue
		}
		skipped = append(skipped, m.Mirror)
	}
	if len(conflicts) > 0 {
		return nil, nil, fmt.Errorf("%s. Make the config's entries agree with it, or remove the mirrors "+
			"from it with 'educates local mirror delete <name>', and run the command again",
			strings.Join(conflicts, "; "))
	}
	return toRecord, skipped, nil
}

// mirrorDifferences describes the values of the registry mirrors a and b
// that the mirror's container is created with and can be seen on it, its
// remote registry and username, that differ, a's first and b's second, or
// returns two empty strings when none does.
func mirrorDifferences(a, b v1alpha1.RegistryMirror) (string, string) {
	remote := func(m v1alpha1.RegistryMirror) string {
		if m.URL == "" {
			return m.Mirror
		}
		return m.URL
	}
	username := func(m v1alpha1.RegistryMirror) string {
		if m.Username == "" {
			return "no username"
		}
		return "username " + m.Username
	}
	var as, bs []string
	if remote(a) != remote(b) {
		as, bs = append(as, "URL "+remote(a)), append(bs, "URL "+remote(b))
	}
	if a.Username != b.Username {
		as, bs = append(as, username(a)), append(bs, username(b))
	}
	return strings.Join(as, " and "), strings.Join(bs, " and ")
}

// backUpConfig keeps data, the previous content of the config at path, in
// a new file beside it, <path>.bak, or <path>.<timestamp>.bak when that
// exists, readable only by the user, and returns its path. It never
// overwrites a file.
func backUpConfig(path string, data []byte) (string, error) {
	backup := path + ".bak"
	err := writeNewFile(backup, data, 0o600)
	if errors.Is(err, fs.ErrExist) {
		backup = path + "." + time.Now().Format("20060102-150405") + ".bak"
		err = writeNewFile(backup, data, 0o600)
	}
	if err != nil {
		return "", fmt.Errorf("back up %s: %w", path, err)
	}
	return backup, nil
}

// writeNewFile writes data to a file at path it creates with the
// permissions perm, and fails with an error wrapping fs.ErrExist when
// path exists.
func writeNewFile(path string, data []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

// moveNote is the note MoveClusterRegistryMirrors writes after moving
// mirrors, the registry mirrors the field field of the config at path
// declared, to the registry mirrors file at mirrorsPath, which already
// recorded those named in alreadyRecorded, keeping the previous config
// as backup.
func moveNote(path, field, mirrorsPath, backup string, mirrors []v1alpha1.RegistryMirror, alreadyRecorded []string) string {
	kept := fmt.Sprintf("The previous config is kept as %s.", backup)
	if len(mirrors) == 0 {
		return fmt.Sprintf("note: removed %s, which declared no registry mirror, from %s, since registry mirrors "+
			"are declared in %s now. %s", field, path, mirrorsPath, kept)
	}
	names := make([]string, 0, len(mirrors))
	for _, m := range mirrors {
		names = append(names, m.Mirror)
	}
	note := fmt.Sprintf("note: moved %s of %s to %s, which declares the registry mirrors of every local cluster: %s.",
		field, path, mirrorsPath, strings.Join(names, ", "))
	if len(alreadyRecorded) > 0 {
		note += fmt.Sprintf(" It already declared %s, which it keeps as it was.", strings.Join(alreadyRecorded, ", "))
	}
	return note + " " + kept
}
