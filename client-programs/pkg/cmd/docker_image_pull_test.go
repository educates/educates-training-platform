package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// The rule is the one the session manager applies on a cluster to a workshop
// image, and to an extension package image declaring no pull policy, so these
// cases are the same tags it recognises.
func TestDefaultImagePullPolicy(t *testing.T) {
	cases := []struct {
		image string
		want  string
	}{
		{image: "localhost:5001/educates-base-environment:latest", want: imagePullPolicyAlways},
		{image: "ghcr.io/educates/educates-base-environment:main", want: imagePullPolicyAlways},
		{image: "ghcr.io/educates/educates-base-environment:master", want: imagePullPolicyAlways},
		{image: "ghcr.io/educates/educates-base-environment:develop", want: imagePullPolicyAlways},
		{image: "ghcr.io/educates/argocd:main", want: imagePullPolicyAlways},
		{image: "ubuntu", want: imagePullPolicyAlways},
		{image: "ghcr.io/educates/educates-base-environment:4.0.0", want: imagePullPolicyIfNotPresent},
		{image: "localhost:5001/greeter:1.0.0", want: imagePullPolicyIfNotPresent},
		{image: "ghcr.io/educates/lab-image@sha256:" + strings.Repeat("a", 64), want: imagePullPolicyIfNotPresent},
	}

	for _, test := range cases {
		t.Run(test.image, func(t *testing.T) {
			if got := defaultImagePullPolicy(test.image); got != test.want {
				t.Errorf("defaultImagePullPolicy(%q) = %q, want %q", test.image, got, test.want)
			}
		})
	}
}

func TestRefreshImage(t *testing.T) {
	const reference = "localhost:5001/educates-base-environment:latest"

	pullFails := func(string) error {
		return errors.New("dial tcp: lookup localhost: connection refused")
	}

	t.Run("a successful pull says nothing more", func(t *testing.T) {
		var stdout bytes.Buffer

		pulled := ""

		err := refreshImage(
			reference, workshopImageDescription,
			func(reference string) error { pulled = reference; return nil },
			func(string) bool { t.Error("presence should not be checked after a pull"); return true },
			&stdout,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if pulled != reference {
			t.Errorf("pulled %q, want %q", pulled, reference)
		}

		if strings.Contains(stdout.String(), "Warning") {
			t.Errorf("expected no warning, got %q", stdout.String())
		}
	})

	t.Run("a failed pull falls back to a local copy with a warning", func(t *testing.T) {
		var stdout bytes.Buffer

		err := refreshImage(reference, workshopImageDescription, pullFails, func(string) bool { return true }, &stdout)

		if err != nil {
			t.Fatalf("expected the local copy to be used, got %v", err)
		}

		for _, want := range []string{"Warning", reference, "connection refused"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("expected the output to contain %q, got %q", want, stdout.String())
			}
		}
	})

	t.Run("a failed pull with no local copy stops the deploy", func(t *testing.T) {
		var stdout bytes.Buffer

		err := refreshImage(reference, workshopImageDescription, pullFails, func(string) bool { return false }, &stdout)

		if err == nil {
			t.Fatal("expected the pull failure to be returned")
		}

		if strings.Contains(stdout.String(), "Warning") {
			t.Errorf("expected no warning when there is nothing to fall back to, got %q", stdout.String())
		}
	})
}
