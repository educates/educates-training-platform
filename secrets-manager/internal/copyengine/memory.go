package copyengine

import (
	"crypto/sha256"
	"encoding/json"
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

// memory holds, for each copy the engine last found in sync or wrote, the
// versions of the source and of the copy it found or wrote, and the shape
// it gave the copy. A copy whose source, copy and shape are still those is
// in sync, and needs no comparison.
//
// The memory lives as long as the process: after a restart, each copy is
// compared once more.
type memory struct {
	mu     sync.Mutex
	copies map[types.NamespacedName]remembered
}

// remembered is what the engine remembers of one copy.
type remembered struct {
	// source names the source the copy was found in sync with.
	source types.NamespacedName

	// sourceVersion and copyVersion are the resource versions of the
	// source and of the copy.
	sourceVersion string
	copyVersion   string

	// shape identifies the shape the rule gave the copy.
	shape shape
}

// shape identifies everything a Copy says of a copy but its source: the
// copy's name, the rule asking for it, and the labels, annotations, owner
// and any other shape the rule gives it.
type shape [sha256.Size]byte

// shapeOf returns the shape of c.
func shapeOf(c Copy) (shape, error) {
	c.Source = nil
	raw, err := json.Marshal(c)
	if err != nil {
		return shape{}, err
	}
	return sha256.Sum256(raw), nil
}

func newMemory() *memory {
	return &memory{copies: map[types.NamespacedName]remembered{}}
}

// holds reports whether the memory of the copy key is r.
func (m *memory) holds(key types.NamespacedName, r remembered) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, ok := m.copies[key]
	return ok && held == r
}

// remember remembers r of the copy key.
func (m *memory) remember(key types.NamespacedName, r remembered) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.copies[key] = r
}

// forget drops what the memory holds of the copy key.
func (m *memory) forget(key types.NamespacedName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.copies, key)
}
