package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestIsAuthenticationFailure(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{
			name:    "an explicit unauthorized",
			message: "unauthorized: authentication required",
			want:    true,
		},
		{
			name:    "no credentials held",
			message: `Get "https://registry.example.com/v2/": no basic auth credentials`,
			want:    true,
		},
		{
			name:    "access denied to an existing repository",
			message: "denied: requested access to the resource is denied",
			want:    true,
		},
		{
			name:    "a repository which simply does not exist",
			message: `manifest unknown: manifest tagged by "v9" is not found`,
			want:    false,
		},
		{
			name:    "a rate limit, which logging in does not fix",
			message: "toomanyrequests: You have reached your pull rate limit",
			want:    false,
		},
		{
			name:    "the registry being unreachable",
			message: "dial tcp: lookup registry.example.com: no such host",
			want:    false,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := isAuthenticationFailure(errors.New(test.message)); got != test.want {
				t.Errorf("isAuthenticationFailure(%q) = %v, want %v", test.message, got, test.want)
			}
		})
	}
}

func capableDaemon() daemonCapabilities {
	return daemonCapabilities{
		ServerVersion:   "29.7.2",
		ComposeVersion:  "5.5.0",
		ContainerdStore: true,
	}
}

func incapableDaemon() daemonCapabilities {
	capabilities := capableDaemon()
	capabilities.ContainerdStore = false

	return capabilities
}

func TestResolveDockerPackageDelivery(t *testing.T) {
	t.Run("auto mounts on a capable daemon", func(t *testing.T) {
		mounting, message, err := resolveDockerPackageDelivery(
			dockerPackageDeliveryAuto, capableDaemon(), false,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mounting {
			t.Error("expected to mount on a capable daemon")
		}

		if !strings.Contains(message, "mounted") {
			t.Errorf("expected the message to say packages are mounted, got %q", message)
		}
	})

	t.Run("auto falls back and names the requirement", func(t *testing.T) {
		mounting, message, err := resolveDockerPackageDelivery(
			dockerPackageDeliveryAuto, incapableDaemon(), false,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if mounting {
			t.Error("expected a fallback to fetching")
		}

		// The author needs to know both what happened and why.
		if !strings.Contains(message, "fetched") {
			t.Errorf("expected the message to say packages are fetched, got %q", message)
		}

		if !strings.Contains(message, "containerd image store") {
			t.Errorf("expected the message to name the requirement, got %q", message)
		}
	})

	t.Run("fetch always fetches, without consulting the daemon", func(t *testing.T) {
		mounting, message, err := resolveDockerPackageDelivery(
			dockerPackageDeliveryFetch, capableDaemon(), false,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if mounting {
			t.Error("expected fetch to be honoured on a capable daemon")
		}

		if !strings.Contains(message, "fetched") {
			t.Errorf("got %q", message)
		}
	})

	t.Run("image-mount forces a mount on a capable daemon", func(t *testing.T) {
		mounting, _, err := resolveDockerPackageDelivery(
			dockerPackageDeliveryImageMount, capableDaemon(), false,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !mounting {
			t.Error("expected to mount")
		}
	})

	t.Run("image-mount refuses an incapable daemon before deploying", func(t *testing.T) {
		_, _, err := resolveDockerPackageDelivery(
			dockerPackageDeliveryImageMount, incapableDaemon(), false,
		)

		if err == nil {
			t.Fatal("expected forcing a mount on an incapable daemon to fail")
		}

		// Failing here rather than at container create is the whole point, so
		// the error has to say what is missing and what to do.
		if !strings.Contains(err.Error(), "containerd image store") {
			t.Errorf("expected the error to name what is missing, got %q", err)
		}

		for _, want := range []string{"--package-delivery", "fetch"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("expected the error to offer %q, got %q", want, err)
			}
		}
	})
}

func TestResolveDockerPackageDeliveryOnPodman(t *testing.T) {
	// Neither podman-compose nor Podman pulls the image behind a volume, so a
	// mount of an image which is not already local fails at container create.

	t.Run("auto fetches when the image is absent locally", func(t *testing.T) {
		mounting, message, err := resolveDockerPackageDelivery(
			dockerPackageDeliveryAuto, capableDaemon(), true,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if mounting {
			t.Error("expected Podman with an absent image to fetch")
		}

		if !strings.Contains(message, "Podman") {
			t.Errorf("expected the message to explain the Podman caveat, got %q", message)
		}
	})

	t.Run("image-mount on Podman with an absent image is refused", func(t *testing.T) {
		_, _, err := resolveDockerPackageDelivery(
			dockerPackageDeliveryImageMount, capableDaemon(), true,
		)

		if err == nil {
			t.Fatal("expected forcing a mount with an absent image on Podman to fail")
		}

		if !strings.Contains(err.Error(), "Podman") {
			t.Errorf("expected the error to explain the Podman caveat, got %q", err)
		}
	})

	t.Run("fetch on Podman is unremarkable", func(t *testing.T) {
		mounting, _, err := resolveDockerPackageDelivery(
			dockerPackageDeliveryFetch, capableDaemon(), true,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if mounting {
			t.Error("expected fetching")
		}
	})
}

func TestDescribePackageDelivery(t *testing.T) {
	t.Run("each package is named, whichever way it arrived", func(t *testing.T) {
		lines := describePackageDelivery([]string{"argocd", "crane"}, true)

		if len(lines) != 2 {
			t.Fatalf("expected one line per package, got %d", len(lines))
		}

		for index, name := range []string{"argocd", "crane"} {
			if !strings.Contains(lines[index], name) {
				t.Errorf("line %d does not name %q: %q", index, name, lines[index])
			}

			if !strings.Contains(lines[index], "mount") {
				t.Errorf("line %d does not say how it arrived: %q", index, lines[index])
			}
		}
	})

	t.Run("a fetched package says so", func(t *testing.T) {
		lines := describePackageDelivery([]string{"argocd"}, false)

		if len(lines) != 1 || !strings.Contains(lines[0], "fetch") {
			t.Errorf("got %v", lines)
		}
	})

	t.Run("no packages yields no lines", func(t *testing.T) {
		if lines := describePackageDelivery(nil, true); len(lines) != 0 {
			t.Errorf("expected nothing, got %v", lines)
		}
	})
}

func TestMissingCredentialsMessage(t *testing.T) {
	message := missingCredentialsMessage(packageImageDescription, "ghcr.io/educates/argocd:v1", "ghcr.io")

	for _, want := range []string{packageImageDescription, "ghcr.io/educates/argocd:v1", "docker login ghcr.io"} {
		if !strings.Contains(message, want) {
			t.Errorf("expected the message to contain %q, got %q", want, message)
		}
	}
}

func TestRegistryHostForReference(t *testing.T) {
	cases := []struct {
		reference string
		host      string
	}{
		{reference: "ghcr.io/educates/argocd:v1", host: "ghcr.io"},
		{reference: "registry.local:5000/crane:v1", host: "registry.local:5000"},
		// A reference with no registry is a Docker Hub one.
		{reference: "ubuntu:22.04", host: "docker.io"},
		{reference: "educates/argocd:v1", host: "docker.io"},
	}

	for _, test := range cases {
		t.Run(test.reference, func(t *testing.T) {
			if got := registryHostForReference(test.reference); got != test.host {
				t.Errorf("registryHostForReference(%q) = %q, want %q", test.reference, got, test.host)
			}
		})
	}
}
