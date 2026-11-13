package resolver

import (
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestMountsIncludeFolder(t *testing.T) {
	cases := []struct {
		name   string
		mounts []container.MountPoint
		want   bool
	}{
		{
			name: "a container that mounts the include folder",
			mounts: []container.MountPoint{
				{Type: "bind", Source: "/data/dnsmasq.conf", Destination: "/etc/dnsmasq.conf"},
				{Type: "bind", Source: "/data/dnsmasq.d", Destination: "/etc/dnsmasq.d"},
			},
			want: true,
		},
		{
			name: "a container created before the include folder",
			mounts: []container.MountPoint{
				{Type: "bind", Source: "/data/dnsmasq.conf", Destination: "/etc/dnsmasq.conf"},
			},
		},
		{
			name: "a container with no mounts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mountsIncludeFolder(tc.mounts); got != tc.want {
				t.Errorf("mountsIncludeFolder = %t, want %t", got, tc.want)
			}
		})
	}
}
