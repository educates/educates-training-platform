// Package tokens signs and verifies the access tokens of API clients, and
// keeps the signing key and each client's revocation counter in a Secret the
// service owns, so that tokens survive a restart of the service.
package tokens

import (
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

var (
	// ErrExpired is returned for a token signed with the service's key whose
	// expiry has passed.
	ErrExpired = errors.New("token has expired")

	// ErrInvalid is returned for a token that is malformed, signed with
	// another key or algorithm, or lacks an expiry.
	ErrInvalid = errors.New("token is invalid")
)

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
// Password is an HMAC of the password the state was last recorded with, so
// that a password change is noticed even across a restart.
type clientState struct {
	UID      string `json:"uid"`
	Counter  int    `json:"counter"`
	Password string `json:"password"`
}

// Store issues and verifies tokens. It is safe for concurrent use.
type Store struct {
	client client.Client
	reader client.Reader
	key    client.ObjectKey
	log    logr.Logger

	mu              sync.Mutex
	signingKey      []byte
	clients         map[string]clientState
	resourceVersion string
}

// Open reads the token Secret in namespace, creating it with a newly
// generated signing key when it does not exist. reader must read from the
// API server, not a cache: the service may only get its own Secret by name.
// The store logs to the logger ctx carries.
func Open(ctx context.Context, c client.Client, reader client.Reader, namespace string) (*Store, error) {
	log := logr.FromContextOrDiscard(ctx)
	s := &Store{
		client: c,
		reader: reader,
		key:    client.ObjectKey{Namespace: namespace, Name: SecretName},
		log:    log,
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
		if err := s.write(ctx, map[string]clientState{}); err != nil {
			return nil, err
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
	s.clients = map[string]clientState{}

	secret, err := s.secret(s.clients)
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

// load takes the signing key and revocation state from secret. It returns an
// error, leaving the state empty, when the key is too short or the state
// unreadable.
func (s *Store) load(secret *corev1.Secret) error {
	s.resourceVersion = secret.ResourceVersion
	s.signingKey = secret.Data[SigningKeyKey]
	s.clients = map[string]clientState{}
	if len(s.signingKey) < minSigningKeyBytes {
		return fmt.Errorf("the signing key is %d bytes, fewer than %d", len(s.signingKey), minSigningKeyBytes)
	}
	if data := secret.Data[ClientsKey]; len(data) > 0 {
		if err := json.Unmarshal(data, &s.clients); err != nil {
			s.clients = map[string]clientState{}
			return fmt.Errorf("reading the revocation state: %w", err)
		}
	}
	return nil
}

// Issue signs a token for c. Its identity is the client's identity now, which
// is recorded first if it is not yet.
func (s *Store) Issue(ctx context.Context, c config.Client) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.current(c)
	if err := s.record(ctx, c.Name, state); err != nil {
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
	s.mu.Lock()
	key := s.signingKey
	s.mu.Unlock()

	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return key, nil },
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
	defer s.mu.Unlock()

	expected := identity(s.current(c))
	return claims.Subject == c.Name &&
		hmac.Equal([]byte(claims.Identity), []byte(expected))
}

// Revoke revokes every token issued to c.
func (s *Store) Revoke(ctx context.Context, c config.Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.current(c)
	state.Counter++
	return s.record(ctx, c.Name, state)
}

// ObserveClient records the state of a created or updated client: a password
// change revokes the client's tokens, and any other update leaves them as
// they are. The state of a deleted client is kept: a client created again
// under its name has a new uid, which resets it.
func (s *Store) ObserveClient(ctx context.Context, c config.Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.record(ctx, c.Name, s.current(c))
}

// current returns the revocation state c has now, which differs from the one
// recorded when the client was created again or its password changed since.
// The caller holds s.mu.
func (s *Store) current(c config.Client) clientState {
	password := s.fingerprint(c.Password)
	recorded, ok := s.clients[c.Name]
	switch {
	case !ok || recorded.UID != c.UID:
		return clientState{UID: c.UID, Counter: 1, Password: password}
	case !hmac.Equal([]byte(recorded.Password), []byte(password)):
		return clientState{UID: c.UID, Counter: recorded.Counter + 1, Password: password}
	default:
		return recorded
	}
}

// record stores state as the client's, writing the Secret when it changes.
// The caller holds s.mu.
func (s *Store) record(ctx context.Context, name string, state clientState) error {
	if recorded, ok := s.clients[name]; ok && recorded == state {
		return nil
	}
	next := maps.Clone(s.clients)
	next[name] = state
	return s.write(ctx, next)
}

// write stores clients in the Secret and, once it is stored, in memory. The
// Secret is the service's own, so a conflicting change made by anyone else is
// overwritten, and a deleted Secret is created again with the same key.
// The caller holds s.mu, except while Open runs.
func (s *Store) write(ctx context.Context, clients map[string]clientState) error {
	for attempt := 0; ; attempt++ {
		secret, err := s.secret(clients)
		if err != nil {
			return err
		}
		secret.ResourceVersion = s.resourceVersion
		err = s.client.Update(ctx, secret)
		if apierrors.IsNotFound(err) {
			secret.ResourceVersion = ""
			err = s.client.Create(ctx, secret)
		}
		if err == nil {
			s.clients = clients
			s.resourceVersion = secret.ResourceVersion
			return nil
		}
		retryable := apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err)
		if !retryable || attempt >= maxWriteAttempts-1 {
			return fmt.Errorf("writing the token Secret %s: %w", s.key, err)
		}
		var latest corev1.Secret
		if err := s.reader.Get(ctx, s.key, &latest); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("reading the token Secret %s: %w", s.key, err)
		}
		s.resourceVersion = latest.ResourceVersion
	}
}

func (s *Store) secret(clients map[string]clientState) (*corev1.Secret, error) {
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

// fingerprint is an HMAC of a password under the signing key. It tells
// whether a password changed without storing the password.
func (s *Store) fingerprint(password string) string {
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
