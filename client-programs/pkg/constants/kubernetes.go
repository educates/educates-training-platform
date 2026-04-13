package constants

// KubernetesVersionToKindImage maps Kubernetes versions to their corresponding kind node images
var (
	KubernetesVersionToKindImage = map[string]string{
		"1.35": "kindest/node:v1.35.0@sha256:452d707d4862f52530247495d180205e029056831160e22870e37e3f6c1ac31f",
		"1.34": "kindest/node:v1.34.3@sha256:08497ee19eace7b4b5348db5c6a1591d7752b164530a36f855cb0f2bdcbadd48",
		"1.33": "kindest/node:v1.33.7@sha256:d26ef333bdb2cbe9862a0f7c3803ecc7b4303d8cea8e814b481b09949d353040",
		"1.32": "kindest/node:v1.32.11@sha256:5fc52d52a7b9574015299724bd68f183702956aa4a2116ae75a63cb574b35af8",
	}
)

const (
	DefaultKubernetesVersion = "1.34"
)
