// Package trainingportal is the Lookup Service's client of the training
// portals' REST API. It reads each portal's robot credentials from the
// Secret the portal's status names, obtains and caches the portal's OAuth
// tokens, requests workshop sessions and reacquires a user's session.
//
// A portal on the cluster the service runs on is reached through its
// in-cluster service, and a portal on another cluster through its public
// URL. TLS is verified against the system roots and the ingress CA the
// service's pod mounts, and every call is bounded by a timeout measured in
// seconds, or by the caller's deadline when that comes first.
package trainingportal

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
)

// The defaults of Options.
const (
	DefaultTimeout = 10 * time.Second
	// DefaultCAFile is where the service's pod mounts the ca.crt of the CA
	// trust Secret, the operator's key-free copy of the ingress CA.
	DefaultCAFile = "/opt/app-root/certs/ingress-ca/ca.crt"
)

const (
	// tokenRefreshMargin is how long before a token expires a new one is
	// obtained.
	tokenRefreshMargin = time.Minute
	// maxResponseBody is the most of a portal's response that is read.
	maxResponseBody = 1 << 20
	// excerptLength is the most of a portal's error response that a failure
	// quotes.
	excerptLength = 200
)

// Options configure the client.
type Options struct {
	// Timeout bounds every call to a training portal, and every read of a
	// portal's robot credentials, unless the caller's deadline comes first.
	Timeout time.Duration
	// CAFile is a PEM file of CA certificates trusted besides the system
	// roots. Without the file only the system roots are trusted.
	CAFile string
	// DialContext dials the portals, in place of a plain network dialer.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
}

func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.CAFile == "" {
		o.CAFile = DefaultCAFile
	}
	return o
}

// ReaderFor returns a reader of the API server of the cluster of that name,
// reading as the identity the cluster is reached as, and whether there is
// one yet.
type ReaderFor func(cluster string) (client.Reader, bool)

// SessionRequest is what a new session is requested with.
type SessionRequest struct {
	// User names the portal user the session is for; the portal creates one
	// when it is empty.
	User         string
	Email        string
	FirstName    string
	LastName     string
	IndexURL     string
	AnalyticsURL string
	// Parameters are the workshop parameters, passed to the portal as given.
	// None is an empty list.
	Parameters json.RawMessage
}

// Session is a session a portal gave.
type Session struct {
	Name string
	// ActivationURL is where the session's user activates it: the portal's
	// public URL followed by the path the portal returned.
	ActivationURL string
}

// Client requests sessions from training portals. It is safe for
// concurrent use.
type Client struct {
	http    *http.Client
	timeout time.Duration
	readers ReaderFor

	mu      sync.Mutex
	portals map[portalKey]*portalState
}

type portalKey struct{ cluster, name string }

// credentialsSource is what a portal's robot credentials are read for: the
// portal's uid, and the namespace and name of its robot credentials Secret.
type credentialsSource struct{ uid, namespace, secret string }

// portalState is what the client keeps of a portal: its robot credentials,
// once read, and its OAuth token, once obtained, for the credentials source
// they were read for.
type portalState struct {
	source      credentialsSource
	credentials *credentials
	token       string
	refreshAt   time.Time
}

type credentials struct {
	username, password, clientID, clientSecret string
}

// NewClient returns a client that reads robot credentials through readers.
// It fails when the CA file exists and holds no certificate.
func NewClient(opts Options, readers ReaderFor, log logr.Logger) (*Client, error) {
	opts = opts.withDefaults()
	roots, err := rootCAs(opts.CAFile, log)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	if opts.DialContext != nil {
		transport.DialContext = opts.DialContext
	}
	return &Client{
		http:    &http.Client{Transport: transport},
		timeout: opts.Timeout,
		readers: readers,
		portals: map[portalKey]*portalState{},
	}, nil
}

// rootCAs returns the system roots and the certificates of the CA file.
func rootCAs(file string, log logr.Logger) (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		log.Info("Cannot load the system's CA certificates: trusting the ingress CA only", "reason", err.Error())
		roots = x509.NewCertPool()
	}
	data, err := os.ReadFile(file)
	switch {
	case errors.Is(err, os.ErrNotExist):
		log.Info("No ingress CA is mounted: training portals are trusted by the system's CA certificates only",
			"file", file)
		return roots, nil
	case err != nil:
		return nil, fmt.Errorf("reading the ingress CA: %w", err)
	}
	if !roots.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("the ingress CA file %s holds no PEM certificate", file)
	}
	log.Info("Trusting the ingress CA for training portals", "file", file)
	return roots, nil
}

// RequestSession requests a new session from the environment's portal for
// the request's user. The portal gives the user's existing session of the
// environment when there is one.
func (c *Client) RequestSession(ctx context.Context, environment *inventory.Environment, request SessionRequest,
) (Session, error) {
	query := url.Values{
		"user":          {request.User},
		"email":         {request.Email},
		"first_name":    {request.FirstName},
		"last_name":     {request.LastName},
		"index_url":     {request.IndexURL},
		"analytics_url": {request.AnalyticsURL},
	}
	parameters := request.Parameters
	if len(parameters) == 0 {
		parameters = json.RawMessage("[]")
	}
	body, err := json.Marshal(struct {
		Parameters json.RawMessage `json:"parameters"`
	}{parameters})
	if err != nil {
		return Session{}, fmt.Errorf("encoding the workshop parameters: %w", err)
	}

	session, err := c.session(ctx, environment, query, body)
	if err != nil {
		return Session{}, fmt.Errorf("requesting a session: %w", err)
	}
	return session, nil
}

// ReacquireSession asks the session's portal to give the session back to its
// user, who returns to the index URL once done with it. The portal gives only
// that session, never a new one, and restarts the wait for its activation
// when the user has not activated it yet.
func (c *Client) ReacquireSession(ctx context.Context, session *inventory.Session, indexURL string,
) (Session, error) {
	query := url.Values{
		"user":      {session.User},
		"index_url": {indexURL},
		"session":   {session.Name},
	}
	reacquired, err := c.session(ctx, session.Environment, query, nil)
	if err != nil {
		return Session{}, fmt.Errorf("reacquiring the session: %w", err)
	}
	if reacquired.Name != session.Name {
		return Session{}, fmt.Errorf("reacquiring the session: the portal's answer names session %q", reacquired.Name)
	}
	return reacquired, nil
}

// session POSTs a session request, with the query and body, or no body when
// body is nil, to the environment's request endpoint, and returns the
// session the portal's answer names.
func (c *Client) session(ctx context.Context, environment *inventory.Environment, query url.Values, body []byte,
) (Session, error) {
	portal := environment.Portal
	if portal.URL == "" {
		return Session{}, errors.New("the portal's status gives no URL yet")
	}
	base, err := baseURL(portal)
	if err != nil {
		return Session{}, err
	}
	endpoint := base + "/workshops/environment/" + url.PathEscape(environment.Name) + "/request/?" + query.Encode()

	// The portal answers with the session's name and the path its user
	// activates it at.
	var answer struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := c.call(ctx, portal, endpoint, body, &answer); err != nil {
		return Session{}, err
	}
	if answer.Name == "" || answer.URL == "" {
		return Session{}, errors.New("the portal's answer names no session")
	}
	return Session{Name: answer.Name, ActivationURL: portal.URL + answer.URL}, nil
}

// baseURL is the URL of the portal's REST API: its in-cluster service on the
// cluster the service runs on, its public URL on another.
func baseURL(portal *inventory.Portal) (string, error) {
	if portal.Cluster.Local {
		if portal.Namespace == "" {
			return "", errors.New("the portal's status gives no namespace yet")
		}
		return "http://training-portal." + portal.Namespace, nil
	}
	return strings.TrimSuffix(portal.URL, "/"), nil
}

// call POSTs body as JSON, or nothing when body is nil, to the portal's
// endpoint with the portal's OAuth token and decodes the answer into out. A
// token the portal refuses is replaced, once, by a new one.
func (c *Client) call(ctx context.Context, portal *inventory.Portal, endpoint string, body []byte, out any) error {
	for attempt := 0; ; attempt++ {
		token, err := c.token(ctx, portal)
		if err != nil {
			return err
		}
		var content io.Reader = http.NoBody
		if body != nil {
			content = bytes.NewReader(body)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, content)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		err = c.do(request, out)
		var refused *statusError
		if attempt == 0 && errors.As(err, &refused) && refused.status == http.StatusUnauthorized {
			c.forgetToken(portal, token)
			continue
		}
		return err
	}
}

// token returns the portal's OAuth token, obtaining a new one when none is
// cached or the cached one is about to expire. The portal's robot
// credentials are read the first time, and again once the portal's uid or
// the namespace or name of its robot credentials Secret changes.
func (c *Client) token(ctx context.Context, portal *inventory.Portal) (string, error) {
	key := portalKey{cluster: portal.Cluster.Name, name: portal.Name}
	source := credentialsSource{uid: portal.UID, namespace: portal.Namespace, secret: portal.RobotCredentialsSecret}

	c.mu.Lock()
	state := c.portals[key]
	if state == nil || state.source != source {
		state = &portalState{source: source}
		c.portals[key] = state
	}
	if state.token != "" && time.Now().Before(state.refreshAt) {
		token := state.token
		c.mu.Unlock()
		return token, nil
	}
	creds := state.credentials
	c.mu.Unlock()

	if creds == nil {
		var err error
		if creds, err = c.readCredentials(ctx, portal); err != nil {
			return "", err
		}
		c.mu.Lock()
		if c.portals[key] == state {
			state.credentials = creds
		}
		c.mu.Unlock()
	}

	token, lifetime, err := c.obtainToken(ctx, portal, creds)
	if err != nil {
		return "", fmt.Errorf("requesting an OAuth token: %w", err)
	}
	c.mu.Lock()
	if c.portals[key] == state && lifetime > tokenRefreshMargin {
		state.token = token
		state.refreshAt = time.Now().Add(lifetime - tokenRefreshMargin)
	}
	c.mu.Unlock()
	return token, nil
}

// forgetToken drops the portal's cached token if it is token.
func (c *Client) forgetToken(portal *inventory.Portal, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state := c.portals[portalKey{cluster: portal.Cluster.Name, name: portal.Name}]; state != nil &&
		state.token == token {
		state.token = ""
	}
}

// readCredentials reads the portal's robot credentials from the Secret its
// status names, as the identity its cluster is reached as.
func (c *Client) readCredentials(ctx context.Context, portal *inventory.Portal) (*credentials, error) {
	if portal.Namespace == "" || portal.RobotCredentialsSecret == "" {
		return nil, errors.New("the portal's status does not name its robot credentials Secret yet")
	}
	reader, ok := c.readers(portal.Cluster.Name)
	if !ok {
		return nil, errors.New("the cluster is not connected")
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	var secret corev1.Secret
	err := reader.Get(ctx, client.ObjectKey{Namespace: portal.Namespace, Name: portal.RobotCredentialsSecret}, &secret)
	switch {
	case err != nil && ctx.Err() != nil:
		return nil, fmt.Errorf("reading the robot credentials Secret %s in namespace %s: %w",
			portal.RobotCredentialsSecret, portal.Namespace, c.unanswered(parent, ctx, "the cluster", err))
	case apierrors.IsNotFound(err):
		return nil, fmt.Errorf("the robot credentials Secret %s in namespace %s does not exist",
			portal.RobotCredentialsSecret, portal.Namespace)
	case err != nil:
		return nil, fmt.Errorf("cannot read the robot credentials Secret %s in namespace %s, "+
			"which the identity the cluster is reached as needs get access on: %w",
			portal.RobotCredentialsSecret, portal.Namespace, err)
	}
	values := map[string]string{}
	for _, key := range []string{"username", "password", "client-id", "client-secret"} {
		value, ok := secret.Data[key]
		if !ok || len(value) == 0 {
			return nil, fmt.Errorf("the robot credentials Secret %s in namespace %s has no %s",
				portal.RobotCredentialsSecret, portal.Namespace, key)
		}
		values[key] = string(value)
	}
	return &credentials{
		username: values["username"], password: values["password"],
		clientID: values["client-id"], clientSecret: values["client-secret"],
	}, nil
}

// obtainToken obtains an OAuth token from the portal with the password
// grant, and returns it with how long it is valid for, 0 when the portal
// does not say.
func (c *Client) obtainToken(ctx context.Context, portal *inventory.Portal, creds *credentials,
) (string, time.Duration, error) {
	base, err := baseURL(portal)
	if err != nil {
		return "", 0, err
	}
	form := url.Values{"grant_type": {"password"}, "username": {creds.username}, "password": {creds.password}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/oauth2/token/",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(creds.clientID, creds.clientSecret)

	var answer struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
	}
	if err := c.do(request, &answer); err != nil {
		return "", 0, err
	}
	if answer.AccessToken == "" {
		return "", 0, errors.New("the portal's answer holds no access token")
	}
	return answer.AccessToken, time.Duration(answer.ExpiresIn * float64(time.Second)), nil
}

// statusError is a portal's answer with a status other than 200.
type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("the portal answered %d %s: %s", e.status, http.StatusText(e.status), e.body)
}

// do sends the request, bounded by the client's timeout or by the request's
// own deadline, whichever comes first, and decodes a 200 answer's JSON body
// into out.
func (c *Client) do(request *http.Request, out any) error {
	parent := request.Context()
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	response, err := c.http.Do(request.WithContext(ctx))
	if err != nil {
		return c.unanswered(parent, ctx, "the portal", err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
	if err != nil {
		return c.unanswered(parent, ctx, "the portal", err)
	}
	if response.StatusCode != http.StatusOK {
		return &statusError{status: response.StatusCode, body: excerpt(data)}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("the portal's answer is not the JSON expected: %w", err)
	}
	return nil
}

// unansweredError is the error of a call that got no answer: the portal, or
// the cluster a portal's robot credentials are read from, could not be
// reached or did not answer in time.
type unansweredError struct{ err error }

func (e *unansweredError) Error() string { return e.err.Error() }

func (e *unansweredError) Unwrap() error { return e.err }

// Unanswered reports whether err is that of a call that got no answer,
// because the portal, or the cluster its robot credentials are read from,
// could not be reached or did not answer in time. Calling it again soon
// after is likely to fare the same.
func Unanswered(err error) bool {
	var unanswered *unansweredError
	return errors.As(err, &unanswered)
}

// unanswered returns the error of a call to who that got no answer, saying
// so when who did not answer in time: within the client's timeout, the
// call's ctx, or before the caller's deadline, parent's.
func (c *Client) unanswered(parent, ctx context.Context, who string, err error) error {
	switch {
	case parent.Err() == nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		err = fmt.Errorf("%s did not answer within %s", who, c.timeout)
	case errors.Is(parent.Err(), context.DeadlineExceeded):
		err = fmt.Errorf("%s did not answer in the time the session request had left", who)
	}
	return &unansweredError{err: err}
}

func excerpt(data []byte) string {
	text := strings.TrimSpace(string(data))
	if len(text) > excerptLength {
		text = text[:excerptLength] + "..."
	}
	return text
}
