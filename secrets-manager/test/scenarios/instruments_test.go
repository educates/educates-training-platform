package scenarios

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// RacedWrites lets a scenario act just before a write of the
// secrets-manager reaches the API server, as another writer acting at that
// moment would, from the transport of the client configuration the
// secrets-manager is given.
type RacedWrites struct {
	mu    sync.Mutex
	races []*Race
}

// Race is what a scenario does just before a write of the secrets-manager
// it picks.
type Race struct {
	picks func(method, path string) bool
	act   func() error

	mu   sync.Mutex
	ran  bool
	err  error
	done bool
}

// Before has act run once, just before the first write request of the
// secrets-manager that picks chooses by its method and path, holding the
// request until act returns. Reads are never picked.
func (w *RacedWrites) Before(picks func(method, path string) bool, act func() error) *Race {
	race := &Race{picks: picks, act: act}
	w.mu.Lock()
	w.races = append(w.races, race)
	w.mu.Unlock()
	return race
}

// Ran reports whether the race has run, and what its act returned.
func (r *Race) Ran() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ran, r.err
}

// Cancel keeps the race from running, when it has not run yet.
func (r *Race) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = true
}

// instrument has config's transport run the races of w before the write
// requests they pick.
func (w *RacedWrites) instrument(config *rest.Config) {
	config.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return roundTripper(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet {
				w.run(request.Method, request.URL.Path)
			}
			return next.RoundTrip(request)
		})
	})
}

// run runs the races that pick a write request of method to path, and have
// not run.
func (w *RacedWrites) run(method, path string) {
	w.mu.Lock()
	races := append([]*Race(nil), w.races...)
	w.mu.Unlock()
	for _, race := range races {
		race.mu.Lock()
		if !race.done && race.picks(method, path) {
			race.done, race.ran = true, true
			race.err = race.act()
		}
		race.mu.Unlock()
	}
}

// requireRacedWrites skips the scenario on a target that cannot race the
// writes of its secrets-manager.
func requireRacedWrites() {
	if target.RacedWrites == nil {
		Skip(target.Name + " cannot race the writes of its secrets-manager")
	}
}

// requireRestart skips the scenario on a target that cannot restart its
// secrets-manager.
func requireRestart() {
	if target.Restart == nil {
		Skip(target.Name + " cannot restart its secrets-manager")
	}
}

// MissedEvents keeps the event handlers of a secrets-manager's cache from
// hearing of changes to the objects it is told about. The cache itself
// still holds those changes, as when an event reaches the cache but is
// mapped to the wrong resource, or to none.
type MissedEvents struct {
	mu      sync.Mutex
	next    int
	missing map[int]func(client.Object) bool
}

// Miss keeps the event handlers from hearing of changes to the objects
// about selects, until the function it returns is called.
func (m *MissedEvents) Miss(about func(client.Object) bool) (stop func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.missing == nil {
		m.missing = map[int]func(client.Object) bool{}
	}
	id := m.next
	m.next++
	m.missing[id] = about
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.missing, id)
	}
}

// missed reports whether an event about obj is to be missed.
func (m *MissedEvents) missed(obj any) bool {
	if tombstone, ok := obj.(toolscache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	o, ok := obj.(client.Object)
	if !ok {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, about := range m.missing {
		if about(o) {
			return true
		}
	}
	return false
}

// newCache creates the cache controller-runtime would, its event handlers
// missing the events m says.
func (m *MissedEvents) newCache(config *rest.Config, opts cache.Options) (cache.Cache, error) {
	c, err := cache.New(config, opts)
	if err != nil {
		return nil, err
	}
	return &missingCache{Cache: c, events: m}, nil
}

// requireMissedEvents skips the scenario on a target that cannot make its
// secrets-manager miss events.
func requireMissedEvents() {
	if target.MissedEvents == nil {
		Skip(target.Name + " cannot make its secrets-manager miss events")
	}
}

// missingCache is a cache whose informers' event handlers miss the events
// its MissedEvents says.
type missingCache struct {
	cache.Cache
	events *MissedEvents
}

func (c *missingCache) GetInformer(ctx context.Context, obj client.Object,
	opts ...cache.InformerGetOption) (cache.Informer, error) {
	informer, err := c.Cache.GetInformer(ctx, obj, opts...)
	if err != nil {
		return nil, err
	}
	return &missingInformer{Informer: informer, events: c.events}, nil
}

// missingInformer is an informer whose event handlers miss the events its
// MissedEvents says.
type missingInformer struct {
	cache.Informer
	events *MissedEvents
}

func (i *missingInformer) AddEventHandler(
	handler toolscache.ResourceEventHandler) (toolscache.ResourceEventHandlerRegistration, error) {
	return i.Informer.AddEventHandler(&missingHandler{handler: handler, events: i.events})
}

func (i *missingInformer) AddEventHandlerWithResyncPeriod(handler toolscache.ResourceEventHandler,
	resyncPeriod time.Duration) (toolscache.ResourceEventHandlerRegistration, error) {
	return i.Informer.AddEventHandlerWithResyncPeriod(&missingHandler{handler: handler, events: i.events},
		resyncPeriod)
}

func (i *missingInformer) AddEventHandlerWithOptions(handler toolscache.ResourceEventHandler,
	options toolscache.HandlerOptions) (toolscache.ResourceEventHandlerRegistration, error) {
	return i.Informer.AddEventHandlerWithOptions(&missingHandler{handler: handler, events: i.events}, options)
}

// missingHandler passes on the events its MissedEvents does not say to
// miss.
type missingHandler struct {
	handler toolscache.ResourceEventHandler
	events  *MissedEvents
}

func (h *missingHandler) OnAdd(obj any, isInInitialList bool) {
	if !h.events.missed(obj) {
		h.handler.OnAdd(obj, isInInitialList)
	}
}

func (h *missingHandler) OnUpdate(oldObj, newObj any) {
	if !h.events.missed(newObj) {
		h.handler.OnUpdate(oldObj, newObj)
	}
}

func (h *missingHandler) OnDelete(obj any) {
	if !h.events.missed(obj) {
		h.handler.OnDelete(obj)
	}
}
