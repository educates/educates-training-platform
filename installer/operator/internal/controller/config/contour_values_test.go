/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"testing"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

func contourConfig(serviceType configv1alpha1.EnvoyServiceType) *configv1alpha1.EducatesClusterConfig {
	bc := &configv1alpha1.BundledContourConfig{}
	if serviceType != "" {
		bc.EnvoyServiceType = serviceType
	}
	return &configv1alpha1.EducatesClusterConfig{
		Spec: configv1alpha1.EducatesClusterConfigSpec{
			Ingress: &configv1alpha1.Ingress{
				Domain:           "test.example.com",
				IngressClassName: "contour",
				Controller: configv1alpha1.IngressController{
					Provider:       configv1alpha1.IngressControllerProviderBundledContour,
					BundledContour: bc,
				},
			},
		},
	}
}

func envoyUseHostPort(values map[string]any) (map[string]any, bool) {
	envoy, ok := values["envoy"].(map[string]any)
	if !ok {
		return nil, false
	}
	uhp, ok := envoy["useHostPort"].(map[string]any)
	return uhp, ok
}

func envoyTolerations(values map[string]any) ([]any, bool) {
	envoy, ok := values["envoy"].(map[string]any)
	if !ok {
		return nil, false
	}
	tolerations, ok := envoy["tolerations"].([]any)
	return tolerations, ok
}

func envoyService(values map[string]any) (map[string]any, bool) {
	envoy, ok := values["envoy"].(map[string]any)
	if !ok {
		return nil, false
	}
	svc, ok := envoy["service"].(map[string]any)
	return svc, ok
}

// ClusterIP Envoy has no LB/NodePort, so the operator must bind the node's
// 80/443 via hostPort (the kind topology, mirroring v3).
func TestRenderContourValues_ClusterIPEnablesHostPort(t *testing.T) {
	values := renderContourValues(contourConfig(configv1alpha1.EnvoyServiceTypeClusterIP))

	uhp, ok := envoyUseHostPort(values)
	if !ok {
		t.Fatal("ClusterIP service did not enable envoy.useHostPort")
	}
	if uhp["http"] != true || uhp["https"] != true {
		t.Errorf("useHostPort = %v, want http+https true", uhp)
	}
}

// LoadBalancer (cloud) and NodePort front Envoy themselves; no hostPort.
func TestRenderContourValues_NonClusterIPNoHostPort(t *testing.T) {
	for _, st := range []configv1alpha1.EnvoyServiceType{
		configv1alpha1.EnvoyServiceTypeLoadBalancer,
		configv1alpha1.EnvoyServiceTypeNodePort,
		"", // default → LoadBalancer
	} {
		values := renderContourValues(contourConfig(st))
		if _, ok := envoyUseHostPort(values); ok {
			t.Errorf("serviceType %q unexpectedly enabled envoy.useHostPort", st)
		}
	}
}

// kind publishes 80/443 from the control-plane node only and keeps its
// NoSchedule taint when the cluster has workers, so a ClusterIP Envoy must
// tolerate it or no Envoy binds the published ports.
func TestRenderContourValues_ClusterIPToleratesControlPlane(t *testing.T) {
	values := renderContourValues(contourConfig(configv1alpha1.EnvoyServiceTypeClusterIP))

	tolerations, ok := envoyTolerations(values)
	if !ok {
		t.Fatal("ClusterIP service did not set envoy.tolerations")
	}
	if len(tolerations) != 1 {
		t.Fatalf("envoy.tolerations = %v, want exactly the control-plane toleration", tolerations)
	}
	toleration, ok := tolerations[0].(map[string]any)
	if !ok {
		t.Fatalf("envoy.tolerations[0] = %T, want map[string]any", tolerations[0])
	}
	want := map[string]any{
		"key":      "node-role.kubernetes.io/control-plane",
		"operator": "Equal",
		"effect":   "NoSchedule",
	}
	if len(toleration) != len(want) {
		t.Errorf("envoy.tolerations[0] = %v, want %v", toleration, want)
	}
	for k, v := range want {
		if toleration[k] != v {
			t.Errorf("envoy.tolerations[0][%q] = %v, want %v", k, toleration[k], v)
		}
	}
}

// LoadBalancer (cloud) and NodePort reach Envoy through a Service, so it
// keeps the chart's default placement and no control-plane toleration.
func TestRenderContourValues_NonClusterIPNoTolerations(t *testing.T) {
	for _, st := range []configv1alpha1.EnvoyServiceType{
		configv1alpha1.EnvoyServiceTypeLoadBalancer,
		configv1alpha1.EnvoyServiceTypeNodePort,
		"", // default → LoadBalancer
	} {
		values := renderContourValues(contourConfig(st))
		envoy, ok := values["envoy"].(map[string]any)
		if !ok {
			t.Fatalf("serviceType %q: envoy values missing", st)
		}
		if _, present := envoy["tolerations"]; present {
			t.Errorf("serviceType %q unexpectedly set envoy.tolerations", st)
		}
	}
}

// The chart defaults envoy.service.externalTrafficPolicy to "Local", which
// the API rejects on a ClusterIP Service and fails the whole release. The
// operator must clear it to the empty string for ClusterIP so the chart
// omits the field.
func TestRenderContourValues_ClusterIPClearsExternalTrafficPolicy(t *testing.T) {
	values := renderContourValues(contourConfig(configv1alpha1.EnvoyServiceTypeClusterIP))

	svc, ok := envoyService(values)
	if !ok {
		t.Fatal("envoy.service missing")
	}
	etp, present := svc["externalTrafficPolicy"]
	if !present {
		t.Fatal("envoy.service.externalTrafficPolicy not set for ClusterIP; want empty string to override chart default")
	}
	if etp != "" {
		t.Errorf("externalTrafficPolicy = %q, want empty string", etp)
	}
}

// For LoadBalancer / NodePort the operator must not touch
// externalTrafficPolicy — the chart default ("Local") is valid for those
// externally-accessible service types.
func TestRenderContourValues_NonClusterIPLeavesExternalTrafficPolicy(t *testing.T) {
	for _, st := range []configv1alpha1.EnvoyServiceType{
		configv1alpha1.EnvoyServiceTypeLoadBalancer,
		configv1alpha1.EnvoyServiceTypeNodePort,
		"", // default → LoadBalancer
	} {
		values := renderContourValues(contourConfig(st))
		svc, ok := envoyService(values)
		if !ok {
			t.Fatalf("serviceType %q: envoy.service missing", st)
		}
		if _, present := svc["externalTrafficPolicy"]; present {
			t.Errorf("serviceType %q unexpectedly set externalTrafficPolicy; chart default must stand", st)
		}
	}
}

func trustedHops(values map[string]any) (any, bool) {
	inline, ok := values["configInline"].(map[string]any)
	if !ok {
		return nil, false
	}
	network, ok := inline["network"].(map[string]any)
	if !ok {
		return nil, false
	}
	hops, ok := network["num-trusted-hops"]
	return hops, ok
}

// A None certificates provider with ingress.protocol https declares that
// TLS ends in front of the cluster, at a proxy that sets the forwarded
// headers: Envoy trusts that one hop, so X-Forwarded-Proto: https reaches
// the backends instead of being overwritten with http.
func TestRenderContourValues_TLSEndedInFrontTrustsOneHop(t *testing.T) {
	obj := contourConfig(configv1alpha1.EnvoyServiceTypeClusterIP)
	obj.Spec.Ingress.Certificates.Provider = configv1alpha1.CertificatesProviderNone
	obj.Spec.Ingress.Protocol = configv1alpha1.IngressProtocolHTTPS

	hops, ok := trustedHops(renderContourValues(obj))
	if !ok || hops != 1 {
		t.Fatalf("configInline.network.num-trusted-hops = %v (set %v), want 1", hops, ok)
	}
}

// Envoy is the first proxy a request meets when it ends TLS itself, or when
// the cluster serves plain HTTP: trusting a hop would let a client set the
// forwarded headers, so Contour keeps its default of none.
func TestRenderContourValues_NoProxyInFrontTrustsNoHop(t *testing.T) {
	cases := map[string]func(*configv1alpha1.EducatesClusterConfig){
		"None provider, plain HTTP": func(o *configv1alpha1.EducatesClusterConfig) {
			o.Spec.Ingress.Certificates.Provider = configv1alpha1.CertificatesProviderNone
		},
		"None provider, protocol http": func(o *configv1alpha1.EducatesClusterConfig) {
			o.Spec.Ingress.Certificates.Provider = configv1alpha1.CertificatesProviderNone
			o.Spec.Ingress.Protocol = configv1alpha1.IngressProtocolHTTP
		},
		"certificate in the cluster, protocol https": func(o *configv1alpha1.EducatesClusterConfig) {
			o.Spec.Ingress.Certificates.Provider = configv1alpha1.CertificatesProviderBundledCertManager
			o.Spec.Ingress.Protocol = configv1alpha1.IngressProtocolHTTPS
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			obj := contourConfig(configv1alpha1.EnvoyServiceTypeLoadBalancer)
			set(obj)
			if hops, ok := trustedHops(renderContourValues(obj)); ok {
				t.Fatalf("configInline.network.num-trusted-hops = %v, want it unset", hops)
			}
		})
	}
}
