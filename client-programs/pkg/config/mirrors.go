package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
)

// LoadLocalMirrors returns the registry mirrors the registry mirrors file
// of the data home dataHome, LocalMirrorsPath, declares. A missing file
// declares none. The file is loaded strictly, as an EducatesLocalMirrors
// its schema validates, with no field the kind lacks and each mirror
// declared once; a file that doesn't load is an error that names the file
// and why.
func LoadLocalMirrors(dataHome string) ([]v1alpha1.RegistryMirror, error) {
	path := LocalMirrorsPath(dataHome)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	mirrors, err := loadLocalMirrorsBytes(data, path)
	if err != nil {
		return nil, err
	}
	return mirrors.RegistryMirrors, nil
}

// marshalLocalMirrors returns the registry mirrors file that declares
// mirrors: the yaml-language-server modeline, then the EducatesLocalMirrors
// that holds them.
func marshalLocalMirrors(mirrors []v1alpha1.RegistryMirror) ([]byte, error) {
	body, err := yaml.Marshal(&v1alpha1.EducatesLocalMirrors{
		TypeMeta:        v1alpha1.TypeMeta{APIVersion: v1alpha1.APIVersion, Kind: v1alpha1.KindEducatesLocalMirrors},
		RegistryMirrors: mirrors,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal the registry mirrors: %w", err)
	}
	return append([]byte(LocalMirrorsModeline+"\n"), body...), nil
}

// loadLocalMirrorsBytes loads data, the registry mirrors file source, by
// the rules of LoadLocalMirrors.
func loadLocalMirrorsBytes(data []byte, source string) (*v1alpha1.EducatesLocalMirrors, error) {
	jsonData, raw, err := yamlToJSON(data, source)
	if err != nil {
		return nil, err
	}
	apiVersion, _ := raw["apiVersion"].(string)
	kind, _ := raw["kind"].(string)
	switch {
	case apiVersion == "" || kind == "":
		return nil, fmt.Errorf("%s: missing required field 'apiVersion' or 'kind'", source)
	case apiVersion != v1alpha1.APIVersion:
		return nil, fmt.Errorf("%s: unsupported apiVersion %q (want %q)", source, apiVersion, v1alpha1.APIVersion)
	case kind != v1alpha1.KindEducatesLocalMirrors:
		return nil, fmt.Errorf("%s: kind %q is not the kind of the registry mirrors file, %q",
			source, kind, v1alpha1.KindEducatesLocalMirrors)
	}
	decoded, err := decode(jsonData, schemas.EducatesLocalMirrors, source, &v1alpha1.EducatesLocalMirrors{}, true)
	if err != nil {
		return nil, err
	}
	mirrors := decoded.(*v1alpha1.EducatesLocalMirrors)
	declared := map[string]bool{}
	for _, m := range mirrors.RegistryMirrors {
		if declared[m.Mirror] {
			return nil, fmt.Errorf("%s: registry mirror %s is declared more than once; declare each mirror once",
				source, m.Mirror)
		}
		declared[m.Mirror] = true
	}
	return mirrors, nil
}
