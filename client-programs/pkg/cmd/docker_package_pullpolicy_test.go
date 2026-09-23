package cmd

import (
	"strings"
	"testing"
)

// The pull policy rides on the package read from the definition, so it is
// read once alongside the reference it applies to.
func TestReadImagePackagesCarriesThePullPolicy(t *testing.T) {
	workshop := workshopFromYAML(t, `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop:
    packages:
    - name: always
      image: $(image_repository)/always:v1
      imagePullPolicy: Always
    - name: never
      image: ghcr.io/educates/never:v1
      imagePullPolicy: Never
    - name: ifnotpresent
      image: ghcr.io/educates/ifnotpresent:v1
      imagePullPolicy: IfNotPresent
    - name: unset
      image: ghcr.io/educates/unset:v1
    - name: unsetlatest
      image: $(image_repository)/unsetlatest:$(workshop_version)
`)

	packages, err := readImagePackages(workshop, testImageRepositories, "lab-testing", "latest")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(packages) != 5 {
		t.Fatalf("expected 5 packages, got %d", len(packages))
	}

	cases := []struct {
		name            string
		policy          string
		reference       string
		daemonReference string
	}{
		{
			name:            "always",
			policy:          "Always",
			reference:       "registry.docker.local:5000/always:v1",
			daemonReference: "localhost:5001/always:v1",
		},
		{
			name:            "never",
			policy:          "Never",
			reference:       "ghcr.io/educates/never:v1",
			daemonReference: "ghcr.io/educates/never:v1",
		},
		{
			name:            "ifnotpresent",
			policy:          "IfNotPresent",
			reference:       "ghcr.io/educates/ifnotpresent:v1",
			daemonReference: "ghcr.io/educates/ifnotpresent:v1",
		},
		// An undeclared policy takes the Kubernetes default for an image
		// volume, which for a fixed tag pulls only when the image is missing.
		{
			name:            "unset",
			policy:          "IfNotPresent",
			reference:       "ghcr.io/educates/unset:v1",
			daemonReference: "ghcr.io/educates/unset:v1",
		},
		// The default is taken after the tokens are expanded, so a workshop
		// version of latest makes the package pulled every time.
		{
			name:            "unsetlatest",
			policy:          "Always",
			reference:       "registry.docker.local:5000/unsetlatest:latest",
			daemonReference: "localhost:5001/unsetlatest:latest",
		},
	}

	for index, want := range cases {
		got := packages[index]

		if got.Name != want.name {
			t.Errorf("package %d name = %q, want %q", index, got.Name, want.name)
		}

		if got.PullPolicy != want.policy {
			t.Errorf("package %q policy = %q, want %q", want.name, got.PullPolicy, want.policy)
		}

		// The reference is expanded once, here, rather than rebuilt later,
		// for the session and for the daemon alike.
		if got.Reference != want.reference {
			t.Errorf("package %q reference = %q, want %q", want.name, got.Reference, want.reference)
		}

		if got.DaemonReference != want.daemonReference {
			t.Errorf("package %q daemon reference = %q, want %q",
				want.name, got.DaemonReference, want.daemonReference)
		}

		if got.Path != "/opt/packages/"+want.name {
			t.Errorf("package %q path = %q", want.name, got.Path)
		}
	}
}

func TestReadImagePackagesRejectsANonStringPullPolicy(t *testing.T) {
	workshop := workshopFromYAML(t, `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop:
    packages:
    - name: bad
      image: ghcr.io/educates/bad:v1
      imagePullPolicy: [not, a, string]
`)

	_, err := readImagePackages(workshop, testImageRepositories, "lab-testing", "1.0")

	if err == nil {
		t.Fatal("expected a non string policy to be refused")
	}

	if !strings.Contains(err.Error(), "imagePullPolicy") {
		t.Errorf("expected the error to name the field, got %q", err)
	}

	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("expected the error to name the package, got %q", err)
	}
}

// Only Always and Never change what the renderer does, so the constants must
// match what an author writes in the workshop definition.
func TestImagePullPolicyConstantsMatchTheDefinition(t *testing.T) {
	if imagePullPolicyAlways != "Always" {
		t.Errorf("imagePullPolicyAlways = %q", imagePullPolicyAlways)
	}

	if imagePullPolicyNever != "Never" {
		t.Errorf("imagePullPolicyNever = %q", imagePullPolicyNever)
	}

	if imagePullPolicyIfNotPresent != "IfNotPresent" {
		t.Errorf("imagePullPolicyIfNotPresent = %q", imagePullPolicyIfNotPresent)
	}
}
