package localclusters

import (
	"strings"
	"testing"
)

func TestCheckPrivilegedPortForwarding(t *testing.T) {
	cases := []struct {
		name        string
		platform    string
		setting     PrivilegedPortForwarding
		wantRefusal bool
		wantWarning bool
	}{
		{name: "Docker Desktop 4.97 with the setting off", platform: "Docker Desktop 4.97.0",
			setting: PrivilegedPortForwardingOff, wantRefusal: true},
		{name: "a later Docker Desktop with the setting off", platform: "Docker Desktop 5.2.1 (210001)",
			setting: PrivilegedPortForwardingOff, wantRefusal: true},
		{name: "Docker Desktop 4.97 with the setting on", platform: "Docker Desktop 4.97.0",
			setting: PrivilegedPortForwardingOn},
		{name: "Docker Desktop 4.97 with the setting unknown", platform: "Docker Desktop 4.97.0",
			setting: PrivilegedPortForwardingUnknown, wantWarning: true},
		{name: "an earlier Docker Desktop with the setting off", platform: "Docker Desktop 4.96.2 (205001)",
			setting: PrivilegedPortForwardingOff},
		{name: "an earlier Docker Desktop with the setting unknown", platform: "Docker Desktop 4.49.0 (208003)",
			setting: PrivilegedPortForwardingUnknown},
		{name: "Docker Engine", platform: "Docker Engine - Community", setting: PrivilegedPortForwardingUnknown},
		{name: "an engine that names no platform", platform: "", setting: PrivilegedPortForwardingOff},
		{name: "Docker Desktop with no version", platform: "Docker Desktop ", setting: PrivilegedPortForwardingOff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			warning, err := CheckPrivilegedPortForwarding(tc.platform, tc.setting)

			if tc.wantRefusal {
				wantErrorMentioning(t, err, "Settings > Advanced > Allow privileged port forwarding to external machines",
					"ports 80 and 443", "non-loopback address")
			} else if err != nil {
				t.Errorf("CheckPrivilegedPortForwarding refused: %v", err)
			}
			switch {
			case tc.wantWarning && !strings.Contains(warning, "Allow privileged port forwarding to external machines"):
				t.Errorf("warning = %q, want one naming the setting", warning)
			case !tc.wantWarning && warning != "":
				t.Errorf("warning = %q, want none", warning)
			}
		})
	}
}
