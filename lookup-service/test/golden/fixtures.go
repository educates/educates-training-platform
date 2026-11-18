// Package golden replays the responses captured from the Python Lookup
// Service, under testdata/golden, against a running service and compares
// what it answers. testdata/golden/README.md describes the fixtures, their
// placeholders and the comparison rules this package implements.
package golden

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Fixture is one captured request and the response to it.
type Fixture struct {
	ID          string       `json:"id"`
	Step        string       `json:"step"`
	Description string       `json:"description"`
	Source      string       `json:"source"`
	Actions     []string     `json:"actions"`
	Request     Request      `json:"request"`
	Response    Response     `json:"response"`
	IssuedToken *IssuedToken `json:"issuedToken"`
	Change      *Change      `json:"change"`
}

// Slug is the fixture's id without its capture order, as auth-login for
// 001-auth-login.
func (f *Fixture) Slug() string {
	_, slug, _ := strings.Cut(f.ID, "-")
	return slug
}

// ServedBy reports whether one of the net/http route patterns, such as
// "GET /api/v1/clients/{client}", serves the fixture's request.
func (f *Fixture) ServedBy(patterns []string) bool {
	mux := http.NewServeMux()
	for _, pattern := range patterns {
		mux.Handle(pattern, http.NotFoundHandler())
	}
	path := placeholderPattern.ReplaceAllString(f.Request.Path, "placeholder")
	request, err := http.NewRequest(f.Request.Method, path, nil)
	if err != nil {
		return false
	}
	_, pattern := mux.Handler(request)
	return pattern != ""
}

// Request is the request a fixture sends.
type Request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body"`
}

// Response is the response a fixture captured.
type Response struct {
	Status      int             `json:"status"`
	ContentType string          `json:"contentType"`
	BodyFormat  string          `json:"bodyFormat"`
	Body        json.RawMessage `json:"body"`
}

// IssuedToken describes the token a login fixture's response carries.
type IssuedToken struct {
	Name            string          `json:"name"`
	Header          json.RawMessage `json:"header"`
	Claims          json.RawMessage `json:"claims"`
	LifetimeSeconds int64           `json:"lifetimeSeconds"`
}

// Change is a response the Go service deliberately gives instead of the
// captured one.
type Change struct {
	Summary  string   `json:"summary"`
	Spec     string   `json:"spec"`
	Expected Expected `json:"expected"`
}

// Expected is the response a Change expects. Members left out keep the
// captured response's.
type Expected struct {
	Status       int                                              `json:"status"`
	ContentType  string                                           `json:"contentType"`
	BodyFormat   string                                           `json:"bodyFormat"`
	Body         json.RawMessage                                  `json:"body"`
	BodyContains *string                                          `json:"bodyContains"`
	SameBodyAs   string                                           `json:"sameBodyAs"`
	Fields       map[string]json.RawMessage                       `json:"fields"`
	Items        map[string]map[string]map[string]json.RawMessage `json:"items"`
	Only         map[string][]string                              `json:"only"`
}

// Fixtures is the set of captured fixtures. It records which of them were
// replayed, so that a suite can check that it asserted all it should.
type Fixtures struct {
	all    []*Fixture
	byName map[string]*Fixture

	mu       sync.Mutex
	replayed map[string]bool
}

// Load reads the fixtures under dir's fixtures/ and derived/ directories.
func Load(dir string) (*Fixtures, error) {
	fixtures := &Fixtures{byName: map[string]*Fixture{}, replayed: map[string]bool{}}
	for _, sub := range []string{"fixtures", "derived"} {
		paths, err := filepath.Glob(filepath.Join(dir, sub, "*.json"))
		if err != nil {
			return nil, err
		}
		slices.Sort(paths)
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var fixture Fixture
			if err := json.Unmarshal(data, &fixture); err != nil {
				return nil, fmt.Errorf("reading the fixture %s: %w", path, err)
			}
			for _, name := range []string{fixture.ID, fixture.Slug()} {
				if _, taken := fixtures.byName[name]; taken {
					return nil, fmt.Errorf("two fixtures are named %s", name)
				}
				fixtures.byName[name] = &fixture
			}
			fixtures.all = append(fixtures.all, &fixture)
		}
	}
	if len(fixtures.all) == 0 {
		return nil, fmt.Errorf("no fixtures under %s", dir)
	}
	return fixtures, nil
}

// Get returns the fixture with that id or slug.
func (f *Fixtures) Get(name string) (*Fixture, error) {
	fixture, ok := f.byName[name]
	if !ok {
		return nil, fmt.Errorf("no fixture is named %s", name)
	}
	return fixture, nil
}

// All returns every fixture, in capture order with the derived ones last.
func (f *Fixtures) All() []*Fixture {
	return slices.Clone(f.all)
}

// NotReplayed returns the ids of the fixtures selected by include that no
// session has replayed.
func (f *Fixtures) NotReplayed(include func(*Fixture) bool) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var missing []string
	for _, fixture := range f.all {
		if include(fixture) && !f.replayed[fixture.ID] {
			missing = append(missing, fixture.ID)
		}
	}
	return missing
}

func (f *Fixtures) markReplayed(fixture *Fixture) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replayed[fixture.ID] = true
}
