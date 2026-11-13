package cluster

import (
	"context"
	"errors"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func loopbackService(serviceType apiv1.ServiceType, externalName string) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "loopback", Namespace: "default"},
		Spec:       apiv1.ServiceSpec{Type: serviceType, ExternalName: externalName},
	}
}

func TestRefreshLoopbackService(t *testing.T) {
	cases := []struct {
		name         string
		existing     *apiv1.Service
		wantChanged  bool
		wantExternal string
	}{
		{
			name:         "stale domain",
			existing:     loopbackService(apiv1.ServiceTypeExternalName, "localhost.192-168-1-131.nip.io"),
			wantChanged:  true,
			wantExternal: "localhost.workshops.example.test",
		},
		{
			name:         "current domain",
			existing:     loopbackService(apiv1.ServiceTypeExternalName, "localhost.workshops.example.test"),
			wantExternal: "localhost.workshops.example.test",
		},
		{
			name:         "unrelated external name",
			existing:     loopbackService(apiv1.ServiceTypeExternalName, "db.example.com"),
			wantExternal: "db.example.com",
		},
		{
			name:     "unrelated service type",
			existing: loopbackService(apiv1.ServiceTypeClusterIP, ""),
		},
		{
			name: "no loopback service",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var objects []runtime.Object
			if tc.existing != nil {
				objects = append(objects, tc.existing)
			}
			cs := fake.NewClientset(objects...)

			changed, err := RefreshLoopbackService(context.Background(), cs, "workshops.example.test")
			if err != nil {
				t.Fatalf("RefreshLoopbackService: %v", err)
			}
			if changed != tc.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tc.wantChanged)
			}

			for _, a := range cs.Actions() {
				if a.GetVerb() != "get" && !tc.wantChanged {
					t.Errorf("unexpected %s action on an unchanged cluster", a.GetVerb())
				}
			}
			if tc.existing == nil {
				return
			}
			got, err := cs.CoreV1().Services("default").Get(context.Background(), "loopback", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get loopback: %v", err)
			}
			if got.Spec.ExternalName != tc.wantExternal {
				t.Errorf("externalName = %q, want %q", got.Spec.ExternalName, tc.wantExternal)
			}
		})
	}
}

func TestRefreshLoopbackService_ReadErrors(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{
			name: "forbidden",
			err:  apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "loopback", errors.New("denied")),
		},
		{
			name:    "other error",
			err:     apierrors.NewServiceUnavailable("unavailable"),
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := fake.NewClientset()
			cs.PrependReactor("get", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, tc.err
			})

			changed, err := RefreshLoopbackService(context.Background(), cs, "workshops.example.test")
			if changed {
				t.Errorf("changed = true, want false")
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, want error: %v", err, tc.wantErr)
			}
		})
	}
}

func TestKindKubeconfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "/elsewhere/config")

	if got, want := KindKubeconfigPath(""), home+"/.kube/config"; got != want {
		t.Errorf("KindKubeconfigPath(\"\") = %q, want %q", got, want)
	}
	if got, want := KindKubeconfigPath("/given/config"), "/given/config"; got != want {
		t.Errorf("KindKubeconfigPath(%q) = %q, want %q", want, got, want)
	}
}
