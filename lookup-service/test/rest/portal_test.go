package rest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/lookup-service/internal/trainingportal"
)

const (
	// portalTimeout bounds every call the suite's service makes to a
	// training portal.
	portalTimeout = 2 * time.Second

	// robotCredentialsSecret is the Secret, in a portal's namespace, that
	// the portal's status names as holding its robot credentials.
	robotCredentialsSecret = "training-portal-robot-credentials"

	// rotatedCredentialsSecret is another Secret a portal's status may name,
	// with other credentials.
	rotatedCredentialsSecret = "training-portal-robot-credentials-rotated"

	// remoteAccessRole is the Role session-manager creates in every portal
	// namespace, letting the remote-access identity get the robot
	// credentials Secret.
	remoteAccessRole = "educates-remote-access"

	// The labels the training portal sets on the sessions it creates, besides
	// environmentNameLabel.
	portalNameLabel     = "training.educates.dev/portal.name"
	portalUIDLabel      = "training.educates.dev/portal.uid"
	environmentUIDLabel = "training.educates.dev/environment.uid"
)

// The fake training portals of the scenario's two portals, and the CA their
// certificates chain to.
var (
	portalA, portalB *fakePortal
	// portalCAFile stands for the ca.crt of the CA trust Secret the
	// service's pod mounts.
	portalCAFile string
)

// portalOptions are the options the suite runs the training portal client
// with: portals are dialled at the fake portals, and the CA their
// certificates chain to is trusted.
func portalOptions() trainingportal.Options {
	return trainingportal.Options{
		Timeout:     portalTimeout,
		CAFile:      portalCAFile,
		DialContext: dialFakePortal,
	}
}

// startFakePortals starts the fake training portals of portal-a, on the
// lookup cluster, reached through its in-cluster service, and of portal-b,
// on the remote cluster, reached through its public URL over TLS, and gives
// each a robot credentials Secret the remote-access identity may get.
func startFakePortals() {
	GinkgoHelper()
	ca, caCert, caKey := newCA()
	directory := GinkgoT().TempDir()
	portalCAFile = filepath.Join(directory, "ca.crt")
	Expect(os.WriteFile(portalCAFile, ca, 0o600)).To(Succeed())

	portalA = newFakePortal("portal-a", clusterA, "portal-a-ui", "training-portal.portal-a-ui:80")
	portalA.server.Start()
	portalB = newFakePortal("portal-b", clusterB, "portal-b-ui", "portal-b-ui.lsg-golden-b.educates.test:443")
	portalB.server.TLS = &tls.Config{Certificates: []tls.Certificate{
		newServerCertificate(caCert, caKey, "portal-b-ui.lsg-golden-b.educates.test"),
	}}
	portalB.server.StartTLS()

	for _, p := range []*fakePortal{portalA, portalB} {
		DeferCleanup(p.stop)
		p.createRobotCredentials()
	}
}

// resetFakePortals makes the fake portals answer every request again, with
// the robot credentials their Secrets first held, and forget what they
// received.
func resetFakePortals() {
	GinkgoHelper()
	for _, p := range []*fakePortal{portalA, portalB} {
		p.reset()
	}
}

// dialFakePortal dials the fake portal the service reaches at address.
func dialFakePortal(ctx context.Context, network, address string) (net.Conn, error) {
	for _, p := range []*fakePortal{portalA, portalB} {
		if p.address != address {
			continue
		}
		if p.behavior() == portalUnreachable {
			return nil, fmt.Errorf("dial %s %s: connection refused", network, address)
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, p.server.Listener.Addr().String())
	}
	return nil, fmt.Errorf("dial %s %s: no such host", network, address)
}

// portalBehavior is how a fake portal answers.
type portalBehavior int

const (
	// portalAnswers answers as the training portal does.
	portalAnswers portalBehavior = iota
	// portalRefuses refuses every session request, as a full portal does.
	portalRefuses
	// portalFails answers every session request with an error.
	portalFails
	// portalHangs accepts session requests and never answers them.
	portalHangs
	// portalHangsOnToken accepts token requests and never answers them.
	portalHangsOnToken
	// portalUnreachable cannot be connected to.
	portalUnreachable
)

// robotCredentials are a training portal's robot credentials.
type robotCredentials struct {
	username, password, clientID, clientSecret string
}

func (c robotCredentials) data() map[string][]byte {
	return map[string][]byte{
		"username":      []byte(c.username),
		"password":      []byte(c.password),
		"client-id":     []byte(c.clientID),
		"client-secret": []byte(c.clientSecret),
	}
}

// tokenRequest is a request a fake portal received for an OAuth token.
type tokenRequest struct {
	grantType, username, password, clientID, clientSecret string
}

// sessionRequest is a request a fake portal received for a session.
type sessionRequest struct {
	method        string
	environment   string
	query         url.Values
	body          string
	authorization string
}

// fakePortal stands in for a training portal's REST API: it answers the
// OAuth token endpoint and the session request endpoint, records what it
// receives, and gives sessions as the portal does, handing out a reserved
// session when the environment has one and otherwise creating the
// WorkshopSession, in the Allocating phase, on its cluster.
type fakePortal struct {
	name      string
	cluster   *testCluster
	namespace string
	// address is the host and port the service reaches the portal at.
	address string
	// credentials are the credentials the robot credentials Secret first
	// holds, and rotated those of the rotated one.
	credentials, rotated robotCredentials
	server               *httptest.Server

	// giving is held while the portal gives a session.
	giving sync.Mutex

	mu sync.Mutex
	// accepted are the credentials the portal accepts.
	accepted robotCredentials
	answer   portalBehavior
	// creationDelay is how long after answering the portal creates a
	// session's WorkshopSession; 0 creates it before answering.
	creationDelay time.Duration
	tokens        map[string]bool
	tokenRequests []tokenRequest
	requests      []sessionRequest
	// created are the sessions the portal created, and pending how many
	// are yet to be, by environment.
	created  int
	pending  map[string]int
	creating sync.WaitGroup
	// released ends every request a hanging portal holds.
	released chan struct{}
}

func newFakePortal(name string, cluster *testCluster, namespace, address string) *fakePortal {
	p := &fakePortal{
		name:      name,
		cluster:   cluster,
		namespace: namespace,
		address:   address,
		credentials: robotCredentials{
			username: "robot@educates", password: name + "-robot-password",
			clientID: name + "-client-id", clientSecret: name + "-client-secret",
		},
		rotated: robotCredentials{
			username: "robot@educates", password: name + "-rotated-password",
			clientID: name + "-rotated-client-id", clientSecret: name + "-rotated-client-secret",
		},
		released: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token/", p.issueToken)
	mux.HandleFunc("/workshops/environment/{environment}/request/", p.requestSession)
	p.server = httptest.NewUnstartedServer(mux)
	p.reset()
	return p
}

// createRobotCredentials creates the portal's namespace, its robot
// credentials Secrets, and the Role and RoleBinding through which the
// remote-access identity may get them.
func (p *fakePortal) createRobotCredentials() {
	GinkgoHelper()
	ctx := context.Background()
	for _, object := range []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.namespace}},
		&corev1.Secret{ObjectMeta: objectMeta(p.namespace, robotCredentialsSecret), Data: p.credentials.data()},
		&corev1.Secret{ObjectMeta: objectMeta(p.namespace, rotatedCredentialsSecret), Data: p.rotated.data()},
		&rbacv1.Role{
			ObjectMeta: objectMeta(p.namespace, remoteAccessRole),
			Rules: []rbacv1.PolicyRule{{
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				ResourceNames: []string{robotCredentialsSecret, rotatedCredentialsSecret},
				Verbs:         []string{"get"},
			}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: objectMeta(p.namespace, remoteAccessRole),
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: remoteAccessRole},
			Subjects: []rbacv1.Subject{{
				Kind: rbacv1.ServiceAccountKind, Name: remoteAccessAccount, Namespace: remoteAccessNamespace,
			}},
		},
	} {
		Expect(p.cluster.client.Create(ctx, object)).To(Succeed())
	}
}

// reset makes the portal answer every request, accept its first
// credentials, and forget the requests and tokens it received. It waits for
// the sessions it is still to create.
func (p *fakePortal) reset() {
	p.creating.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	close(p.released)
	p.released = make(chan struct{})
	p.accepted = p.credentials
	p.answer = portalAnswers
	p.creationDelay = 0
	p.tokens = map[string]bool{}
	p.tokenRequests = nil
	p.requests = nil
	p.pending = map[string]int{}
}

func (p *fakePortal) stop() {
	p.mu.Lock()
	close(p.released)
	p.released = make(chan struct{})
	p.mu.Unlock()
	p.server.CloseClientConnections()
	p.server.Close()
}

// behave makes the portal answer as behavior says.
func (p *fakePortal) behave(behavior portalBehavior) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answer = behavior
}

func (p *fakePortal) behavior() portalBehavior {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.answer
}

// delayCreation makes the portal create the WorkshopSession of a session it
// gives that long after answering.
func (p *fakePortal) delayCreation(delay time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.creationDelay = delay
}

// accept makes the portal accept those credentials only.
func (p *fakePortal) accept(credentials robotCredentials) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.accepted = credentials
}

// revokeTokens revokes every token the portal issued.
func (p *fakePortal) revokeTokens() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens = map[string]bool{}
}

// receivedTokenRequests returns the token requests the portal received.
func (p *fakePortal) receivedTokenRequests() []tokenRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]tokenRequest{}, p.tokenRequests...)
}

// receivedRequests returns the session requests the portal received.
func (p *fakePortal) receivedRequests() []sessionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]sessionRequest{}, p.requests...)
}

// awaitCreations waits until the portal has created every session it gave.
func (p *fakePortal) awaitCreations() {
	p.creating.Wait()
}

// hold holds a request until it is cancelled or the portal released.
func (p *fakePortal) hold(r *http.Request) {
	p.mu.Lock()
	released := p.released
	p.mu.Unlock()
	select {
	case <-r.Context().Done():
	case <-released:
	}
}

func (p *fakePortal) issueToken(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret, _ := r.BasicAuth()
	if err := r.ParseForm(); err != nil {
		writePortalJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}
	received := tokenRequest{
		grantType: r.PostForm.Get("grant_type"), username: r.PostForm.Get("username"),
		password: r.PostForm.Get("password"), clientID: clientID, clientSecret: clientSecret,
	}

	p.mu.Lock()
	p.tokenRequests = append(p.tokenRequests, received)
	behavior, accepted := p.answer, p.accepted
	p.mu.Unlock()

	if behavior == portalHangsOnToken {
		p.hold(r)
		return
	}
	if received.clientID != accepted.clientID || received.clientSecret != accepted.clientSecret {
		writePortalJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
		return
	}
	if received.grantType != "password" || received.username != accepted.username ||
		received.password != accepted.password {
		writePortalJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}

	token := randomToken()
	p.mu.Lock()
	p.tokens[token] = true
	p.mu.Unlock()
	writePortalJSON(w, http.StatusOK, map[string]any{
		"access_token": token, "expires_in": 36000, "token_type": "Bearer", "scope": "user:info",
		"refresh_token": randomToken(),
	})
}

func (p *fakePortal) requestSession(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	received := sessionRequest{
		method:        r.Method,
		environment:   r.PathValue("environment"),
		query:         r.URL.Query(),
		body:          string(body),
		authorization: r.Header.Get("Authorization"),
	}

	p.mu.Lock()
	p.requests = append(p.requests, received)
	behavior := p.answer
	token, _ := strings.CutPrefix(received.authorization, "Bearer ")
	valid := p.tokens[token]
	p.mu.Unlock()

	if !valid {
		writePortalJSON(w, http.StatusUnauthorized, map[string]any{
			"detail": "Authentication credentials were not provided.",
		})
		return
	}
	switch behavior {
	case portalRefuses:
		writePortalJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "No session available"})
		return
	case portalFails:
		http.Error(w, "<h1>Server Error (500)</h1>", http.StatusInternalServerError)
		return
	case portalHangs:
		p.hold(r)
		return
	}

	indexURL := received.query.Get("index_url")
	if indexURL == "" {
		http.Error(w, "Need redirect URL for workshop index", http.StatusBadRequest)
		return
	}
	user := received.query.Get("user")
	if user == "" {
		user = string(uuid.NewUUID())
	}

	session, status, err := p.giveSession(r.Context(), received.environment, user)
	switch {
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	case status != http.StatusOK:
		writePortalJSON(w, status, map[string]any{"error": "No session available"})
		return
	}
	writePortalJSON(w, http.StatusOK, map[string]any{
		"name":    session,
		"session": session,
		"user":    user,
		"url": "/workshops/session/" + session + "/activate/?token=" + url.QueryEscape(randomToken()) +
			"&index_url=" + url.QueryEscape(indexURL),
		"namespace":   session,
		"environment": received.environment,
	})
}

// giveSession gives user a session of the environment: the session the user
// has already, a reserved session, or a new one while the environment and
// the portal have room. It returns the session's name, or the status
// refusing the request. The portal gives one session at a time, as the
// training portal does.
func (p *fakePortal) giveSession(ctx context.Context, environment, user string) (string, int, error) {
	p.giving.Lock()
	defer p.giving.Unlock()

	c := p.cluster.client
	env := &unstructured.Unstructured{}
	env.SetGroupVersionKind(workshopEnvironmentKind)
	if err := c.Get(ctx, client.ObjectKey{Name: environment}, env); err != nil {
		if apierrors.IsNotFound(err) {
			return "", http.StatusForbidden, nil
		}
		return "", 0, err
	}
	portal := &unstructured.Unstructured{}
	portal.SetGroupVersionKind(trainingPortalKind)
	if err := c.Get(ctx, client.ObjectKey{Name: p.name}, portal); err != nil {
		return "", 0, err
	}

	sessions := &unstructured.UnstructuredList{}
	sessions.SetGroupVersionKind(workshopSessionKind)
	if err := c.List(ctx, sessions); err != nil {
		return "", 0, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	inEnvironment, inPortal := p.pending[environment], 0
	for _, pending := range p.pending {
		inPortal += pending
	}
	var reserved *unstructured.Unstructured
	for i := range sessions.Items {
		session := &sessions.Items[i]
		labels := session.GetLabels()
		if labels[portalNameLabel] != p.name {
			continue
		}
		phase, _, _ := unstructured.NestedString(session.Object, "status", "educates", "phase")
		owner, _, _ := unstructured.NestedString(session.Object, "status", "educates", "user")
		if phase == "Allocating" || phase == "Allocated" {
			inPortal++
		}
		if labels[environmentNameLabel] != environment {
			continue
		}
		switch {
		case phase == "Stopping":
			continue
		case owner == user && (phase == "Allocating" || phase == "Allocated"):
			return session.GetName(), http.StatusOK, nil
		case phase == "Available" && owner == "" && reserved == nil:
			reserved = session
		}
		inEnvironment++
	}

	if reserved != nil {
		if err := unstructured.SetNestedStringMap(reserved.Object,
			map[string]string{"phase": "Allocating", "user": user}, "status", "educates"); err != nil {
			return "", 0, err
		}
		return reserved.GetName(), http.StatusOK, c.Update(ctx, reserved)
	}
	if capacity, ok, _ := unstructured.NestedInt64(env.Object, "status", "educates", "capacity"); ok &&
		int64(inEnvironment) >= capacity {
		return "", http.StatusServiceUnavailable, nil
	}
	if maximum, _, _ := unstructured.NestedInt64(portal.Object, "spec", "portal", "sessions", "maximum"); maximum > 0 &&
		int64(inPortal) >= maximum {
		return "", http.StatusServiceUnavailable, nil
	}

	p.created++
	name := fmt.Sprintf("%s-s%d", environment, p.created)
	session := p.newSession(env, name, user)
	if p.creationDelay == 0 {
		return name, http.StatusOK, c.Create(ctx, session)
	}
	p.pending[environment]++
	delay := p.creationDelay
	p.creating.Go(func() {
		time.Sleep(delay)
		_ = c.Create(context.Background(), session)
		p.mu.Lock()
		p.pending[environment]--
		p.mu.Unlock()
	})
	return name, http.StatusOK, nil
}

// newSession returns the WorkshopSession of a session the portal gives
// user, labelled as the portal labels it.
func (p *fakePortal) newSession(env *unstructured.Unstructured, name, user string) *unstructured.Unstructured {
	workshop, _, _ := unstructured.NestedString(env.Object, "spec", "workshop", "name")
	session := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"workshop":    map[string]any{"name": workshop},
			"environment": map[string]any{"name": env.GetName()},
		},
		"status": map[string]any{"educates": map[string]any{"phase": "Allocating", "user": user}},
	}}
	session.SetGroupVersionKind(workshopSessionKind)
	session.SetName(name)
	session.SetLabels(map[string]string{
		portalNameLabel:      p.name,
		portalUIDLabel:       env.GetLabels()[portalUIDLabel],
		environmentNameLabel: env.GetName(),
		environmentUIDLabel:  string(env.GetUID()),
	})
	return session
}

func writePortalJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func randomToken() string {
	return strings.ReplaceAll(string(uuid.NewUUID()), "-", "")
}

// newCA returns a new CA's certificate in PEM form, and the certificate and
// key it signs with.
func newCA() ([]byte, *x509.Certificate, *ecdsa.PrivateKey) {
	GinkgoHelper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Educates test ingress CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	cert, err := x509.ParseCertificate(der)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert, key
}

// newServerCertificate returns a certificate for host signed by the CA.
func newServerCertificate(ca *x509.Certificate, caKey *ecdsa.PrivateKey, host string) tls.Certificate {
	GinkgoHelper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	Expect(err).NotTo(HaveOccurred())
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
