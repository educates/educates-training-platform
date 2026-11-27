package router

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// TestRouterImageAcceptsTheRenderedConfiguration runs HAProxy's
// configuration check, in the image ROUTER_IMAGE_CHECK names, against a
// router folder rendered with every kind of route the router serves: TLS
// passed through, TLS ended at the router with its certificate, a nested
// domain, extra domains and a stopped cluster. It needs Docker, so it runs
// only when ROUTER_IMAGE_CHECK is set, as the educates-upgrade-host-images
// skill sets it before it moves the router's pin:
//
//	ROUTER_IMAGE_CHECK=<image reference> go test ./pkg/router -run TestRouterImageAcceptsTheRenderedConfiguration
func TestRouterImageAcceptsTheRenderedConfiguration(t *testing.T) {
	image := os.Getenv("ROUTER_IMAGE_CHECK")
	if image == "" {
		t.Skip("set ROUTER_IMAGE_CHECK to the image to check, such as the pinned " + Image)
	}

	dataHome := t.TempDir()
	named := func(name string) localclusters.Identity {
		t.Helper()
		id, err := localclusters.Named(name, dataHome)
		if err != nil {
			t.Fatalf("Named(%q) error = %v", name, err)
		}
		return id
	}
	passedThrough := localclusters.Default(dataHome)
	stopped := named("stopped")
	ended := named("ended")
	routes := []localclusters.RouterRoute{
		{
			Cluster:       passedThrough,
			IngressDomain: "educates.test",
			ExtraDomains:  []string{"extra.test"},
			Node:          passedThrough.ControlPlaneNode(),
			State:         localclusters.Running,
		},
		{
			Cluster:       stopped,
			IngressDomain: "stopped.educates.test",
			Node:          stopped.ControlPlaneNode(),
			State:         localclusters.Stopped,
		},
		{
			Cluster:         ended,
			IngressDomain:   "ended.test",
			ExtraDomains:    []string{"ended-extra.test"},
			Node:            ended.ControlPlaneNode(),
			State:           localclusters.Running,
			TLSEndsAtRouter: true,
		},
	}
	leaf := testpki.NewRootCA().NewLeaf(
		testpki.WithCommonName("*.ended.test"),
		testpki.WithDNSNames("ended.test", "*.ended.test"),
	)
	files := localclusters.RenderRouter(routes, localclusters.RouterCertificate{
		Domain:      "ended.test",
		Certificate: leaf.ChainPEM(),
		Key:         leaf.KeyPEM,
	})
	if err := localclusters.WriteRouterFolder(dataHome, files); err != nil {
		t.Fatalf("WriteRouterFolder() error = %v", err)
	}

	// The router runs as the CLI's user, which must be able to read the
	// folder's private keys, so the check does too.
	check := exec.Command("docker", "run", "--rm",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--volume", config.LocalRouterDir(dataHome)+":"+localclusters.RouterConfigDir+":ro",
		"--entrypoint", "haproxy",
		image,
		"-c", "-f", localclusters.RouterConfigDir+"/haproxy.cfg",
	)
	output, err := check.CombinedOutput()
	if err != nil {
		t.Fatalf("haproxy -c in %s refused the rendered configuration: %v\n%s", image, err, output)
	}
	t.Logf("haproxy -c in %s: %s", image, output)
}
