package cmd

import (
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func TestLookupKubeconfigServerOfAKubeContext(t *testing.T) {
	dataHome := t.TempDir()
	named, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatalf("Named: %v", err)
	}
	if err := localclusters.MakeClusterFolder(named); err != nil {
		t.Fatalf("MakeClusterFolder: %v", err)
	}

	cases := []struct {
		name          string
		kubeContext   string
		contextServer string
		want          string
	}{
		{
			name:          "the default local cluster",
			kubeContext:   "kind-educates",
			contextServer: "https://127.0.0.1:59223",
			want:          "https://educates-control-plane:6443",
		},
		{
			name:          "a named local cluster",
			kubeContext:   "kind-educates-test2",
			contextServer: "https://127.0.0.1:59224",
			want:          "https://educates-test2-control-plane:6443",
		},
		{
			name:          "a kind cluster named like a local cluster the data home has no folder for",
			kubeContext:   "kind-educates-other",
			contextServer: "https://127.0.0.1:59225",
			want:          "https://127.0.0.1:59225",
		},
		{
			name:          "a cluster that is no local cluster",
			kubeContext:   "gke_project_region_cluster-1",
			contextServer: "https://34.1.2.3",
			want:          "https://34.1.2.3",
		},
		{
			name:          "no kube context, as in a pod",
			kubeContext:   "",
			contextServer: "https://10.96.0.1:443",
			want:          "https://10.96.0.1:443",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := lookupKubeconfigServer("", tc.kubeContext, tc.contextServer, dataHome)
			if err != nil {
				t.Fatalf("lookupKubeconfigServer: %v", err)
			}
			if got != tc.want {
				t.Errorf("lookupKubeconfigServer(%q) = %q, want %q", tc.kubeContext, got, tc.want)
			}
		})
	}
}

func TestLookupKubeconfigServerGivenWithServer(t *testing.T) {
	dataHome := t.TempDir()

	for _, kubeContext := range []string{"kind-educates", "gke_project_region_cluster-1"} {
		t.Run(kubeContext, func(t *testing.T) {
			got, err := lookupKubeconfigServer("https://api.cluster-1.example.com:6443", kubeContext, "https://127.0.0.1:59223", dataHome)
			if err != nil {
				t.Fatalf("lookupKubeconfigServer: %v", err)
			}
			if want := "https://api.cluster-1.example.com:6443"; got != want {
				t.Errorf("lookupKubeconfigServer = %q, want %q", got, want)
			}
		})
	}
}

func TestLookupKubeconfigServerRefusesAServerThatIsNoHTTPSAddress(t *testing.T) {
	dataHome := t.TempDir()

	for _, server := range []string{
		"http://api.cluster-1.example.com:6443",
		"api.cluster-1.example.com:6443",
		"https://",
		"https://api.cluster-1.example.com:6443/%zz",
	} {
		t.Run(server, func(t *testing.T) {
			_, err := lookupKubeconfigServer(server, "kind-educates", "https://127.0.0.1:59223", dataHome)
			if err == nil {
				t.Fatalf("lookupKubeconfigServer(%q) gave no error", server)
			}
		})
	}
}
