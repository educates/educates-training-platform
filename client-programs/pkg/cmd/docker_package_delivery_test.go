package cmd

import (
	"strings"
	"testing"
)

func TestParseDockerPackageDelivery(t *testing.T) {
	cases := []struct {
		value string
		want  dockerPackageDelivery
		ok    bool
	}{
		{value: "", want: dockerPackageDeliveryAuto, ok: true},
		{value: "auto", want: dockerPackageDeliveryAuto, ok: true},
		{value: "image-mount", want: dockerPackageDeliveryImageMount, ok: true},
		{value: "fetch", want: dockerPackageDeliveryFetch, ok: true},
		// The flag names the delivery, not the cluster setting's words, so
		// the cluster spellings are not quietly accepted.
		{value: "enabled", ok: false},
		{value: "Auto", ok: false},
		{value: "mount", ok: false},
	}

	for _, test := range cases {
		t.Run(test.value, func(t *testing.T) {
			parsed, err := parseDockerPackageDelivery(test.value)

			if test.ok {
				if err != nil {
					t.Fatalf("parseDockerPackageDelivery(%q) failed: %v", test.value, err)
				}

				if parsed != test.want {
					t.Errorf("parsed = %q, want %q", parsed, test.want)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected %q to be rejected", test.value)
			}

			// The message has to name what is allowed, since the flag is the
			// only place these words appear.
			for _, want := range []string{"auto", "image-mount", "fetch"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected the error to name %q, got %q", want, err)
				}
			}
		})
	}
}

func TestDaemonSupportsImageMounts(t *testing.T) {
	// The values a current Docker Desktop reports.
	supported := daemonCapabilities{
		ServerVersion:   "29.7.2",
		ComposeVersion:  "5.5.0",
		ContainerdStore: true,
	}

	t.Run("a current daemon supports mounting", func(t *testing.T) {
		ok, reason := daemonSupportsImageMounts(supported)

		if !ok {
			t.Fatalf("expected support, got %q", reason)
		}
	})

	t.Run("the overlay2 image store is the common blocker", func(t *testing.T) {
		// A daemon upgraded in place from 28.x keeps the old image store, so
		// it can be arbitrarily new and still not mount.
		capabilities := supported
		capabilities.ContainerdStore = false

		ok, reason := daemonSupportsImageMounts(capabilities)

		if ok {
			t.Fatal("expected no support without the containerd image store")
		}

		if !strings.Contains(reason, "containerd image store") {
			t.Errorf("expected the reason to name the image store, got %q", reason)
		}
	})

	t.Run("an old engine", func(t *testing.T) {
		capabilities := supported
		capabilities.ServerVersion = "27.5.1"

		ok, reason := daemonSupportsImageMounts(capabilities)

		if ok {
			t.Fatal("expected no support on Engine 27")
		}

		if !strings.Contains(reason, "28.0") {
			t.Errorf("expected the reason to name the required version, got %q", reason)
		}
	})

	t.Run("an old compose", func(t *testing.T) {
		capabilities := supported
		capabilities.ComposeVersion = "2.20.0"

		ok, reason := daemonSupportsImageMounts(capabilities)

		if ok {
			t.Fatal("expected no support on Compose 2.20")
		}

		if !strings.Contains(reason, "2.35") {
			t.Errorf("expected the reason to name the required version, got %q", reason)
		}
	})

	t.Run("a version which cannot be read is reported rather than assumed", func(t *testing.T) {
		capabilities := supported
		capabilities.ServerVersion = "nonsense"

		ok, reason := daemonSupportsImageMounts(capabilities)

		if ok {
			t.Fatal("expected no support when the version cannot be read")
		}

		if !strings.Contains(reason, "nonsense") {
			t.Errorf("expected the reason to quote the unreadable version, got %q", reason)
		}
	})

	t.Run("an unreadable compose version does not block either", func(t *testing.T) {
		// A plugin which answers with something unparseable must not be
		// treated more harshly than one which does not answer at all.
		capabilities := supported
		capabilities.ComposeVersion = "nonsense"

		if ok, reason := daemonSupportsImageMounts(capabilities); !ok {
			t.Errorf("expected support with an unreadable compose version, got %q", reason)
		}
	})

	t.Run("a missing compose version does not block", func(t *testing.T) {
		// Compose's version comes from a separate command which may not be
		// present; that is not a reason to refuse a daemon which is otherwise
		// capable, since Compose reports its own error if it cannot do it.
		capabilities := supported
		capabilities.ComposeVersion = ""

		if ok, reason := daemonSupportsImageMounts(capabilities); !ok {
			t.Errorf("expected support with no compose version, got %q", reason)
		}
	})
}

func TestParseDockerVersion(t *testing.T) {
	cases := []struct {
		value string
		major int
		minor int
		ok    bool
	}{
		{value: "29.7.2", major: 29, minor: 7, ok: true},
		{value: "28.0.0", major: 28, minor: 0, ok: true},
		{value: "2.35.1", major: 2, minor: 35, ok: true},
		{value: "v5.5.0", major: 5, minor: 5, ok: true},
		// Docker Desktop and vendor builds carry suffixes.
		{value: "28.1.1+azure-2", major: 28, minor: 1, ok: true},
		{value: "27.5.1-rd", major: 27, minor: 5, ok: true},
		{value: "", ok: false},
		{value: "nonsense", ok: false},
		{value: "29", ok: false},
	}

	for _, test := range cases {
		t.Run(test.value, func(t *testing.T) {
			major, minor, ok := parseDockerVersion(test.value)

			if ok != test.ok {
				t.Fatalf("parseDockerVersion(%q) ok = %v, want %v", test.value, ok, test.ok)
			}

			if !test.ok {
				return
			}

			if major != test.major || minor != test.minor {
				t.Errorf("parseDockerVersion(%q) = %d.%d, want %d.%d",
					test.value, major, minor, test.major, test.minor)
			}
		})
	}
}

func TestContainerdStoreInUse(t *testing.T) {
	t.Run("the snapshotter pair is the signal", func(t *testing.T) {
		// A current daemon reports driver "overlayfs" with this pair. The
		// driver name alone is not the signal.
		if !containerdStoreInUse([][2]string{{"driver-type", "io.containerd.snapshotter.v1"}}) {
			t.Error("expected the containerd snapshotter to be recognised")
		}
	})

	t.Run("the classic graph driver is not", func(t *testing.T) {
		status := [][2]string{
			{"Backing Filesystem", "extfs"},
			{"Supports d_type", "true"},
		}

		if containerdStoreInUse(status) {
			t.Error("expected overlay2 status not to be mistaken for the snapshotter")
		}
	})

	t.Run("no status at all", func(t *testing.T) {
		if containerdStoreInUse(nil) {
			t.Error("expected an empty status not to claim support")
		}
	})
}
