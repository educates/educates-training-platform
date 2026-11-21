package scenarios

import (
	"net/http"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/rest"
)

// APICalls counts the requests a secrets-manager makes to the API server,
// watches aside, from the transport of the client configuration it is
// given.
type APICalls struct {
	mu    sync.Mutex
	calls []apiCall
}

// apiCall is one request to the API server.
type apiCall struct {
	method string
	path   string
}

// APICallCount counts requests: reads are gets and lists, writes every
// other request.
type APICallCount struct {
	Reads  int
	Writes int
}

// instrument has config's transport record each request but watches in
// calls.
func (calls *APICalls) instrument(config *rest.Config) {
	config.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return roundTripper(func(request *http.Request) (*http.Response, error) {
			if watch := request.URL.Query().Get("watch"); watch != "true" && watch != "1" {
				calls.mu.Lock()
				calls.calls = append(calls.calls, apiCall{method: request.Method, path: request.URL.Path})
				calls.mu.Unlock()
			}
			return next.RoundTrip(request)
		})
	})
}

// About counts the requests whose path names part, such as the id of a
// scenario, which every name it gives holds.
func (calls *APICalls) About(part string) APICallCount {
	calls.mu.Lock()
	defer calls.mu.Unlock()
	count := APICallCount{}
	for _, call := range calls.calls {
		switch {
		case !strings.Contains(call.path, part):
		case call.method == http.MethodGet:
			count.Reads++
		default:
			count.Writes++
		}
	}
	return count
}

// roundTripper is a function that serves as an http.RoundTripper.
type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// requireAPICalls skips the scenario on a target that cannot count the
// requests of its secrets-manager.
func requireAPICalls() {
	if target.APICalls == nil {
		Skip(target.Name + " cannot count the requests of its secrets-manager")
	}
}

// settledAPICalls waits until the count of the secrets-manager's requests
// about part keeps its value between two reads, and returns it.
func settledAPICalls(part string) APICallCount {
	GinkgoHelper()
	var last *APICallCount
	Eventually(func(g Gomega) {
		current := target.APICalls.About(part)
		stable := last != nil && current == *last
		last = &current
		g.Expect(stable).To(BeTrue(), "requests about %s still being made", part)
	}).WithTimeout(target.Timing.Converge).WithPolling(5 * target.Timing.Poll).Should(Succeed())
	return *last
}

// requireRestart skips the scenario on a target that cannot restart its
// secrets-manager.
func requireRestart() {
	if target.Restart == nil {
		Skip(target.Name + " cannot restart its secrets-manager")
	}
}
