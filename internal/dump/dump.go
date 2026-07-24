// Package dump turns source documents into a versioned element tree for
// stdlib-only ingest scripts (see contapila dump).
package dump

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Node is one element in a hybrid CST: type + optional props + children.
type Node struct {
	Type     string         `json:"type"`
	Props    map[string]any `json:"props,omitempty"`
	Children []Node         `json:"children,omitempty"`
}

// ExtractedData is the envelope written by every dialect.
type ExtractedData struct {
	Dialect string `json:"dialect"`
	Source  string `json:"source"`
	Data    Node   `json:"data"`
}

// Extractor builds ExtractedData for one path.
type Extractor func(path string) (ExtractedData, error)

var (
	mu       sync.RWMutex
	registry = map[string]Extractor{}
)

// Register adds a dialect id → extractor mapping. Safe for init().
func Register(dialect string, fn Extractor) {
	if dialect == "" {
		panic("dump: empty dialect")
	}
	if fn == nil {
		panic("dump: nil extractor for " + dialect)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := registry[dialect]; ok {
		panic("dump: duplicate dialect " + dialect)
	}
	registry[dialect] = fn
}

// Dialects returns registered dialect ids in sorted order.
func Dialects() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the extractor for dialect, or false.
func Lookup(dialect string) (Extractor, bool) {
	mu.RLock()
	defer mu.RUnlock()
	fn, ok := registry[dialect]
	return fn, ok
}

// Extract runs the registered extractor for dialect on path.
func Extract(dialect, path string) (ExtractedData, error) {
	fn, ok := Lookup(dialect)
	if !ok {
		return ExtractedData{}, fmt.Errorf("unknown dialect %q (known: %s)", dialect, joinDialects())
	}
	return fn(path)
}

// MarshalCompact encodes v as compact JSON (no HTML escape).
func MarshalCompact(v any) ([]byte, error) {
	var buf []byte
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func joinDialects() string {
	ids := Dialects()
	if len(ids) == 0 {
		return "(none)"
	}
	out := ids[0]
	for _, id := range ids[1:] {
		out += ", " + id
	}
	return out
}
