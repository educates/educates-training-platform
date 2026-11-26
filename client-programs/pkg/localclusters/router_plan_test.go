package localclusters

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// pinnedRouterImage is the router image the tests' CLI pins.
const pinnedRouterImage = "haproxy:3.4.8-alpine@sha256:8aa6"

// routerContainer is a router container created from image, configured
// to publish ports 80 and 443 on hostIP.
func routerContainer(image, hostIP string, running bool) *RouterContainer {
	addr := netip.MustParseAddr(hostIP)
	return &RouterContainer{
		Running: running,
		Image:   image,
		PortBindings: []PortBinding{
			{HostIP: addr, HostPort: 80},
			{HostIP: addr, HostPort: 443},
		},
	}
}

// planRouter plans the router of a data home whose router address is
// address, on a host with the interfaces of hostInterfaces, with router
// the router container observed, nil for none.
func planRouter(t *testing.T, address string, router *RouterContainer) (RouterPlan, error) {
	t.Helper()
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: "+address+"\n")
	inv := NewInventory(Observed{InterfaceAddresses: hostInterfaces, Router: router}, dataHome)
	return inv.PlanRouter(pinnedRouterImage)
}

func TestPlanRouterCreatesAMissingRouterOnTheRouterAddress(t *testing.T) {
	plan, err := planRouter(t, "10.10.10.1", nil)
	if err != nil {
		t.Fatalf("PlanRouter: %v", err)
	}

	if plan.Action != RouterCreate || plan.Address != "10.10.10.1" {
		t.Errorf("PlanRouter() = %+v, want to create the router on 10.10.10.1", plan)
	}
}

func TestPlanRouterOnlyReloadsARunningRouterOnThePinnedImageAndTheRouterAddress(t *testing.T) {
	plan, err := planRouter(t, "10.10.10.1", routerContainer(pinnedRouterImage, "10.10.10.1", true))
	if err != nil {
		t.Fatalf("PlanRouter: %v", err)
	}

	if plan.Action != RouterReload || plan.Address != "10.10.10.1" || plan.RecreateReason != "" {
		t.Errorf("PlanRouter() = %+v, want to reload the router on 10.10.10.1", plan)
	}
}

func TestPlanRouterRecreatesARouterThatRunsAnotherImageListensElsewhereOrHasStopped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		router *RouterContainer
		why    []string
	}{
		{
			name:   "an older pinned image",
			router: routerContainer("haproxy:3.4.7-alpine@sha256:1234", "10.10.10.1", true),
			why:    []string{"haproxy:3.4.7-alpine@sha256:1234", pinnedRouterImage},
		},
		{
			name:   "another address",
			router: routerContainer(pinnedRouterImage, "10.10.10.2", true),
			why:    []string{"10.10.10.2", "10.10.10.1"},
		},
		{
			name:   "stopped",
			router: routerContainer(pinnedRouterImage, "10.10.10.1", false),
			why:    []string{"not running"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planRouter(t, "10.10.10.1", tc.router)
			if err != nil {
				t.Fatalf("PlanRouter: %v", err)
			}

			if plan.Action != RouterRecreate || plan.Address != "10.10.10.1" {
				t.Errorf("PlanRouter() = %+v, want to re-create the router on 10.10.10.1", plan)
			}
			for _, part := range tc.why {
				if !strings.Contains(plan.RecreateReason, part) {
					t.Errorf("RecreateReason %q does not mention %q", plan.RecreateReason, part)
				}
			}
		})
	}
}

// pinnedRouterImageID is the ID a Docker host gives the image the tests'
// CLI pins, and oldRouterImageID the ID of another image.
const (
	pinnedRouterImageID = "sha256:c0ffee"
	oldRouterImageID    = "sha256:0ld"
)

// Docker reports the image a container was created from as it was given,
// with its name normalised, reduced to its digest, or as an image ID, and
// a router on the pinned image is kept whichever form it reports.
func TestPlanRouterReloadsARouterOnThePinnedImageHoweverDockerReportsIt(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		image, imageID, pinned string
	}{
		{name: "as pinned", image: pinnedRouterImage, imageID: pinnedRouterImageID, pinned: pinnedRouterImageID},
		{name: "with its name normalised", image: "docker.io/library/haproxy:3.4.8-alpine@sha256:8aa6",
			imageID: pinnedRouterImageID, pinned: pinnedRouterImageID},
		{name: "reduced to its digest", image: "docker.io/library/haproxy@sha256:8aa6",
			imageID: pinnedRouterImageID, pinned: pinnedRouterImageID},
		{name: "as the image ID", image: pinnedRouterImageID, imageID: pinnedRouterImageID, pinned: pinnedRouterImageID},
		{name: "by a tag alone", image: "haproxy:3.4.8-alpine", imageID: pinnedRouterImageID, pinned: pinnedRouterImageID},
		{name: "normalised, with the pinned image missing", image: "docker.io/library/haproxy:3.4.8-alpine@sha256:8aa6",
			imageID: oldRouterImageID},
		{name: "reduced to its digest, with the pinned image missing", image: "haproxy@sha256:8aa6", imageID: oldRouterImageID},
		// The containerd image store gives an image the digest of its
		// image index as its ID.
		{name: "as an image ID that is the pinned digest", image: "sha256:8aa6", imageID: "sha256:8aa6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := routerContainer(tc.image, "10.10.10.1", true)
			router.ImageID, router.PinnedImageID = tc.imageID, tc.pinned

			plan, err := planRouter(t, "10.10.10.1", router)
			if err != nil {
				t.Fatalf("PlanRouter: %v", err)
			}

			if plan.Action != RouterReload {
				t.Errorf("PlanRouter() = %+v, want to reload the router", plan)
			}
		})
	}
}

func TestPlanRouterRecreatesARouterOnAnotherImageHoweverDockerReportsIt(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		image, imageID, pinned string
	}{
		{name: "an older pinned image", image: "haproxy:3.4.7-alpine@sha256:1234", imageID: oldRouterImageID,
			pinned: pinnedRouterImageID},
		{name: "the pinned reference, re-pointed to another image", image: pinnedRouterImage, imageID: oldRouterImageID,
			pinned: pinnedRouterImageID},
		{name: "as another image ID", image: oldRouterImageID, imageID: oldRouterImageID, pinned: pinnedRouterImageID},
		{name: "by a tag of another image", image: "haproxy:3.4.8-alpine", imageID: oldRouterImageID,
			pinned: pinnedRouterImageID},
		{name: "another digest, with the pinned image missing", image: "docker.io/library/haproxy@sha256:1234",
			imageID: oldRouterImageID},
		{name: "a tag alone, with the pinned image missing", image: "haproxy:3.4.8-alpine", imageID: oldRouterImageID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := routerContainer(tc.image, "10.10.10.1", true)
			router.ImageID, router.PinnedImageID = tc.imageID, tc.pinned

			plan, err := planRouter(t, "10.10.10.1", router)
			if err != nil {
				t.Fatalf("PlanRouter: %v", err)
			}

			if plan.Action != RouterRecreate || !strings.Contains(plan.RecreateReason, tc.image) {
				t.Errorf("PlanRouter() = %+v, want to re-create the router, naming the image %s", plan, tc.image)
			}
		})
	}
}

// The router address rules apply wherever the router is created or
// re-created, so an address on no host interface is refused then.
func TestPlanRouterRefusesToCreateTheRouterOnAnAddressOnNoInterface(t *testing.T) {
	for _, router := range []*RouterContainer{nil, routerContainer(pinnedRouterImage, "10.10.10.2", true)} {
		_, err := planRouter(t, "10.10.10.9", router)

		wantErrorMentioning(t, err, "router.yaml", "10.10.10.9", "no network interface", "educates local router update --address")
	}
}

// A router that needs no re-creating keeps the address it runs on, even
// one the host no longer has.
func TestPlanRouterReloadsARouterWhoseAddressLeftTheInterfaces(t *testing.T) {
	plan, err := planRouter(t, "10.10.10.9", routerContainer(pinnedRouterImage, "10.10.10.9", true))
	if err != nil {
		t.Fatalf("PlanRouter: %v", err)
	}

	if plan.Action != RouterReload {
		t.Errorf("PlanRouter() = %+v, want to reload the router", plan)
	}
}

func TestPlanRouterNeedsTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()
	inv := NewInventory(Observed{InterfaceAddresses: hostInterfaces}, dataHome)

	_, err := inv.PlanRouter(pinnedRouterImage)

	wantErrorMentioning(t, err, "router.yaml", "educates local router update --address")
}

// A cluster made by an earlier release of the CLI publishes ports 80 and
// 443 from its node, which the router can't publish beside it.
func TestClustersPublishingIngressPortsAreThoseWhoseNodeIsConfiguredToPublishThem(t *testing.T) {
	dataHome := t.TempDir()
	namedForTest(t, dataHome, "old")
	namedForTest(t, dataHome, "new")
	inv := NewInventory(Observed{Nodes: []NodeContainer{
		controlPlane("educates", "10.10.10.1", false),
		kindControlPlane("educates-new", true),
		controlPlane("educates-old", "0.0.0.0", true),
		controlPlane("kind", "0.0.0.0", true),
	}}, dataHome)

	got := kindNames(inv.ClustersPublishingIngressPorts())

	if want := []string{"educates", "educates-old"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("ClustersPublishingIngressPorts() = %v, want %v", got, want)
	}
}

func TestRouterStartErrorNamesTheClustersThatStillPublishPorts80And443(t *testing.T) {
	dataHome := t.TempDir()
	namedForTest(t, dataHome, "old")
	inv := NewInventory(Observed{Nodes: []NodeContainer{
		controlPlane("educates", "10.10.10.1", true),
		controlPlane("educates-old", "10.10.10.2", false),
	}}, dataHome)
	dockerErr := errors.New("listen tcp4 0.0.0.0:443: bind: address already in use")

	err := inv.RouterStartError("10.10.10.1", dockerErr)

	wantErrorMentioning(t, err, "10.10.10.1", "80", "443", dockerErr.Error(),
		"the default local cluster, which is running", `local cluster "old", which is stopped`,
		"earlier release", "educates local cluster delete --local-cluster old",
		"educates local cluster delete\n", "create")
	if !errors.Is(err, dockerErr) {
		t.Errorf("error %q does not wrap Docker's error", err)
	}
}

func TestRouterStartErrorCarriesDockersErrorWithTheAddressAndPorts(t *testing.T) {
	inv := NewInventory(Observed{Nodes: []NodeContainer{
		kindControlPlane("educates", true),
		controlPlane("kind", "0.0.0.0", true),
	}}, t.TempDir())
	dockerErr := errors.New("Bind for 10.10.10.1:80 failed: port is already allocated")

	err := inv.RouterStartError("10.10.10.1", dockerErr)

	wantErrorMentioning(t, err, "ports 80 and 443 on 10.10.10.1", dockerErr.Error(), "Another process")
	if strings.Contains(err.Error(), "earlier release") {
		t.Errorf("error %q blames a local cluster, none of which publishes the ports", err)
	}
	if !errors.Is(err, dockerErr) {
		t.Errorf("error %q does not wrap Docker's error", err)
	}
}

// The router container runs as a user that can read the keys of the
// router folder, which only the CLI's user may read.
func TestRouterUser(t *testing.T) {
	for _, tc := range []struct {
		name            string
		securityOptions []string
		uid, gid        int
		want            string
	}{
		{name: "Docker Desktop", securityOptions: []string{"name=seccomp,profile=unconfined", "name=cgroupns"},
			uid: 501, gid: 20, want: "501:20"},
		{name: "a Linux engine run by root", securityOptions: []string{"name=apparmor", "name=seccomp,profile=builtin"},
			uid: 1000, gid: 1000, want: "1000:1000"},
		// Only root in the container maps to the host user who runs a
		// rootless engine, and who owns the keys.
		{name: "a rootless Linux engine", securityOptions: []string{"name=seccomp,profile=builtin", "name=rootless", "name=cgroupns"},
			uid: 1000, gid: 1000, want: "0:0"},
		{name: "no security options", uid: 1000, gid: 1000, want: "1000:1000"},
		// A host with no user IDs, as Windows has none, leaves the
		// image's own user.
		{name: "Windows", securityOptions: []string{"name=seccomp,profile=unconfined"}, uid: -1, gid: -1, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RouterUser(tc.securityOptions, tc.uid, tc.gid); got != tc.want {
				t.Errorf("RouterUser(%q, %d, %d) = %q, want %q", tc.securityOptions, tc.uid, tc.gid, got, tc.want)
			}
		})
	}
}
