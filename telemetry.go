package appswitch

import (
	"sync"
	"time"
)

type counter struct {
	count    int
	lastSeen time.Time
}

// telemetry accumulates per-path read counters (CLIENT.md §4), drained to
// POST /v1/_stats. host carries the client name, version the client version.
type telemetry struct {
	mu       sync.Mutex
	host     string
	version  string
	now      func() time.Time
	counters map[string]*counter
}

func newTelemetry(host, version string) *telemetry {
	return &telemetry{
		host:     host,
		version:  version,
		now:      time.Now,
		counters: make(map[string]*counter),
	}
}

func (t *telemetry) record(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.counters[path]; ok {
		c.count++
		c.lastSeen = t.now()
		return
	}
	t.counters[path] = &counter{count: 1, lastSeen: t.now()}
}

// drain returns the accumulated counters as wire rows and clears them.
func (t *telemetry) drain() []statsEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]statsEntry, 0, len(t.counters))
	for path, c := range t.counters {
		entries = append(entries, statsEntry{
			Path:     path,
			Host:     t.host,
			Version:  t.version,
			Count:    c.count,
			LastSeen: c.lastSeen.UTC().Format(time.RFC3339Nano),
		})
	}
	t.counters = make(map[string]*counter)
	return entries
}
