package cmd

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// escapeKindWithCRs is an EducatesConfig for a kind cluster whose custom
// resources declare their own domain and plain HTTP ingress.
const escapeKindWithCRs = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesConfig
target:
  provider: kind
educatesClusterConfig:
  mode: Managed
  ingress:
    domain: workshops.example.test
    ingressClassName: contour
    protocol: http
    controller:
      provider: BundledContour
      bundledContour:
        envoyServiceType: ClusterIP
    certificates:
      provider: None
secretsManager: {}
sessionManager: {}
`

// prepareForTest loads the config at path as local cluster create does
// and prepares it, with a developer-built CLI's project info so that any
// CLI defaulting would show in the result.
func prepareForTest(t *testing.T, path string) *localCreateInputs {
	t.Helper()
	cfg, _, _, err := loadLocalConfig(&LocalClusterCreateOptions{Config: path}, defaultLocalCluster())
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}
	p := &ProjectInfo{Version: "latest", ImageRepository: "localhost:5001"}
	in, err := p.prepareLocalCreate(cfg)
	if err != nil {
		t.Fatalf("prepareLocalCreate: %v", err)
	}
	return in
}

// translateForTest translates the prepared config as the deploy
// tail-call does.
func translateForTest(t *testing.T, in *localCreateInputs) *translator.Output {
	t.Helper()
	opts := translator.Options{}
	if in.Plan != nil {
		opts = in.Plan.TranslatorOptions()
	}
	out, err := translator.Translate(in.Deploy, opts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	return out
}

func TestPrepareLocalCreate_EscapeConfig_DeploysItsCustomResources(t *testing.T) {
	in := prepareForTest(t, writeFixture(t, escapeKindWithCRs))

	escape, ok := in.Deploy.(*v1alpha1.EducatesConfig)
	if !ok {
		t.Fatalf("Deploy = %T, want *v1alpha1.EducatesConfig", in.Deploy)
	}
	if in.Plan != nil {
		t.Errorf("Plan = %+v, want nil (no local certificates for EducatesConfig)", in.Plan)
	}
	if got, want := in.Domain, "workshops.example.test"; got != want {
		t.Errorf("Domain = %q, want %q", got, want)
	}
	if got, want := in.Cluster.ListenAddress, "127.0.0.1"; got != want {
		t.Errorf("Cluster.ListenAddress = %q, want %q (local kind's default)", got, want)
	}

	out := translateForTest(t, in)
	if want := translator.TranslateEscape(escape); !reflect.DeepEqual(out, want) {
		t.Errorf("deploy output differs from TranslateEscape:\n got: %#v\nwant: %#v", out, want)
	}
	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	ingress := spec["ingress"].(map[string]interface{})
	if got, want := ingress["domain"], "workshops.example.test"; got != want {
		t.Errorf("EducatesClusterConfig ingress.domain = %v, want %q", got, want)
	}
	if out.LookupService != nil {
		t.Errorf("LookupService = %v, want nil (not declared in the config)", out.LookupService)
	}
}

func TestPrepareLocalCreate_EscapeWithTarget_NoCLIDefaults(t *testing.T) {
	in := prepareForTest(t, filepath.Join("..", "config", "testdata", "escape-with-target.yaml"))

	if _, ok := in.Deploy.(*v1alpha1.EducatesConfig); !ok {
		t.Fatalf("Deploy = %T, want *v1alpha1.EducatesConfig", in.Deploy)
	}
	if in.Plan != nil {
		t.Errorf("Plan = %+v, want nil", in.Plan)
	}
	if in.Domain != "" {
		t.Errorf("Domain = %q, want empty (no educatesClusterConfig declared)", in.Domain)
	}

	out := translateForTest(t, in)
	image, _ := out.OperatorChartValues["image"].(map[string]interface{})
	if got, want := image["repository"], "ghcr.io/educates/educates-operator"; got != want {
		t.Errorf("operator image.repository = %v, want %q (from the config, not the CLI)", got, want)
	}
	if got, want := image["tag"], "4.0.0"; got != want {
		t.Errorf("operator image.tag = %v, want %q (from the config, not the CLI)", got, want)
	}
	smSpec := out.SessionManager["spec"].(map[string]interface{})
	if images, ok := smSpec["images"]; ok {
		t.Errorf("SessionManager spec.images = %v, want unset (no CLI image defaults)", images)
	}
	if out.LookupService != nil {
		t.Errorf("LookupService = %v, want nil (not declared in the config)", out.LookupService)
	}
}

func TestLoadLocalConfig_EscapeTargetProvider(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		wantErr string
	}{
		{name: "kind", target: "target:\n  provider: kind\n"},
		{name: "other spelling", target: "target:\n  provider: Kind\n", wantErr: "schema validation failed"},
		{name: "other provider", target: "target:\n  provider: gke\n", wantErr: "schema validation failed"},
		{name: "no provider", target: "target:\n  cluster: {}\n", wantErr: "target.provider=kind"},
		{name: "no target", target: "", wantErr: "target.provider=kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFixture(t, escapeMinimal+tc.target)
			cfg, _, _, err := loadLocalConfig(&LocalClusterCreateOptions{Config: path}, defaultLocalCluster())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want an error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadLocalConfig: %v", err)
			}
			if _, ok := cfg.(*v1alpha1.EducatesConfig); !ok {
				t.Errorf("config = %T, want *v1alpha1.EducatesConfig", cfg)
			}
		})
	}
}

func TestKindBootstrapFromConfig_CarriesTemplateFields(t *testing.T) {
	tr := true
	cfg := &v1alpha1.EducatesLocalConfig{
		Cluster: v1alpha1.LocalClusterConfig{
			ListenAddress: "192.168.50.50",
			ApiServer: v1alpha1.ApiServerConfig{
				Address: "192.168.50.50",
				Port:    6443,
			},
			Networking: v1alpha1.NetworkingConfig{
				ServiceSubnet: "10.96.0.0/12",
				PodSubnet:     "10.244.0.0/16",
			},
			VolumeMounts: []v1alpha1.VolumeMount{
				{HostPath: "/tmp/data", ContainerPath: "/data", ReadOnly: &tr},
			},
		},
	}
	in := kindBootstrapFromConfig(cfg.Cluster)

	if got, want := in.ListenAddress, "192.168.50.50"; got != want {
		t.Errorf("ListenAddress = %q, want %q", got, want)
	}
	if got, want := in.ApiServer.Port, 6443; got != want {
		t.Errorf("ApiServer.Port = %d, want %d", got, want)
	}
	if got, want := in.Networking.PodSubnet, "10.244.0.0/16"; got != want {
		t.Errorf("Networking.PodSubnet = %q, want %q", got, want)
	}
	if got := len(in.VolumeMounts); got != 1 {
		t.Fatalf("VolumeMounts len = %d, want 1", got)
	}
	if got, want := in.VolumeMounts[0].HostPath, "/tmp/data"; got != want {
		t.Errorf("VolumeMounts[0].HostPath = %q, want %q", got, want)
	}
	if !in.VolumeMounts[0].HasReadOnly || !in.VolumeMounts[0].ReadOnly {
		t.Errorf("VolumeMounts[0] readOnly pair = {Has=%v, RO=%v}, want {true, true}",
			in.VolumeMounts[0].HasReadOnly, in.VolumeMounts[0].ReadOnly)
	}
}

func TestKindBootstrapFromConfig_VolumeMountWithoutReadOnly_LeavesPairUnset(t *testing.T) {
	cfg := &v1alpha1.EducatesLocalConfig{
		Cluster: v1alpha1.LocalClusterConfig{
			VolumeMounts: []v1alpha1.VolumeMount{
				{HostPath: "/tmp/data", ContainerPath: "/data"}, // ReadOnly nil
			},
		},
	}
	in := kindBootstrapFromConfig(cfg.Cluster)
	if in.VolumeMounts[0].HasReadOnly {
		t.Errorf("HasReadOnly = true, want false (source ReadOnly was nil)")
	}
}

func TestKindBootstrapFromConfig_Empty_NoCrash(t *testing.T) {
	in := kindBootstrapFromConfig(v1alpha1.LocalClusterConfig{})
	if in == nil {
		t.Fatal("nil result")
	}
	if got := len(in.VolumeMounts); got != 0 {
		t.Errorf("empty VolumeMounts len = %d, want 0", got)
	}
}
