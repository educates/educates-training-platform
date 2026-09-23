package config

import (
	"errors"
	"sync"
	"testing"
	"time"
)

const (
	supportedAPIServerVersion   = "v1.36.4"
	unsupportedAPIServerVersion = "v1.35.0"
)

// countingReader records how often the version was actually read, and can be
// told to fail, standing in for a discovery client.
type countingReader struct {
	mutex   sync.Mutex
	reads   int
	version string
	err     error
}

func (r *countingReader) read() (string, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.reads++

	if r.err != nil {
		return "", r.err
	}

	return r.version, nil
}

func (r *countingReader) count() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.reads
}

func (r *countingReader) set(version string, err error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.version = version
	r.err = err
}

func TestAPIServerVersionCacheReadsOnce(t *testing.T) {
	reader := &countingReader{version: supportedAPIServerVersion}
	cache := &apiServerVersionCache{refreshAfter: time.Hour}

	for range 5 {
		version, err := cache.get(reader.read)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if version != supportedAPIServerVersion {
			t.Errorf("version = %q, want %q", version, supportedAPIServerVersion)
		}
	}

	// The API server version changes on upgrade, not between reconciles, so
	// repeated resolutions must not each cost a round trip.
	if reads := reader.count(); reads != 1 {
		t.Errorf("read the API server version %d times, want 1", reads)
	}
}

func TestAPIServerVersionCacheRefreshes(t *testing.T) {
	reader := &countingReader{version: unsupportedAPIServerVersion}
	cache := &apiServerVersionCache{refreshAfter: time.Hour}

	if _, err := cache.get(reader.read); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// An upgrade lands, and the cache is past its refresh interval.
	reader.set(supportedAPIServerVersion, nil)
	cache.expire()

	version, err := cache.get(reader.read)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if version != supportedAPIServerVersion {
		t.Errorf("version = %q, want the upgraded %q", version, supportedAPIServerVersion)
	}

	if reads := reader.count(); reads != 2 {
		t.Errorf("read %d times, want 2", reads)
	}
}

func TestAPIServerVersionCacheKeepsLastKnownValueOnFailure(t *testing.T) {
	reader := &countingReader{version: supportedAPIServerVersion}
	cache := &apiServerVersionCache{refreshAfter: time.Hour}

	if _, err := cache.get(reader.read); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The API server goes briefly unreachable and the refresh is due. A blip
	// must not be reported as a probe failure, because that would flip the
	// condition and roll the session manager on a cluster which is fine.
	reader.set("", errors.New("connection refused"))
	cache.expire()

	version, err := cache.get(reader.read)

	if err != nil {
		t.Fatalf("a failed refresh should not surface an error: %v", err)
	}

	if version != supportedAPIServerVersion {
		t.Errorf("version = %q, want the last known %q", version, supportedAPIServerVersion)
	}
}

func TestAPIServerVersionCacheBacksOffAfterAFailedRefresh(t *testing.T) {
	reader := &countingReader{version: supportedAPIServerVersion}
	cache := &apiServerVersionCache{refreshAfter: time.Hour}

	if _, err := cache.get(reader.read); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader.set("", errors.New("connection refused"))
	cache.expire()

	// The refresh fails, and the reconciler carries on with the last known
	// value.
	if _, err := cache.get(reader.read); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	readsAfterFailure := reader.count()

	// Reconciles keep arriving while the API server is unreachable. Retrying
	// on every one of them would restore the per reconcile round trip this
	// cache exists to remove, so the failure is held for the refresh interval
	// like a success.
	for range 5 {
		if _, err := cache.get(reader.read); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if reads := reader.count(); reads != readsAfterFailure {
		t.Errorf("retried %d times during the outage, want no further reads", reads-readsAfterFailure)
	}
}

func TestAPIServerVersionCacheReportsFirstFailure(t *testing.T) {
	reader := &countingReader{err: errors.New("connection refused")}
	cache := &apiServerVersionCache{refreshAfter: time.Hour}

	// With no value ever read there is nothing to fall back to, so the
	// failure is real and must be reported.
	if _, err := cache.get(reader.read); err == nil {
		t.Fatal("expected the first failure to surface")
	}

	// A later success is picked up rather than the failure being cached.
	reader.set(supportedAPIServerVersion, nil)

	version, err := cache.get(reader.read)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if version != supportedAPIServerVersion {
		t.Errorf("version = %q, want %q", version, supportedAPIServerVersion)
	}
}

func TestAPIServerVersionCacheRefreshesWhenZeroValued(t *testing.T) {
	reader := &countingReader{version: unsupportedAPIServerVersion}

	// The reconciler embeds the cache, so the zero value is what production
	// runs with. It must still refresh, or a control plane upgrade would
	// never be noticed without restarting the operator.
	cache := &apiServerVersionCache{}

	if _, err := cache.get(reader.read); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader.set(supportedAPIServerVersion, nil)
	cache.expire()

	version, err := cache.get(reader.read)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if version != supportedAPIServerVersion {
		t.Errorf("version = %q, want the upgraded %q", version, supportedAPIServerVersion)
	}
}

func TestAPIServerVersionCacheIsConcurrencySafe(t *testing.T) {
	reader := &countingReader{version: supportedAPIServerVersion}
	cache := &apiServerVersionCache{refreshAfter: time.Hour}

	var group sync.WaitGroup

	for range 16 {
		group.Go(func() {
			if _, err := cache.get(reader.read); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	group.Wait()

	if reads := reader.count(); reads != 1 {
		t.Errorf("read %d times under concurrency, want 1", reads)
	}
}
