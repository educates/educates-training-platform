// Package tokens signs and verifies the access tokens of API clients, and
// keeps the signing key and each client's revocation counter in a Secret the
// service owns, so that tokens survive a restart of the service.
//
// More than one instance of the service may share the Secret for a while,
// as the pods of a rolling update do. Each instance merges what the others
// wrote into what it holds, whenever its own write meets one of theirs and
// every refresh interval besides, so a logout one instance handled is
// honoured by the others within that interval.
package tokens

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
)

const (
	// SecretName is the name of the Secret, in the service's own namespace,
	// that holds the signing key and the revocation counters.
	SecretName = "lookup-service-tokens"

	// SigningKeyKey is the Secret key holding the HS256 signing key.
	SigningKeyKey = "signing-key"

	// ClientsKey is the Secret key holding each client's revocation state.
	ClientsKey = "clients"

	// Lifetime is how long a token stays valid after it is issued.
	Lifetime = 72 * time.Hour

	signingKeyBytes    = 64
	minSigningKeyBytes = 32
	maxWriteAttempts   = 5
)

// The defaults of Options.
const (
	DefaultRefreshInterval = 10 * time.Second
	DefaultWriteTimeout    = 5 * time.Second
)

var (
	// ErrExpired is returned for a token signed with the service's key whose
	// expiry has passed.
	ErrExpired = errors.New("token has expired")

	// ErrInvalid is returned for a token that is malformed, signed with
	// another key or algorithm, or lacks an expiry.
	ErrInvalid = errors.New("token is invalid")
)

// Options configure the store.
type Options struct {
	// RefreshInterval is how often the store reads the Secret again, to
	// learn of the revocations another instance of the service recorded.
	RefreshInterval time.Duration
	// WriteTimeout bounds every recording of a client's state in the Secret,
	// the wait for an earlier one to finish included, and every refresh.
	WriteTimeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.RefreshInterval <= 0 {
		o.RefreshInterval = DefaultRefreshInterval
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = DefaultWriteTimeout
	}
	return o
}

// Token is an access token issued to a client.
type Token struct {
	// Value is the signed JWT.
	Value string
	// ExpiresAt is the Unix time the token expires at.
	ExpiresAt int64
}

// Claims are the claims of a verified token.
type Claims struct {
	// Subject is the name of the client the token was issued to.
	Subject string
	// Identity is the client's identity when the token was issued.
	Identity string
}

// clientState is the revocation state of one client. Counter is part of
// every token's identity, so raising it revokes every token issued before.
// PasswordHMAC is an HMAC of the password the state was last recorded with,
// so that a password change is noticed even across a restart.
type clientState struct {
	UID          string `json:"uid"`
	Counter      int    `json:"counter"`
	PasswordHMAC string `json:"passwordHMAC"`
}

// Store issues and verifies tokens. It is safe for concurrent use.
type Store struct {
	client client.Client
	reader client.Reader
	key    client.ObjectKey
	log    logr.Logger
	opts   Options

	// signingKey never changes once Open has returned, so tokens are
	// verified without a lock.
	signingKey []byte

	// secret is held, by sending to it, by the one caller that reads or
	// writes the Secret, and guards resourceVersion. It is a channel so
	// that waiting for it can be given up.
	secret          chan struct{}
	resourceVersion string

	// mu guards clients. It is never held across a call to the API server,
	// so checking a token never waits for one.
	mu      sync.Mutex
	clients map[string]clientState
}

// Open reads the token Secret in namespace, creating it with a newly
// generated signing key when it does not exist. reader must read from the
// API server, not a cache: the service may only get its own Secret by name.
// The store logs to the logger ctx carries.
func Open(ctx context.Context, c client.Client, reader client.Reader, namespace string, opts Options,
) (*Store, error) {
	log := logr.FromContextOrDiscard(ctx)
	s := &Store{
		client:  c,
		reader:  reader,
		key:     client.ObjectKey{Namespace: namespace, Name: SecretName},
		log:     log,
		opts:    opts.withDefaults(),
		secret:  make(chan struct{}, 1),
		clients: map[string]clientState{},
	}

	var secret corev1.Secret
	err := reader.Get(ctx, s.key, &secret)
	switch {
	case apierrors.IsNotFound(err):
		if err := s.create(ctx); err != nil {
			return nil, err
		}
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("reading the token Secret %s: %w", s.key, err)
	}

	if err := s.load(&secret); err != nil {
		// Without the revocation state, revoked tokens would be valid again,
		// so a new key invalidates every token instead.
		log.Error(err, "The token Secret is unusable; generating a new signing key, which invalidates every issued token",
			"secret", s.key)
		if s.signingKey, err = newSigningKey(); err != nil {
			return nil, err
		}
		if err := s.put(ctx, s.clients); err != nil {
			return nil, fmt.Errorf("writing the token Secret %s: %w", s.key, err)
		}
	}
	return s, nil
}

// create generates a signing key and creates the Secret holding it. When
// another start created the Secret first, its key is used instead.
func (s *Store) create(ctx context.Context) error {
	key, err := newSigningKey()
	if err != nil {
		return err
	}
	s.signingKey = key

	secret, err := s.secretOf(s.clients)
	if err != nil {
		return err
	}
	err = s.client.Create(ctx, secret)
	if apierrors.IsAlreadyExists(err) {
		var existing corev1.Secret
		if err := s.reader.Get(ctx, s.key, &existing); err != nil {
			return fmt.Errorf("reading the token Secret %s: %w", s.key, err)
		}
		return s.load(&existing)
	}
	if err != nil {
		return fmt.Errorf("creating the token Secret %s: %w", s.key, err)
	}
	s.resourceVersion = secret.ResourceVersion
	s.log.Info("Created the token Secret with a new signing key", "secret", s.key)
	return nil
}

// load takes the signing key and revocation state from secret, while Open
// runs. It returns an error, leaving the state empty, when the key is too
// short or the state unreadable.
func (s *Store) load(secret *corev1.Secret) error {
	s.resourceVersion = secret.ResourceVersion
	s.signingKey = secret.Data[SigningKeyKey]
	if len(s.signingKey) < minSigningKeyBytes {
		return fmt.Errorf("the signing key is %d bytes, fewer than %d", len(s.signingKey), minSigningKeyBytes)
	}
	clients, err := decodeClients(secret.Data[ClientsKey])
	if err != nil {
		return err
	}
	s.clients = clients
	return nil
}

func decodeClients(data []byte) (map[string]clientState, error) {
	clients := map[string]clientState{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &clients); err != nil {
			return nil, fmt.Errorf("reading the revocation state: %w", err)
		}
	}
	return clients, nil
}

// Start reads the Secret again every refresh interval until ctx is done, so
// that the store learns of what another instance of the service recorded
// even while it has nothing to record itself.
func (s *Store) Start(ctx context.Context) error {
	ticker := time.NewTicker(s.opts.RefreshInterval)
	defer ticker.Stop()
	problem := ""
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		err := s.refresh(ctx)
		switch {
		case err == nil:
			problem = ""
		case ctx.Err() == nil && err.Error() != problem:
			problem = err.Error()
			s.log.Error(err, "Cannot read the token Secret again: a logout another instance of the service "+
				"handled is not known here until it can be read", "secret", s.key)
		}
	}
}

// refresh merges the Secret's revocation state into the store's.
func (s *Store) refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.opts.WriteTimeout)
	defer cancel()
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	return s.reload(ctx)
}

// acquire waits for the Secret to be the caller's to read and write, or for
// ctx to be done.
func (s *Store) acquire(ctx context.Context) error {
	select {
	case s.secret <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting to write the token Secret %s: %w", s.key, ctx.Err())
	}
}

func (s *Store) release() { <-s.secret }

// Issue signs a token for c. Its identity is the client's identity now, which
// is recorded first if it is not yet.
func (s *Store) Issue(ctx context.Context, c config.Client) (Token, error) {
	state, err := s.record(ctx, c, false)
	if err != nil {
		return Token{}, err
	}

	expiresAt := time.Now().Add(Lifetime).Unix()
	claims := jwt.RegisteredClaims{
		Subject:   c.Name,
		ID:        identity(state),
		ExpiresAt: jwt.NewNumericDate(time.Unix(expiresAt, 0)),
	}
	value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.signingKey)
	if err != nil {
		return Token{}, fmt.Errorf("signing a token: %w", err)
	}
	return Token{Value: value, ExpiresAt: expiresAt}, nil
}

// Verify checks that token is an HS256 token signed with the service's key
// and not expired, and returns its claims. It returns ErrExpired or
// ErrInvalid when it is not.
func (s *Store) Verify(token string) (Claims, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return s.signingKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired())
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return Claims{}, ErrExpired
	case err != nil:
		return Claims{}, ErrInvalid
	}
	return Claims{Subject: claims.Subject, Identity: claims.ID}, nil
}

// Authenticates reports whether claims were issued to c and are not revoked:
// the client has not logged out, changed its password or been created again
// since.
func (s *Store) Authenticates(c config.Client, claims Claims) bool {
	s.mu.Lock()
	expected := identity(s.current(c))
	s.mu.Unlock()

	return claims.Subject == c.Name &&
		hmac.Equal([]byte(claims.Identity), []byte(expected))
}

// Revoke revokes every token issued to c.
func (s *Store) Revoke(ctx context.Context, c config.Client) error {
	_, err := s.record(ctx, c, true)
	return err
}

// ObserveClient records the state of a created or updated client: a password
// change revokes the client's tokens, and any other update leaves them as
// they are. The state of a deleted client is kept: a client created again
// under its name has a new uid, which resets it.
func (s *Store) ObserveClient(ctx context.Context, c config.Client) error {
	_, err := s.record(ctx, c, false)
	return err
}

// current returns the revocation state c has now, which differs from the one
// recorded when the client was created again or its password changed since.
// The caller holds s.mu.
func (s *Store) current(c config.Client) clientState {
	mac := s.passwordHMAC(c.Password)
	recorded, ok := s.clients[c.Name]
	switch {
	case !ok || recorded.UID != c.UID:
		return clientState{UID: c.UID, Counter: 1, PasswordHMAC: mac}
	case !hmac.Equal([]byte(recorded.PasswordHMAC), []byte(mac)):
		return clientState{UID: c.UID, Counter: recorded.Counter + 1, PasswordHMAC: mac}
	default:
		return recorded
	}
}

// record stores the state c has now as the client's, with its counter
// raised when revoke is set, and returns it. It writes the Secret when the
// state changes, within the write timeout. When the write meets one of
// another instance of the service, that instance's state is merged in first,
// so nothing either recorded is lost: a counter raised there already revokes
// the tokens a revocation here is meant to.
func (s *Store) record(ctx context.Context, c config.Client, revoke bool) (clientState, error) {
	s.mu.Lock()
	state := s.current(c)
	recorded, ok := s.clients[c.Name]
	s.mu.Unlock()
	atLeast := state.Counter
	if revoke {
		atLeast++
	} else if ok && recorded == state {
		return state, nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.opts.WriteTimeout)
	defer cancel()
	if err := s.acquire(ctx); err != nil {
		return clientState{}, err
	}
	defer s.release()

	for attempt := 0; ; attempt++ {
		s.mu.Lock()
		state := s.current(c)
		state.Counter = max(state.Counter, atLeast)
		if recorded, ok := s.clients[c.Name]; ok && recorded == state {
			s.mu.Unlock()
			return state, nil
		}
		next := maps.Clone(s.clients)
		s.mu.Unlock()
		next[c.Name] = state

		err := s.put(ctx, next)
		if err == nil {
			// Only the holder of the Secret changes the clients, so next is
			// still the clients with this client's state changed.
			s.mu.Lock()
			s.clients = next
			s.mu.Unlock()
			return state, nil
		}
		retryable := apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err)
		if !retryable || attempt >= maxWriteAttempts-1 {
			return clientState{}, fmt.Errorf("writing the token Secret %s: %w", s.key, err)
		}
		if err := s.reload(ctx); err != nil {
			return clientState{}, err
		}
	}
}

// put stores clients in the Secret, creating it again, with the same key,
// when it was deleted. The caller holds the Secret, except while Open runs.
func (s *Store) put(ctx context.Context, clients map[string]clientState) error {
	secret, err := s.secretOf(clients)
	if err != nil {
		return err
	}
	secret.ResourceVersion = s.resourceVersion
	err = s.client.Update(ctx, secret)
	if apierrors.IsNotFound(err) {
		secret.ResourceVersion = ""
		err = s.client.Create(ctx, secret)
	}
	if err != nil {
		return err
	}
	s.resourceVersion = secret.ResourceVersion
	return nil
}

// reload reads the Secret and merges its revocation state into the store's,
// so that the next write is made on top of what the Secret holds. A Secret
// that is gone, or whose state belongs to another signing key or cannot be
// read, changes nothing: the next write replaces it. The caller holds the
// Secret.
func (s *Store) reload(ctx context.Context) error {
	var latest corev1.Secret
	err := s.reader.Get(ctx, s.key, &latest)
	if apierrors.IsNotFound(err) {
		s.resourceVersion = ""
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the token Secret %s: %w", s.key, err)
	}
	s.resourceVersion = latest.ResourceVersion
	if !bytes.Equal(latest.Data[SigningKeyKey], s.signingKey) {
		return nil
	}
	theirs, err := decodeClients(latest.Data[ClientsKey])
	if err != nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients = merge(s.clients, theirs)
	return nil
}

// merge returns the revocation state of every client in ours or theirs,
// where theirs is what the Secret holds. For a client in both, the Secret's
// state holds unless ours is the same client, by its uid, with a higher
// counter: a counter never goes down.
func merge(ours, theirs map[string]clientState) map[string]clientState {
	merged := maps.Clone(ours)
	for name, their := range theirs {
		if our, ok := ours[name]; ok && our.UID == their.UID && our.Counter > their.Counter {
			continue
		}
		merged[name] = their
	}
	return merged
}

func (s *Store) secretOf(clients map[string]clientState) (*corev1.Secret, error) {
	data, err := json.Marshal(clients)
	if err != nil {
		return nil, fmt.Errorf("encoding the revocation state: %w", err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.key.Name,
			Namespace: s.key.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "lookup-service",
				"app.kubernetes.io/component":  "lookup-service",
				"app.kubernetes.io/part-of":    "educates",
				"app.kubernetes.io/managed-by": "lookup-service",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SigningKeyKey: s.signingKey,
			ClientsKey:    data,
		},
	}, nil
}

// passwordHMAC is an HMAC of a password under the signing key. It tells
// whether a password changed without storing the password.
func (s *Store) passwordHMAC(password string) string {
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(password))
	return hex.EncodeToString(mac.Sum(nil))
}

// identity is a client's identity as tokens carry it in their jti claim.
func identity(state clientState) string {
	return fmt.Sprintf("client@educates:%s#%d", state.UID, state.Counter)
}

func newSigningKey() ([]byte, error) {
	key := make([]byte, signingKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating a signing key: %w", err)
	}
	return key, nil
}
