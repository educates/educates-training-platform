package tokens_test

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/tokens"
)

const namespace = "educates"

var (
	alice = config.Client{Name: "alice", UID: "uid-alice", Password: "alice-password"}
	bob   = config.Client{Name: "bob", UID: "uid-bob", Password: "bob-password"}
)

// newAPIServer returns a fake API server, which refuses an update made with
// a stale resourceVersion as a real one does, with the calls funcs intercept.
func newAPIServer(t *testing.T, funcs interceptor.Funcs) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(funcs).Build()
}

// open opens a store on the API server, as one instance of the service does.
func open(t *testing.T, api client.Client, opts tokens.Options) *tokens.Store {
	t.Helper()
	store, err := tokens.Open(context.Background(), api, api, namespace, opts)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	return store
}

// issue logs the client in on the store and returns its token's claims.
func issue(t *testing.T, store *tokens.Store, c config.Client) tokens.Claims {
	t.Helper()
	token, err := store.Issue(context.Background(), c)
	if err != nil {
		t.Fatalf("issuing a token for %s: %v", c.Name, err)
	}
	claims, err := store.Verify(token.Value)
	if err != nil {
		t.Fatalf("verifying the token of %s: %v", c.Name, err)
	}
	return claims
}

func TestALogoutAnotherInstanceRecordedSurvivesThisInstancesWrite(t *testing.T) {
	api := newAPIServer(t, interceptor.Funcs{})
	older := open(t, api, tokens.Options{})
	claims := issue(t, older, alice)

	newer := open(t, api, tokens.Options{})
	if !newer.Authenticates(alice, claims) {
		t.Fatal("the newer instance refuses a token the older one issued")
	}
	if err := older.Revoke(context.Background(), alice); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	// The newer instance's write meets the older one's, and must keep it.
	issue(t, newer, bob)

	if newer.Authenticates(alice, claims) {
		t.Error("the newer instance accepts the token the older one revoked")
	}
	if open(t, api, tokens.Options{}).Authenticates(alice, claims) {
		t.Error("the Secret lost the revocation: an instance started later accepts the revoked token")
	}
}

func TestARevocationAnotherInstanceRecordedIsSeenWithoutAWrite(t *testing.T) {
	api := newAPIServer(t, interceptor.Funcs{})
	older := open(t, api, tokens.Options{})
	claims := issue(t, older, alice)

	newer := open(t, api, tokens.Options{RefreshInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = newer.Start(ctx) }()

	if err := older.Revoke(context.Background(), alice); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for newer.Authenticates(alice, claims) {
		if time.Now().After(deadline) {
			t.Fatal("the newer instance still accepts the token the older one revoked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestASlowAPIServerDelaysNeitherVerificationNorTheWriteForLong(t *testing.T) {
	stall := make(chan struct{})
	defer close(stall)
	stalled := false
	api := newAPIServer(t, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if !stalled {
				return c.Update(ctx, obj, opts...)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-stall:
				return c.Update(ctx, obj, opts...)
			}
		},
	})
	store := open(t, api, tokens.Options{WriteTimeout: 300 * time.Millisecond})
	token, err := store.Issue(context.Background(), alice)
	if err != nil {
		t.Fatal(err)
	}
	stalled = true

	issued := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := store.Issue(context.Background(), bob)
		issued <- err
	}()
	time.Sleep(50 * time.Millisecond)

	checked := make(chan bool, 1)
	go func() {
		claims, err := store.Verify(token.Value)
		checked <- err == nil && store.Authenticates(alice, claims)
	}()
	select {
	case ok := <-checked:
		if !ok {
			t.Error("the token issued before the API server slowed down is refused")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("verifying a token waits for the write of the Secret")
	}

	select {
	case err := <-issued:
		if err == nil {
			t.Error("a token was issued although its client's state was not recorded")
		}
		if took := time.Since(started); took > 2*time.Second {
			t.Errorf("the write took %s to give up", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the write of the Secret is not bounded in time")
	}
}
