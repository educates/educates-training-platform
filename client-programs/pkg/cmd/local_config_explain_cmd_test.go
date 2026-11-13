package cmd

import (
	"bytes"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
)

func TestExplainSchemaForKind(t *testing.T) {
	cases := []struct {
		kind    string
		wantSch []byte
	}{
		{"", schemas.EducatesLocalConfig},
		{"local", schemas.EducatesLocalConfig},
		{"EducatesLocalConfig", schemas.EducatesLocalConfig},
		{"gke", schemas.EducatesGKEConfig},
		{"EducatesGKEConfig", schemas.EducatesGKEConfig},
		{"eks", schemas.EducatesEKSConfig},
		{"inline", schemas.EducatesInlineConfig},
		{"escape", schemas.EducatesConfig},
		{"EducatesConfig", schemas.EducatesConfig},
		{"mirrors", schemas.EducatesLocalMirrors},
		{"EducatesLocalMirrors", schemas.EducatesLocalMirrors},
	}
	for _, tc := range cases {
		sch, err := explainSchemaForKind(tc.kind)
		if err != nil {
			t.Fatalf("explainSchemaForKind(%q): %v", tc.kind, err)
		}
		if &sch[0] != &tc.wantSch[0] {
			t.Errorf("explainSchemaForKind(%q) selected the wrong schema", tc.kind)
		}
	}

	if _, err := explainSchemaForKind("bogus"); err == nil {
		t.Error("expected an error for an unknown kind")
	}
}

// End-to-end through the command: --kind routes to the GKE schema and the
// path resolves a documented field.
func TestLocalConfigExplain_KindFlagRoutes(t *testing.T) {
	p := ProjectInfo{}
	cmd := p.NewLocalConfigExplainCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"gcp.project", "--kind", "gke"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("FIELD:    gcp.project")) {
		t.Errorf("expected the GKE gcp.project field:\n%s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("Cloud DNS")) {
		t.Errorf("expected the GKE gcp.project description:\n%s", buf.String())
	}
}

// The resolver section explains the one resolver every local cluster
// shares, where its names resolve, and where other names go.
func TestLocalConfigExplain_Resolver_ExplainsTheSharedResolver(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{"resolver", []string{"every local cluster shares", "cluster.listenAddress", "dnsmasq.d"}},
		{"cluster.listenAddress", []string{"specific IP address", "0.0.0.0", "loopback", "educates local config init", "10.10.10.1", "Allow privileged port forwarding to external machines"}},
		{"resolver.extraDomains", []string{"subdomains", "another local cluster", "error"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			p := ProjectInfo{}
			cmd := p.NewLocalConfigExplainCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetArgs([]string{tc.path})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			for _, want := range tc.want {
				if !bytes.Contains(buf.Bytes(), []byte(want)) {
					t.Errorf("explain %s does not mention %q:\n%s", tc.path, want, buf.String())
				}
			}
			if bytes.Contains(buf.Bytes(), []byte("targetAddress")) {
				t.Errorf("explain %s still mentions targetAddress:\n%s", tc.path, buf.String())
			}
		})
	}
}

// The registry mirrors file's kind explains the mirrors every local
// cluster shares.
func TestLocalConfigExplain_MirrorsKind_ExplainsTheRegistryMirrors(t *testing.T) {
	p := ProjectInfo{}
	cmd := p.NewLocalConfigExplainCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"registryMirrors", "--kind", "mirrors"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{"FIELD:    registryMirrors <[]Object>", "every local cluster", "mirror", "password"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("explain registryMirrors --kind mirrors does not mention %q:\n%s", want, buf.String())
		}
	}
}

func TestLocalConfigExplain_UnknownKind_Errors(t *testing.T) {
	p := ProjectInfo{}
	cmd := p.NewLocalConfigExplainCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--kind", "bogus"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an unknown --kind")
	}
}
