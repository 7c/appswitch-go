package appswitch

import (
	"context"
	"sync"
	"time"
)

type subscription struct {
	match func(Event) bool
	fn    Listener
}

// AppSwitch is the appswitch client. Construct with New; call Start before
// reading. After Start, reads are answered synchronously from the in-memory
// snapshot. Safe for concurrent use.
type AppSwitch struct {
	cfg       resolvedConfig
	transport *transport
	disk      *diskCache
	telemetry *telemetry // nil when telemetry is disabled

	mu          sync.RWMutex
	snap        snapshot
	etag        string
	fetchedAt   time.Time
	stage       StageID
	project     string
	fromCache   bool
	established bool

	subsMu    sync.Mutex
	subs      map[int]*subscription
	nextSubID int

	readyCh   chan struct{}
	readyOnce sync.Once

	startMu sync.Mutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New validates config and constructs a client. It does not perform any I/O;
// call Start to load the first snapshot.
func New(cfg Config) (*AppSwitch, error) {
	rc, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	c := &AppSwitch{
		cfg:       rc,
		transport: newTransport(rc),
		disk:      newDiskCache(rc.cacheFolder),
		snap:      snapshot{},
		subs:      make(map[int]*subscription),
		readyCh:   make(chan struct{}),
	}
	if rc.telemetryFlush > 0 {
		c.telemetry = newTelemetry(rc.name, rc.version)
	}
	return c, nil
}

// ── lifecycle ────────────────────────────────────────────────────────

// Start loads any disk cache, performs the first fetch, and starts background
// polling + telemetry. Returns an error only when no snapshot can be obtained
// (no cache and the network is down).
func (c *AppSwitch) Start(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.started {
		return nil
	}
	c.ctx, c.cancel = context.WithCancel(ctx)

	if loaded, ok := c.disk.load(); ok {
		c.mu.Lock()
		c.snap = toSnapshot(loaded.response.Keys)
		c.etag = loaded.etag
		c.fetchedAt = loaded.fetchedAt
		c.stage = loaded.response.Stage
		c.project = loaded.response.Project
		c.fromCache = true
		c.mu.Unlock()
		c.markReady()
	}

	if c.isEstablished() {
		go func() {
			if err := c.Refresh(c.ctx); err != nil {
				c.cfg.onError(err)
			}
		}()
	} else if err := c.Refresh(c.ctx); err != nil {
		return err
	} else if !c.isEstablished() {
		return newError(CodeNotReady, "failed to load initial snapshot")
	}

	c.startTimers()
	c.started = true
	return nil
}

// Stop cancels polling, flushes telemetry, and persists the latest snapshot.
func (c *AppSwitch) Stop() error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if !c.started {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()

	flushCtx, cancel := context.WithTimeout(context.Background(), c.cfg.requestTimeout)
	defer cancel()
	c.flush(flushCtx)

	c.mu.RLock()
	established, etag, at := c.established, c.etag, c.fetchedAt
	resp := keysResponse{Stage: c.stage, Project: c.project, Keys: c.snapshotLocked()}
	c.mu.RUnlock()
	if established {
		c.disk.save(resp, etag, at)
	}

	c.started = false
	return nil
}

// Ready returns a channel closed when the first snapshot is in memory.
func (c *AppSwitch) Ready() <-chan struct{} { return c.readyCh }

// LastFetch reports metadata about the most recent successful fetch.
func (c *AppSwitch) LastFetch() FetchMetadata {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return FetchMetadata{
		ETag:      c.etag,
		FetchedAt: c.fetchedAt,
		Stage:     c.stage,
		Project:   c.project,
		FromCache: c.fromCache,
	}
}

// Refresh fetches the snapshot conditionally; on change it diffs and fires hooks.
func (c *AppSwitch) Refresh(ctx context.Context) error {
	result, err := c.transport.fetchSnapshot(ctx, c.currentEtag())
	if err != nil {
		c.cfg.onError(err)
		if !c.cfg.staleIfError && !c.isEstablished() {
			return err
		}
		return nil
	}
	if result.notModified {
		c.mu.Lock()
		c.fetchedAt = time.Now()
		c.fromCache = false
		c.mu.Unlock()
		return nil
	}

	at := time.Now()
	next := toSnapshot(result.response.Keys)
	var events []Event

	c.mu.Lock()
	wasEstablished := c.established
	if wasEstablished {
		events = diffSnapshots(c.snap, next, at)
	}
	c.snap = next
	c.etag = result.etag
	c.fetchedAt = at
	c.stage = result.response.Stage
	c.project = result.response.Project
	c.fromCache = false
	c.mu.Unlock()

	if wasEstablished {
		c.emit(events)
	} else {
		c.markReady()
	}
	c.disk.save(*result.response, result.etag, at)
	return nil
}

// ── reads ────────────────────────────────────────────────────────────

func serveTyped[T any](c *AppSwitch, path string, coerce func(ResolvedKey) (T, error), fallback []T) (T, error) {
	key, err := c.serve(path)
	if err != nil {
		if len(fallback) > 0 && isAvailabilityErr(err) {
			return fallback[0], nil
		}
		var zero T
		return zero, err
	}
	return coerce(key)
}

func identity(k ResolvedKey) (any, error) { return k.Value, nil }

// Get returns the resolved value, or a fallback when unavailable.
func (c *AppSwitch) Get(path string, fallback ...any) (any, error) {
	return serveTyped(c, path, identity, fallback)
}

// Raw is an alias of Get — the JSON value, unchecked.
func (c *AppSwitch) Raw(path string, fallback ...any) (any, error) {
	return serveTyped(c, path, identity, fallback)
}

func (c *AppSwitch) Number(path string, fallback ...float64) (float64, error) {
	return serveTyped(c, path, asNumber, fallback)
}
func (c *AppSwitch) String(path string, fallback ...string) (string, error) {
	return serveTyped(c, path, asString, fallback)
}
func (c *AppSwitch) Bool(path string, fallback ...bool) (bool, error) {
	return serveTyped(c, path, asBool, fallback)
}
func (c *AppSwitch) URL(path string, fallback ...string) (string, error) {
	return serveTyped(c, path, asURL, fallback)
}
func (c *AppSwitch) Datetime(path string, fallback ...time.Time) (time.Time, error) {
	return serveTyped(c, path, asDatetime, fallback)
}
func (c *AppSwitch) Interval(path string, fallback ...time.Duration) (time.Duration, error) {
	return serveTyped(c, path, asInterval, fallback)
}
func (c *AppSwitch) ArrayString(path string, fallback ...[]string) ([]string, error) {
	return serveTyped(c, path, asArrayString, fallback)
}
func (c *AppSwitch) ArrayNumber(path string, fallback ...[]float64) ([]float64, error) {
	return serveTyped(c, path, asArrayNumber, fallback)
}
func (c *AppSwitch) Enum(path string, fallback ...string) (string, error) {
	return serveTyped(c, path, asEnum, fallback)
}

// JSON decodes a json-typed key into out (e.g. a struct pointer).
func (c *AppSwitch) JSON(path string, out any) error {
	key, err := c.serve(path)
	if err != nil {
		return err
	}
	return asJSON(key, out)
}

// Snapshot returns every resolved key currently in memory.
func (c *AppSwitch) Snapshot() []ResolvedKey {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshotLocked()
}

// ── handles ──────────────────────────────────────────────────────────

// Key returns a handle to a single key.
func (c *AppSwitch) Key(path string) *AppSwitchKey {
	return &AppSwitchKey{c: c, Path: path}
}

// Section returns a handle to a dotted section / glob selector.
func (c *AppSwitch) Section(selector string) *AppSwitchSection {
	return &AppSwitchSection{c: c, Selector: selector}
}

// ── hooks ────────────────────────────────────────────────────────────

func (c *AppSwitch) OnChange(fn Listener) Unsubscribe {
	return c.subscribe(func(Event) bool { return true }, fn)
}
func (c *AppSwitch) OnSection(selector string, fn Listener) Unsubscribe {
	return c.subscribe(func(e Event) bool { return matchesSection(selector, e.Path) }, fn)
}
func (c *AppSwitch) OnNew(fn Listener) Unsubscribe {
	return c.subscribe(func(e Event) bool { return e.Kind == EventNew }, fn)
}
func (c *AppSwitch) OnModify(fn Listener) Unsubscribe {
	return c.subscribe(func(e Event) bool { return e.Kind == EventModified }, fn)
}
func (c *AppSwitch) OnRemove(fn Listener) Unsubscribe {
	return c.subscribe(func(e Event) bool { return e.Kind == EventRemoved }, fn)
}

// ── internals ────────────────────────────────────────────────────────

func (c *AppSwitch) serve(path string) (ResolvedKey, error) {
	c.mu.RLock()
	established, fetchedAt := c.established, c.fetchedAt
	key, ok := c.snap[path]
	c.mu.RUnlock()

	if !established {
		return ResolvedKey{}, newError(CodeNotReady, "client not started")
	}
	if c.cfg.maxStaleness > 0 && !fetchedAt.IsZero() && time.Since(fetchedAt) > c.cfg.maxStaleness {
		return ResolvedKey{}, newError(CodeStale, "snapshot exceeds MaxStaleness for \""+path+"\"")
	}
	if !ok {
		return ResolvedKey{}, newError(CodeNotFound, "key \""+path+"\" not found")
	}
	if c.telemetry != nil {
		c.telemetry.record(path)
	}
	return key, nil
}

func (c *AppSwitch) lookup(path string) (ResolvedKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, ok := c.snap[path]
	return k, ok
}

func (c *AppSwitch) paths() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.snap))
	for p := range c.snap {
		out = append(out, p)
	}
	return out
}

// snapshotLocked copies snapshot values; caller must hold c.mu (R or W).
func (c *AppSwitch) snapshotLocked() []ResolvedKey {
	out := make([]ResolvedKey, 0, len(c.snap))
	for _, k := range c.snap {
		out = append(out, k)
	}
	return out
}

func (c *AppSwitch) currentEtag() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.etag
}

func (c *AppSwitch) isEstablished() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.established
}

func (c *AppSwitch) markReady() {
	c.mu.Lock()
	c.established = true
	c.mu.Unlock()
	c.readyOnce.Do(func() { close(c.readyCh) })
}

func (c *AppSwitch) subscribe(match func(Event) bool, fn Listener) Unsubscribe {
	c.subsMu.Lock()
	id := c.nextSubID
	c.nextSubID++
	c.subs[id] = &subscription{match: match, fn: fn}
	c.subsMu.Unlock()
	return func() {
		c.subsMu.Lock()
		delete(c.subs, id)
		c.subsMu.Unlock()
	}
}

func (c *AppSwitch) emit(events []Event) {
	if len(events) == 0 {
		return
	}
	c.subsMu.Lock()
	subs := make([]*subscription, 0, len(c.subs))
	for _, s := range c.subs {
		subs = append(subs, s)
	}
	c.subsMu.Unlock()

	for _, ev := range events {
		for _, s := range subs {
			if s.match(ev) {
				s.fn(ev)
			}
		}
	}
}

func (c *AppSwitch) startTimers() {
	if c.cfg.pollInterval > 0 {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			t := time.NewTicker(c.cfg.pollInterval)
			defer t.Stop()
			for {
				select {
				case <-c.ctx.Done():
					return
				case <-t.C:
					if err := c.Refresh(c.ctx); err != nil {
						c.cfg.onError(err)
					}
				}
			}
		}()
	}
	if c.telemetry != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			t := time.NewTicker(c.cfg.telemetryFlush)
			defer t.Stop()
			for {
				select {
				case <-c.ctx.Done():
					return
				case <-t.C:
					c.flush(c.ctx)
				}
			}
		}()
	}
}

func (c *AppSwitch) flush(ctx context.Context) {
	if c.telemetry == nil {
		return
	}
	entries := c.telemetry.drain()
	if len(entries) == 0 {
		return
	}
	_ = c.transport.postStats(ctx, entries)
}
