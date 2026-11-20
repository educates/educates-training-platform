package golden

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// Session replays fixtures against one service, keeping the placeholder
// bindings the fixtures share: the uids of the objects the test created and
// the tokens the service issued.
type Session struct {
	fixtures *Fixtures
	bindings *bindings

	// BaseURL is the URL of the service's REST API.
	BaseURL string
	// Client sends the requests.
	Client *http.Client
}

// NewSession starts a session with no placeholder bound.
func (f *Fixtures) NewSession(baseURL string) *Session {
	return &Session{
		fixtures: f,
		bindings: newBindings(),
		BaseURL:  baseURL,
		Client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Bind binds a placeholder bound by the test, such as
// <uid:cluster-a/ClientConfig/tenant-client>, to its value.
func (s *Session) Bind(placeholder, value string) {
	s.bindings.values[placeholder] = value
}

// Value returns the value a placeholder is bound to, and whether it is.
func (s *Session) Value(placeholder string) (string, bool) {
	value, ok := s.bindings.values[placeholder]
	return value, ok
}

// Expand replaces every placeholder in text by the value it is bound to. A
// placeholder that is not bound is an error.
func (s *Session) Expand(text string) (string, error) {
	return s.bindings.expand(text)
}

// Option adjusts what a replay expects.
type Option func(*replay)

// WithIssuedClaim expects the token a login fixture issues to carry value,
// in placeholder form, as its claim name, in place of the captured claim. It
// is for a claim that a deliberate change elsewhere in the scenario changes.
func WithIssuedClaim(name string, value any) Option {
	return func(r *replay) { r.claims[name] = value }
}

type replay struct {
	claims map[string]any
}

// Replay sends the request of the fixture with that id or slug and compares
// the response with the one the fixture expects: the captured response, or
// the response its change describes. It returns an error describing the
// first difference.
func (s *Session) Replay(name string, opts ...Option) error {
	fixture, err := s.fixtures.Get(name)
	if err != nil {
		return err
	}
	s.fixtures.markReplayed(fixture)

	r := &replay{claims: map[string]any{}}
	for _, opt := range opts {
		opt(r)
	}

	if err := s.replay(fixture, r); err != nil {
		return fmt.Errorf("fixture %s (%s): %w", fixture.ID, fixture.Description, err)
	}
	return nil
}

func (s *Session) replay(fixture *Fixture, r *replay) error {
	request, err := s.request(fixture)
	if err != nil {
		return err
	}

	sentAt := time.Now()
	response, err := s.Client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	receivedAt := time.Now()

	expected, err := s.expected(fixture)
	if err != nil {
		return err
	}

	b := s.bindings.clone()
	b.perFixture = map[string]string{}
	if response.StatusCode != expected.status {
		return fmt.Errorf("status is %d, want %d; body %q", response.StatusCode, expected.status, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != expected.contentType {
		return fmt.Errorf("content type is %q, want %q", contentType, expected.contentType)
	}
	if err := expected.compareBody(b, body); err != nil {
		return fmt.Errorf("%w; body %s", err, body)
	}
	if fixture.IssuedToken != nil {
		if err := checkIssuedToken(b, fixture.IssuedToken, r.claims, sentAt, receivedAt); err != nil {
			return err
		}
	}

	s.bindings.values = b.values
	return nil
}

func (s *Session) request(fixture *Fixture) (*http.Request, error) {
	path, err := s.bindings.expand(fixture.Request.Path)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if fixture.Request.Body != nil {
		text, err := s.bindings.expand(*fixture.Request.Body)
		if err != nil {
			return nil, err
		}
		body = strings.NewReader(text)
	}
	request, err := http.NewRequest(fixture.Request.Method, strings.TrimSuffix(s.BaseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	for name, value := range fixture.Request.Headers {
		expanded, err := s.bindings.expand(value)
		if err != nil {
			return nil, err
		}
		request.Header.Set(name, expanded)
	}
	return request, nil
}

// expectedResponse is the response a replay must get.
type expectedResponse struct {
	status       int
	contentType  string
	format       string
	body         any
	hasBody      bool
	bodyContains *string
}

func (s *Session) expected(fixture *Fixture) (*expectedResponse, error) {
	captured := fixture.Response
	expected := &expectedResponse{
		status:      captured.Status,
		contentType: captured.ContentType,
		format:      captured.BodyFormat,
	}
	if fixture.Change == nil {
		body, err := decodeBody(captured)
		if err != nil {
			return nil, err
		}
		expected.body, expected.hasBody = body, true
		return expected, nil
	}

	change := fixture.Change.Expected
	expected.status = change.Status
	if change.ContentType != "" {
		expected.contentType = change.ContentType
	}
	if change.BodyFormat != "" {
		expected.format = change.BodyFormat
	}
	expected.bodyContains = change.BodyContains

	var err error
	switch {
	case len(change.Body) > 0:
		expected.body, err = decodeBody(Response{BodyFormat: expected.format, Body: change.Body})
		expected.hasBody = true
	case change.SameBodyAs != "":
		other, otherErr := s.fixtures.Get(change.SameBodyAs)
		if otherErr != nil {
			return nil, otherErr
		}
		expected.body, err = decodeBody(other.Response)
		expected.hasBody = true
	case change.Fields != nil:
		expected.body, err = replaceFields(captured, change.Fields)
		expected.hasBody = true
	case change.Items != nil:
		expected.body, err = replaceItems(captured, change.Items)
		expected.hasBody = true
	case change.Only != nil:
		expected.body, err = keepOnly(captured, change.Only)
		expected.hasBody = true
	}
	return expected, err
}

func (e *expectedResponse) compareBody(b *bindings, body []byte) error {
	var actual any
	switch e.format {
	case "text":
		actual = string(body)
	case "json":
		value, err := decodeJSON(body)
		if err != nil {
			return fmt.Errorf("the body is not JSON: %w", err)
		}
		actual = value
	case "yaml":
		data, err := yaml.YAMLToJSON(body)
		if err != nil {
			return fmt.Errorf("the body is not YAML: %w", err)
		}
		if actual, err = decodeJSON(data); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown body format %q", e.format)
	}

	if e.bodyContains != nil {
		text, ok := actual.(string)
		if !ok || !strings.Contains(text, *e.bodyContains) {
			return fmt.Errorf("the body does not contain %q", *e.bodyContains)
		}
	}
	if !e.hasBody {
		return nil
	}
	return b.match("body", e.body, actual)
}

// decodeBody returns a response body in the form match compares: a JSON
// value for json and yaml, a string for text.
func decodeBody(response Response) (any, error) {
	switch response.BodyFormat {
	case "json":
		return decodeJSON(response.Body)
	case "text":
		var text string
		err := json.Unmarshal(response.Body, &text)
		return text, err
	case "yaml":
		var text string
		if err := json.Unmarshal(response.Body, &text); err != nil {
			return nil, err
		}
		data, err := yaml.YAMLToJSON([]byte(text))
		if err != nil {
			return nil, err
		}
		return decodeJSON(data)
	}
	return nil, fmt.Errorf("unknown body format %q", response.BodyFormat)
}

func replaceFields(captured Response, fields map[string]json.RawMessage) (any, error) {
	body, err := decodeBody(captured)
	if err != nil {
		return nil, err
	}
	object, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New("a fields change needs an object body")
	}
	return object, setMembers(object, fields)
}

func replaceItems(captured Response, items map[string]map[string]map[string]json.RawMessage) (any, error) {
	body, err := decodeBody(captured)
	if err != nil {
		return nil, err
	}
	for list, elements := range items {
		for name, fields := range elements {
			element, err := findElement(body, list, name)
			if err != nil {
				return nil, err
			}
			if err := setMembers(element, fields); err != nil {
				return nil, err
			}
		}
	}
	return body, nil
}

func keepOnly(captured Response, only map[string][]string) (any, error) {
	body, err := decodeBody(captured)
	if err != nil {
		return nil, err
	}
	object, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New("an only change needs an object body")
	}
	for list, names := range only {
		elements, ok := object[list].([]any)
		if !ok {
			return nil, fmt.Errorf("the body has no list %s", list)
		}
		kept := []any{}
		for _, element := range elements {
			if object, ok := element.(map[string]any); ok && slices.Contains(names, elementName(object)) {
				kept = append(kept, element)
			}
		}
		object[list] = kept
	}
	return object, nil
}

// elementName names a list element as changes do: cluster/name when it has
// a cluster, its name otherwise.
func elementName(element map[string]any) string {
	name, _ := element["name"].(string)
	if cluster, ok := element["cluster"].(string); ok {
		return cluster + "/" + name
	}
	return name
}

func findElement(body any, list, name string) (map[string]any, error) {
	object, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New("an items change needs an object body")
	}
	elements, ok := object[list].([]any)
	if !ok {
		return nil, fmt.Errorf("the body has no list %s", list)
	}
	for _, element := range elements {
		if object, ok := element.(map[string]any); ok && elementName(object) == name {
			return object, nil
		}
	}
	return nil, fmt.Errorf("the list %s has no element %s", list, name)
}

func setMembers(object map[string]any, fields map[string]json.RawMessage) error {
	for name, raw := range fields {
		value, err := decodeJSON(raw)
		if err != nil {
			return err
		}
		object[name] = value
	}
	return nil
}

// checkIssuedToken checks the token a login issued, which the response body
// bound to the token's placeholder: its header and claims, with overrides,
// and that it expires the fixture's lifetime after it was issued.
func checkIssuedToken(b *bindings, issued *IssuedToken, overrides map[string]any,
	sentAt, receivedAt time.Time) error {
	token, ok := b.values[issued.Name]
	if !ok {
		return fmt.Errorf("the response did not bind %s", issued.Name)
	}
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return fmt.Errorf("the token %s is not a JWT", issued.Name)
	}

	header, err := decodeSegment(segments[0])
	if err != nil {
		return err
	}
	expectedHeader, err := decodeJSON(issued.Header)
	if err != nil {
		return err
	}
	if err := b.match("token.header", expectedHeader, header); err != nil {
		return err
	}

	claims, err := decodeSegment(segments[1])
	if err != nil {
		return err
	}
	expectedClaims, err := decodeJSON(issued.Claims)
	if err != nil {
		return err
	}
	claimMembers, ok := expectedClaims.(map[string]any)
	if !ok {
		return errors.New("the fixture's token claims are not an object")
	}
	maps.Copy(claimMembers, overrides)
	if err := b.match("token.claims", expectedClaims, claims); err != nil {
		return err
	}

	exp, err := strconv.ParseInt(fmt.Sprint(claims.(map[string]any)["exp"]), 10, 64)
	if err != nil {
		return fmt.Errorf("the token's exp is not an integer: %w", err)
	}
	earliest := sentAt.Unix() + issued.LifetimeSeconds
	latest := receivedAt.Unix() + issued.LifetimeSeconds
	if exp < earliest || exp > latest {
		return fmt.Errorf("the token expires at %d, want %d seconds after it was issued, between %d and %d",
			exp, issued.LifetimeSeconds, earliest, latest)
	}
	return nil
}

func decodeSegment(segment string) (any, error) {
	data, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return nil, fmt.Errorf("decoding a token segment: %w", err)
	}
	return decodeJSON(data)
}
