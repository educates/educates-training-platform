// Package api serves the Lookup Service's REST API. Its routes, status codes,
// plain-text error bodies and field names are a contract with API clients,
// and stay as the service has always served them.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-logr/logr"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/tokens"
)

// maxRequestBody is the largest request body read, as aiohttp allowed.
const maxRequestBody = 1 << 20

const (
	contentTypeJSON = "application/json; charset=utf-8"
	contentTypeText = "text/plain; charset=utf-8"
)

// Server is the REST API's HTTP handler.
type Server struct {
	config *config.Model
	tokens *tokens.Store
	log    logr.Logger
	mux    *http.ServeMux
}

// NewServer returns the REST API over the configuration model and the token
// store.
func NewServer(model *config.Model, store *tokens.Store, log logr.Logger) *Server {
	s := &Server{config: model, tokens: store, log: log, mux: http.NewServeMux()}

	s.mux.HandleFunc("POST /auth/login", s.login)
	s.mux.HandleFunc("POST /login", s.login)
	s.mux.HandleFunc("POST /auth/logout", s.logout)
	s.mux.Handle("GET /auth/verify", s.loginRequired(s.verify))

	s.mux.Handle("GET /api/v1/clients", s.loginRequired(rolesAccepted(s.listClients, roleAdmin)))
	s.mux.Handle("GET /api/v1/clients/{client}", s.loginRequired(rolesAccepted(s.getClient, roleAdmin, roleTenant)))

	return s
}

// ServeHTTP checks the request's bearer token, if it has one, before
// routing it, so that a malformed or invalid token is refused whatever the
// route, as it always was.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.log.V(1).Info("Request", "method", r.Method, "path", r.URL.Path)

	claims, ok := s.bearerToken(w, r)
	if !ok {
		return
	}
	if claims != nil {
		r = r.WithContext(withClaims(r.Context(), claims))
	}

	handler, pattern := s.mux.Handler(r)
	if pattern == "" {
		unrouted(w, r, handler)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// unrouted answers a request no route serves with the status the mux gives
// it, 404 or 405, and the body aiohttp gave it.
func unrouted(w http.ResponseWriter, r *http.Request, handler http.Handler) {
	recorder := &statusRecorder{header: w.Header()}
	handler.ServeHTTP(recorder, r)
	status := recorder.status
	if status == 0 {
		status = http.StatusNotFound
	}
	writeText(w, status, fmt.Sprintf("%d: %s", status, http.StatusText(status)))
}

// statusRecorder keeps the status and headers a handler writes and drops its
// body.
type statusRecorder struct {
	header http.Header
	status int
}

func (r *statusRecorder) Header() http.Header { return r.header }

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return len(b), nil
}

// writeText writes a plain-text response, its body exactly as given.
func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Type", contentTypeText)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// writeJSON writes value as a 200 JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		s.internalError(w, r, fmt.Errorf("encoding the response: %w", err))
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// internalError logs err and answers 500 with aiohttp's default body.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error(err, "Request failed", "method", r.Method, "path", r.URL.Path)
	writeText(w, http.StatusInternalServerError, "500 Internal Server Error\n\nServer got itself in trouble")
}

// errNotJSONObject is returned for a request body that is not a JSON object.
var errNotJSONObject = errors.New("request body is not a JSON object")

// readJSONObject reads the request body as a JSON object. A body that is
// not valid JSON, or is JSON but not an object, is an error.
func readJSONObject(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, errNotJSONObject
	}
	return object, nil
}

// badRequestBody answers a request whose body readJSONObject refused.
func badRequestBody(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeText(w, http.StatusRequestEntityTooLarge, "413: Request Entity Too Large")
		return
	}
	writeText(w, http.StatusBadRequest, "Request body is not a JSON object")
}
