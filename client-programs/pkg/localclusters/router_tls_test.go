package localclusters

import (
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// A cluster whose TLS ends at the router keeps https on its derived
// nip.io domain: the domain does not make it insecure, which the loader
// would refuse beside externalTLSTermination.
func TestLoadConfigOfAClusterEndingTLSAtTheRouterKeepsHTTPSOnItsNipDomain(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n"+
		"ingress:\n  externalTLSTermination: true\n")

	loaded, err := LoadConfig(Default(dataHome))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if got, want := loaded.Domain(), "10-10-10-1.nip.io"; got != want {
		t.Errorf("Domain() = %q, want %q", got, want)
	}
	c := loaded.Config.(*v1alpha1.EducatesLocalConfig)
	if c.Ingress.Insecure {
		t.Errorf("ingress.insecure = true, want false for a cluster whose TLS ends at the router")
	}
	if !c.Ingress.ExternalTLSTermination {
		t.Errorf("ingress.externalTLSTermination = false, want true as declared")
	}
}
