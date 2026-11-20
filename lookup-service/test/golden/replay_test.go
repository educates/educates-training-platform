package golden

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeService answers each request path with a canned response, and records
// the Authorization header of the last request.
type fakeService struct {
	responses     map[string]cannedResponse
	authorization string
}

type cannedResponse struct {
	status      int
	contentType string
	body        string
}

func (f *fakeService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.authorization = r.Header.Get("Authorization")
	response, ok := f.responses[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", response.contentType)
	w.WriteHeader(response.status)
	_, _ = w.Write([]byte(response.body))
}

const (
	jsonType = "application/json; charset=utf-8"
	textType = "text/plain; charset=utf-8"
)

// writeFixtures writes fixtures, given as JSON, into a fixtures directory
// and loads them.
func writeFixtures(t *testing.T, fixtures ...string) *Fixtures {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		var header struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(fixture), &header); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fixtures", header.ID+".json"), []byte(fixture), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

const listFixture = `{
  "id": "001-list", "step": "initial", "description": "A list.",
  "request": {"method": "GET", "path": "/list", "headers": {}, "body": null},
  "response": {"status": 200, "contentType": "application/json; charset=utf-8", "bodyFormat": "json",
    "body": {"items": [
      {"name": "a", "uid": "<uid:cluster-a/Thing/a>", "session": "<session:first>", "count": 1},
      {"name": "b", "uid": "<uid:cluster-a/Thing/b>", "session": "<session:second>", "count": 2}
    ]}}
}`

const useFixture = `{
  "id": "002-use", "step": "initial", "description": "A request carrying a bound session.",
  "request": {"method": "GET", "path": "/use", "headers": {"Authorization": "Bearer <session:second>"}, "body": null},
  "response": {"status": 404, "contentType": "text/plain; charset=utf-8", "bodyFormat": "text", "body": "Not here"}
}`

func TestReplayComparesListsAsMultisetsAndBindsOnSight(t *testing.T) {
	fixtures := writeFixtures(t, listFixture, useFixture)
	service := &fakeService{responses: map[string]cannedResponse{
		"/list": {200, jsonType, `{"items": [
			{"name": "b", "uid": "uid-b", "session": "s-2", "count": 2},
			{"name": "a", "uid": "uid-a", "session": "s-1", "count": 1.0}]}`},
		"/use": {404, textType, "Not here"},
	}}
	server := httptest.NewServer(service)
	defer server.Close()

	session := fixtures.NewSession(server.URL)
	session.Bind("<uid:cluster-a/Thing/a>", "uid-a")
	session.Bind("<uid:cluster-a/Thing/b>", "uid-b")
	if err := session.Replay("list"); err != nil {
		t.Fatal(err)
	}
	if err := session.Replay("use"); err != nil {
		t.Fatal(err)
	}
	if service.authorization != "Bearer s-2" {
		t.Fatalf("the request carried %q, want the session bound on sight", service.authorization)
	}
	if missing := fixtures.NotReplayed(func(*Fixture) bool { return true }); len(missing) != 0 {
		t.Fatalf("fixtures %v are not recorded as replayed", missing)
	}
}

// listBody is a response to listFixture's request: element a with its uid
// and count, and element b with its session.
func listBody(uidA string, countA int, sessionB string) string {
	return fmt.Sprintf(`{"items": [
		{"name": "a", "uid": %q, "session": "s-1", "count": %d},
		{"name": "b", "uid": "uid-b", "session": %q, "count": 2}]}`, uidA, countA, sessionB)
}

func TestReplayReportsDifferences(t *testing.T) {
	for _, test := range []struct {
		name     string
		response cannedResponse
		bind     bool
		want     string
	}{
		{
			name:     "a member's value",
			response: cannedResponse{200, jsonType, listBody("uid-a", 3, "s-2")},
			bind:     true,
			want:     "no element matches",
		},
		{
			name:     "a uid the test bound to another value",
			response: cannedResponse{200, jsonType, listBody("uid-x", 1, "s-2")},
			bind:     true,
			want:     "no element matches",
		},
		{
			name:     "two placeholders standing for one session",
			response: cannedResponse{200, jsonType, listBody("uid-a", 1, "s-1")},
			bind:     true,
			want:     "no element matches",
		},
		{
			name:     "a placeholder the test did not bind",
			response: cannedResponse{200, jsonType, listBody("uid-a", 1, "s-2")},
			want:     "not bound by the test",
		},
		{
			name:     "a content type",
			response: cannedResponse{200, "application/json", `{"items": []}`},
			bind:     true,
			want:     "content type",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixtures := writeFixtures(t, listFixture)
			server := httptest.NewServer(&fakeService{responses: map[string]cannedResponse{"/list": test.response}})
			defer server.Close()

			session := fixtures.NewSession(server.URL)
			if test.bind {
				session.Bind("<uid:cluster-a/Thing/a>", "uid-a")
				session.Bind("<uid:cluster-a/Thing/b>", "uid-b")
			}
			err := session.Replay("list")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Replay returned %v, want an error about %q", err, test.want)
			}
		})
	}
}

func TestReplayComparesTextBodiesByteForByte(t *testing.T) {
	fixtures := writeFixtures(t, useFixture)
	server := httptest.NewServer(&fakeService{responses: map[string]cannedResponse{"/use": {404, textType, "Not here\n"}}})
	defer server.Close()

	session := fixtures.NewSession(server.URL)
	session.Bind("<session:second>", "s-2")
	if err := session.Replay("use"); err == nil {
		t.Fatal("a text body with a trailing newline matched one without")
	}
}

func TestReplayExpectsTheResponseAChangeDescribes(t *testing.T) {
	portals := `{
	  "id": "003-portals", "step": "initial", "description": "Portals.",
	  "request": {"method": "GET", "path": "/portals", "headers": {}, "body": null},
	  "response": {"status": 200, "contentType": "application/json; charset=utf-8", "bodyFormat": "json",
	    "body": {"portals": [
	      {"name": "portal-a", "cluster": "cluster-a", "allocated": 0, "generation": "<generation>"},
	      {"name": "portal-b", "cluster": "cluster-b", "allocated": 0, "generation": "<generation>"}]}},
	  "change": {"summary": "Counted.", "spec": "Counting",
	    "expected": {"status": 200, "items": {"portals": {"cluster-b/portal-b": {"allocated": 1}}}}}
	}`
	crash := `{
	  "id": "004-crash", "step": "initial", "description": "A crash.",
	  "request": {"method": "POST", "path": "/crash", "headers": {}, "body": "["},
	  "response": {"status": 500, "contentType": "text/plain; charset=utf-8", "bodyFormat": "text", "body": "500"},
	  "change": {"summary": "A 400.", "spec": "Errors",
	    "expected": {"status": 400, "contentType": "text/plain; charset=utf-8", "bodyFormat": "text"}}
	}`
	same := `{
	  "id": "005-same", "step": "initial", "description": "Same as the portals.",
	  "request": {"method": "GET", "path": "/same", "headers": {}, "body": null},
	  "response": {"status": 500, "contentType": "text/plain; charset=utf-8", "bodyFormat": "text", "body": "500"},
	  "change": {"summary": "No crash.", "spec": "Labels",
	    "expected": {"status": 200, "contentType": "application/json; charset=utf-8", "bodyFormat": "json",
	      "sameBodyAs": "portals"}}
	}`
	fixtures := writeFixtures(t, portals, crash, same)
	counted := `{"portals": [{"name": "portal-a", "cluster": "cluster-a", "allocated": 0, "generation": 3},
		{"name": "portal-b", "cluster": "cluster-b", "allocated": 1, "generation": 1}]}`
	uncounted := strings.Replace(counted, `"allocated": 1`, `"allocated": 0`, 1)
	service := &fakeService{responses: map[string]cannedResponse{
		"/portals": {200, jsonType, counted},
		"/crash":   {400, textType, "Request body is not a JSON object"},
		"/same":    {200, jsonType, uncounted},
	}}
	server := httptest.NewServer(service)
	defer server.Close()

	session := fixtures.NewSession(server.URL)
	for _, name := range []string{"portals", "crash", "same"} {
		if err := session.Replay(name); err != nil {
			t.Fatal(err)
		}
	}

	service.responses["/portals"] = cannedResponse{200, jsonType, uncounted}
	if err := session.Replay("portals"); err == nil {
		t.Fatal("the captured response matched where the change expects another")
	}
}
