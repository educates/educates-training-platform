package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/tokens"
)

const (
	roleAdmin  = "admin"
	roleTenant = "tenant"
)

type claimsKey struct{}

func withClaims(ctx context.Context, claims *tokens.Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// claimsFrom returns the claims of the request's verified bearer token, or
// nil when the request carried none.
func claimsFrom(ctx context.Context) *tokens.Claims {
	claims, _ := ctx.Value(claimsKey{}).(*tokens.Claims)
	return claims
}

// caller is the authenticated client of a request.
type caller struct {
	client config.Client
	// roles are the roles of the client that the route accepts.
	roles []string
}

// actsAs reports whether the route accepted the client in role.
func (c caller) actsAs(role string) bool {
	return slices.Contains(c.roles, role)
}

// callerHandler handles a request of an authenticated client.
type callerHandler func(w http.ResponseWriter, r *http.Request, c caller)

// bearerToken verifies the request's bearer token, if it has one. It answers
// the request itself, and returns false, when the Authorization header is
// malformed or the token invalid.
func (s *Server) bearerToken(w http.ResponseWriter, r *http.Request) (*tokens.Claims, bool) {
	authorization := r.Header.Get("Authorization")
	if authorization == "" {
		return nil, true
	}

	parts := strings.Fields(authorization)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		writeText(w, http.StatusBadRequest, "Invalid Authorization header")
		return nil, false
	}

	claims, err := s.tokens.Verify(parts[1])
	switch {
	case errors.Is(err, tokens.ErrExpired):
		writeText(w, http.StatusUnauthorized, "JWT token has expired")
		return nil, false
	case err != nil:
		writeText(w, http.StatusUnauthorized, "JWT token is invalid")
		return nil, false
	}
	return &claims, true
}

// authenticatedClient returns the client the request's token was issued to.
// It answers the request itself, and returns false, when there is no token,
// the client no longer exists, or the token is revoked.
func (s *Server) authenticatedClient(w http.ResponseWriter, r *http.Request) (config.Client, bool) {
	claims := claimsFrom(r.Context())
	if claims == nil {
		writeText(w, http.StatusBadRequest, "JWT token not supplied")
		return config.Client{}, false
	}

	client, found, err := s.config.Client(r.Context(), claims.Subject)
	if err != nil {
		s.internalError(w, r, err)
		return config.Client{}, false
	}
	if !found {
		writeText(w, http.StatusUnauthorized, "Client details not found")
		return config.Client{}, false
	}
	if !s.tokens.Authenticates(client, *claims) {
		writeText(w, http.StatusUnauthorized, "Client identity does not match")
		return config.Client{}, false
	}
	return client, true
}

// loginRequired passes the request on to next only for an authenticated
// client.
func (s *Server) loginRequired(next callerHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, ok := s.authenticatedClient(w, r)
		if !ok {
			return
		}
		next(w, r, caller{client: client})
	})
}

// rolesAccepted passes the request on to next only for a client holding one
// of roles, recording which of them it holds.
func rolesAccepted(next callerHandler, roles ...string) callerHandler {
	return func(w http.ResponseWriter, r *http.Request, c caller) {
		for _, role := range roles {
			if c.client.HasRole(role) {
				c.roles = append(c.roles, role)
			}
		}
		if len(c.roles) == 0 {
			writeText(w, http.StatusForbidden, "Client access not permitted")
			return
		}
		next(w, r, c)
	}
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresAt   int64  `json:"expires_at"`
}

// login answers POST /auth/login and its alias POST /login: a client sends
// its name and password and receives a token.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONObject(w, r)
	if err != nil {
		badRequestBody(w, err)
		return
	}

	username, ok := body["username"]
	if !ok || username == nil {
		writeText(w, http.StatusBadRequest, "No username provided")
		return
	}
	password, ok := body["password"]
	if !ok || password == nil {
		writeText(w, http.StatusBadRequest, "No password provided")
		return
	}

	client, ok := s.clientWithPassword(w, r, username, password)
	if !ok {
		return
	}

	token, err := s.tokens.Issue(r.Context(), client)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.V(1).Info("Client logged in", "client", client.Name)

	s.writeJSON(w, r, loginResponse{
		AccessToken: token.Value,
		TokenType:   "Bearer",
		ExpiresAt:   token.ExpiresAt,
	})
}

// clientWithPassword returns the client named username when password is
// its password. It answers the request itself, and returns false, otherwise.
func (s *Server) clientWithPassword(w http.ResponseWriter, r *http.Request, username, password any) (config.Client, bool) {
	name, nameIsString := username.(string)
	secret, secretIsString := password.(string)

	var client config.Client
	found := false
	if nameIsString {
		var err error
		client, found, err = s.config.Client(r.Context(), name)
		if err != nil {
			s.internalError(w, r, err)
			return config.Client{}, false
		}
	}
	// The password is compared even when there is no such client, so that
	// the time taken does not tell which client names exist.
	matches := client.PasswordMatches(secret)
	if !found || !secretIsString || !matches {
		writeText(w, http.StatusUnauthorized, "Invalid username/password")
		return config.Client{}, false
	}
	return client, true
}

// logout answers POST /auth/logout, revoking every token of the client.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	client, ok := s.authenticatedClient(w, r)
	if !ok {
		return
	}
	if err := s.tokens.Revoke(r.Context(), client); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.V(1).Info("Client logged out", "client", client.Name)

	s.writeJSON(w, r, struct{}{})
}

// verify answers GET /auth/verify: reaching it means the token is valid.
func (s *Server) verify(w http.ResponseWriter, r *http.Request, _ caller) {
	s.writeJSON(w, r, struct{}{})
}
