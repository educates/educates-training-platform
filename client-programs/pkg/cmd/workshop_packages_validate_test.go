package cmd

import (
	"strings"
	"testing"
)

// The cases below mirror, one for one, the manifests applied against a real
// API server while implementing this rule set. A divergence between the two
// enforcement sites shows up here as a failure.
func TestValidateWorkshopPackages(t *testing.T) {
	cases := []struct {
		name     string
		packages string
		wantErr  string
	}{
		{
			name: "image alone is accepted",
			packages: `
    - name: argocd
      image: ghcr.io/educates/packages/argocd:v2.10.6
`,
		},
		{
			name: "image with a pull policy is accepted",
			packages: `
    - name: argocd
      image: ghcr.io/educates/packages/argocd:v2.10.6
      imagePullPolicy: IfNotPresent
`,
		},
		{
			name: "image with a pull secret is accepted",
			packages: `
    - name: argocd
      image: ghcr.io/educates/packages/argocd:v2.10.6
      pullSecretRef:
        name: my-registry
`,
		},
		{
			name: "files alone is accepted",
			packages: `
    - name: argocd
      files:
      - image:
          url: ghcr.io/educates/packages/argocd:v2.10.6
`,
		},
		{
			name: "the three expanding variables are accepted",
			packages: `
    - name: argocd
      image: $(image_repository)/$(workshop_name)-argocd:$(workshop_version)
`,
		},
		{
			name: "files and image together are rejected",
			packages: `
    - name: argocd
      image: ghcr.io/educates/packages/argocd:v2.10.6
      files:
      - image:
          url: ghcr.io/educates/packages/argocd:v2.10.6
`,
			wantErr: "either files or image, not both",
		},
		{
			name: "neither files nor image is rejected",
			packages: `
    - name: argocd
`,
			wantErr: "either files or image, not both",
		},
		{
			name: "a pull policy without image is rejected",
			packages: `
    - name: argocd
      imagePullPolicy: Always
      files:
      - image:
          url: ghcr.io/educates/packages/argocd:v2.10.6
`,
			wantErr: "imagePullPolicy requires image",
		},
		{
			name: "a pull secret without image is rejected",
			packages: `
    - name: argocd
      pullSecretRef:
        name: my-registry
      files:
      - image:
          url: ghcr.io/educates/packages/argocd:v2.10.6
`,
			wantErr: "pullSecretRef requires image",
		},
		{
			name: "the reserved platform architecture variable is rejected",
			packages: `
    - name: argocd
      image: ghcr.io/educates/packages/argocd:v2.10.6-$(platform_arch)
`,
			wantErr: "reserved",
		},
		{
			name: "the reserved image cache variable is rejected",
			packages: `
    - name: argocd
      image: $(oci_image_cache)/argocd:v2.10.6
`,
			wantErr: "reserved",
		},
		{
			name: "a name which is not a DNS label is rejected with image",
			packages: `
    - name: Argo_CD
      image: ghcr.io/educates/packages/argocd:v2.10.6
`,
			wantErr: "DNS label name",
		},
		{
			name: "a digest reference is rejected",
			packages: `
    - name: argocd
      image: ghcr.io/educates/packages/argocd@sha256:aaaabbbbccccddddeeeeffff0000111122223333444455556666777788889999
`,
			wantErr: "tagged reference with no digest",
		},
		{
			name: "an untagged reference is rejected",
			packages: `
    - name: argocd
      image: ghcr.io/educates/packages/argocd
`,
			wantErr: "tagged reference with no digest",
		},
		{
			name: "an unknown pull policy is rejected",
			packages: `
    - name: argocd
      image: ghcr.io/educates/packages/argocd:v2.10.6
      imagePullPolicy: Sometimes
`,
			wantErr: "must be one of",
		},
		{
			name: "a name which is not a string is reported",
			packages: `
    - name: 42
      image: ghcr.io/educates/packages/argocd:v2.10.6
`,
			wantErr: "name 42 is not a string",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			workshop := workshopFromYAML(t, "spec:\n  workshop:\n    packages:"+test.packages)

			err := validateWorkshopPackages(workshop)

			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the package to be accepted, got: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected an error mentioning %q, the package was accepted", test.wantErr)
			}

			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("expected the error to mention %q, got: %v", test.wantErr, err)
			}
		})
	}
}

// A workshop declaring no packages at all is valid.
func TestValidateWorkshopPackages_NoPackages(t *testing.T) {
	workshop := workshopFromYAML(t, "spec:\n  workshop: {}\n")

	if err := validateWorkshopPackages(workshop); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
