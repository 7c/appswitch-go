package appswitch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	appswitch "github.com/7c/appswitch-go"
)

// ── mock backend ─────────────────────────────────────────────────────

type statsBatch struct {
	Entries []struct {
		Path    string `json:"path"`
		Host    string `json:"host"`
		Version string `json:"version"`
		Count   int    `json:"count"`
	} `json:"entries"`
}

type mockServer struct {
	mu      sync.Mutex
	srv     *httptest.Server
	keys    []appswitch.ResolvedKey
	etag    int
	batches []statsBatch
}

func newMockServer(initial []appswitch.ResolvedKey) *mockServer {
	m := &mockServer{keys: initial, etag: 1}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *mockServer) URL() string { return m.srv.URL }
func (m *mockServer) Close()      { m.srv.Close() }

func (m *mockServer) setKeys(keys []appswitch.ResolvedKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys = keys
	m.etag++
}

func (m *mockServer) handle(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case r.URL.Path == "/v1/keys":
		tag := `"` + itoa(m.etag) + `"`
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", tag)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stage": "prod", "project": "test", "keys": m.keys,
		})

	case len(r.URL.Path) > len("/v1/keys/") && r.URL.Path[:len("/v1/keys/")] == "/v1/keys/":
		path := r.URL.Path[len("/v1/keys/"):]
		for _, k := range m.keys {
			if k.Path == path {
				_ = json.NewEncoder(w).Encode(k)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "NOT_FOUND", "message": "no"})

	case r.URL.Path == "/v1/_stats":
		var b statsBatch
		_ = json.NewDecoder(r.Body).Decode(&b)
		m.batches = append(m.batches, b)
		w.WriteHeader(http.StatusAccepted)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func key(path string, t appswitch.DataType, value any) appswitch.ResolvedKey {
	return appswitch.ResolvedKey{Path: path, Type: t, Value: value, ResolvedFrom: appswitch.StageProd, Explicit: true}
}

func baseConfig(url string) appswitch.Config {
	return appswitch.Config{
		Name:                   "checkout-api",
		Version:                "3.2.1",
		APIKey:                 "ws_pk_test_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Endpoint:               url,
		PollInterval:           -1, // disabled (drive via Refresh)
		TelemetryFlushInterval: -1, // disabled
		DisableDiskCache:       true,
		OnError:                func(error) {},
	}
}

func defaultKeys() []appswitch.ResolvedKey {
	return []appswitch.ResolvedKey{
		key("main.lockport", appswitch.TypeNumber, 8443),
		key("main.maintenance", appswitch.TypeBoolean, false),
		key("analytics.endpoint", appswitch.TypeURL, "https://a.example"),
		key("features.checkout.regions", appswitch.TypeArrayString, []string{"US", "CA"}),
		key("features.checkout.theme", appswitch.TypeEnumString, "dark"),
	}
}

func ctx() context.Context { return context.Background() }

// ── tests ────────────────────────────────────────────────────────────

func TestNewValidation(t *testing.T) {
	if _, err := appswitch.New(appswitch.Config{Version: "1", APIKey: "k"}); appswitch.CodeOf(err) != appswitch.CodeConfig {
		t.Fatalf("expected CONFIG error for missing name, got %v", err)
	}
	if _, err := appswitch.New(appswitch.Config{Name: "a", APIKey: "k"}); appswitch.CodeOf(err) != appswitch.CodeConfig {
		t.Fatalf("expected CONFIG error for missing version, got %v", err)
	}
	if _, err := appswitch.New(appswitch.Config{Name: "a", Version: "1"}); appswitch.CodeOf(err) != appswitch.CodeConfig {
		t.Fatalf("expected CONFIG error for missing apiKey, got %v", err)
	}
}

func TestStartAndTypedReads(t *testing.T) {
	m := newMockServer(defaultKeys())
	defer m.Close()
	c, err := appswitch.New(baseConfig(m.URL()))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx()); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	if n, _ := c.Number("main.lockport"); n != 8443 {
		t.Fatalf("Number = %v, want 8443", n)
	}
	if b, _ := c.Bool("main.maintenance"); b != false {
		t.Fatalf("Bool = %v", b)
	}
	if u, _ := c.URL("analytics.endpoint"); u != "https://a.example" {
		t.Fatalf("URL = %v", u)
	}
	regions, _ := c.ArrayString("features.checkout.regions")
	if len(regions) != 2 || regions[0] != "US" {
		t.Fatalf("ArrayString = %v", regions)
	}
	if e, _ := c.Enum("features.checkout.theme"); e != "dark" {
		t.Fatalf("Enum = %v", e)
	}
	if got := len(c.Snapshot()); got != 5 {
		t.Fatalf("Snapshot len = %d", got)
	}
	if c.LastFetch().Stage != appswitch.StageProd {
		t.Fatalf("stage = %v", c.LastFetch().Stage)
	}
}

func TestTypeMismatch(t *testing.T) {
	m := newMockServer(defaultKeys())
	defer m.Close()
	c, _ := appswitch.New(baseConfig(m.URL()))
	_ = c.Start(ctx())
	defer c.Stop()

	if _, err := c.Number("main.maintenance"); appswitch.CodeOf(err) != appswitch.CodeTypeMismatch {
		t.Fatalf("expected TYPE_MISMATCH, got %v", err)
	}
}

func TestFallbackAndReadiness(t *testing.T) {
	m := newMockServer(defaultKeys())
	defer m.Close()
	c, _ := appswitch.New(baseConfig(m.URL()))

	// before Start → NOT_READY, unless fallback
	if _, err := c.Number("main.lockport"); appswitch.CodeOf(err) != appswitch.CodeNotReady {
		t.Fatalf("expected NOT_READY, got %v", err)
	}
	if n, err := c.Number("main.lockport", 1); err != nil || n != 1 {
		t.Fatalf("fallback before ready: %v %v", n, err)
	}

	_ = c.Start(ctx())
	defer c.Stop()

	if _, err := c.Number("missing.key"); appswitch.CodeOf(err) != appswitch.CodeNotFound {
		t.Fatalf("expected NOT_FOUND, got %v", err)
	}
	if n, _ := c.Number("missing.key", 7); n != 7 {
		t.Fatalf("fallback for missing: %v", n)
	}
}

func TestETagConditional(t *testing.T) {
	m := newMockServer(defaultKeys())
	defer m.Close()
	c, _ := appswitch.New(baseConfig(m.URL()))
	_ = c.Start(ctx())
	defer c.Stop()

	etag := c.LastFetch().ETag
	if etag == "" {
		t.Fatal("expected an ETag")
	}
	// no change → 304 → value preserved
	if err := c.Refresh(ctx()); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Number("main.lockport"); n != 8443 {
		t.Fatalf("after 304, Number = %v", n)
	}
}

func TestChangeHooks(t *testing.T) {
	m := newMockServer(defaultKeys())
	defer m.Close()
	c, _ := appswitch.New(baseConfig(m.URL()))

	var mu sync.Mutex
	var events []appswitch.Event
	c.OnChange(func(e appswitch.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	_ = c.Start(ctx())
	defer c.Stop()

	mu.Lock()
	if len(events) != 0 {
		t.Fatalf("first snapshot should be silent, got %d events", len(events))
	}
	mu.Unlock()

	m.setKeys([]appswitch.ResolvedKey{
		key("main.lockport", appswitch.TypeNumber, 9000), // modified
		key("main.maintenance", appswitch.TypeBoolean, false),
		key("analytics.endpoint", appswitch.TypeURL, "https://a.example"),
		key("new.flag", appswitch.TypeBoolean, true), // new; features.* removed
	})
	if err := c.Refresh(ctx()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	kinds := map[string]appswitch.EventKind{}
	paths := []string{}
	for _, e := range events {
		kinds[e.Path] = e.Kind
		paths = append(paths, e.Path)
	}
	if kinds["main.lockport"] != appswitch.EventModified {
		t.Fatalf("lockport kind = %v", kinds["main.lockport"])
	}
	if kinds["new.flag"] != appswitch.EventNew {
		t.Fatalf("new.flag kind = %v", kinds["new.flag"])
	}
	if kinds["features.checkout.regions"] != appswitch.EventRemoved {
		t.Fatalf("regions kind = %v", kinds["features.checkout.regions"])
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("events not in lexicographic order: %v", paths)
	}
}

func TestHandlesAndUnsubscribe(t *testing.T) {
	m := newMockServer(defaultKeys())
	defer m.Close()
	c, _ := appswitch.New(baseConfig(m.URL()))

	var sectionHits []string
	var keyHits []any
	offSection := c.Section("features.checkout").OnChange(func(e appswitch.Event) {
		sectionHits = append(sectionHits, e.Path)
	})
	c.Key("main.lockport").OnChange(func(e appswitch.Event) { keyHits = append(keyHits, e.Current) })

	_ = c.Start(ctx())
	defer c.Stop()
	offSection() // unsubscribe before the change

	m.setKeys([]appswitch.ResolvedKey{
		key("main.lockport", appswitch.TypeNumber, 9001),
		key("features.checkout.theme", appswitch.TypeEnumString, "light"),
	})
	_ = c.Refresh(ctx())

	if len(keyHits) != 1 {
		t.Fatalf("key handle hits = %v", keyHits)
	}
	if len(sectionHits) != 0 {
		t.Fatalf("section should be unsubscribed, got %v", sectionHits)
	}
	if !c.Key("main.lockport").Exists() {
		t.Fatal("key should exist")
	}
}

func TestJSONAccessor(t *testing.T) {
	m := newMockServer([]appswitch.ResolvedKey{
		key("features.shipping", appswitch.TypeJSON, map[string]any{"flat": 5.0, "regions": []any{"US"}}),
	})
	defer m.Close()
	c, _ := appswitch.New(baseConfig(m.URL()))
	_ = c.Start(ctx())
	defer c.Stop()

	var shipping struct {
		Flat    float64  `json:"flat"`
		Regions []string `json:"regions"`
	}
	if err := c.JSON("features.shipping", &shipping); err != nil {
		t.Fatal(err)
	}
	if shipping.Flat != 5 || len(shipping.Regions) != 1 {
		t.Fatalf("decoded = %+v", shipping)
	}
}

func TestDiskCacheAndResilience(t *testing.T) {
	dir := t.TempDir()
	m := newMockServer(defaultKeys())

	cfgA := baseConfig(m.URL())
	cfgA.DisableDiskCache = false
	cfgA.CacheFolder = dir
	a, _ := appswitch.New(cfgA)
	if err := a.Start(ctx()); err != nil {
		t.Fatal(err)
	}
	_ = a.Stop()

	if _, err := os.Stat(filepath.Join(dir, "snapshot.json")); err != nil {
		t.Fatal("snapshot.json not written")
	}

	m.Close() // network down

	errCh := make(chan error, 4)
	cfgB := baseConfig(m.URL())
	cfgB.DisableDiskCache = false
	cfgB.CacheFolder = dir
	cfgB.OnError = func(e error) { errCh <- e }
	b, _ := appswitch.New(cfgB)
	if err := b.Start(ctx()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()

	if n, _ := b.Number("main.lockport", 0); n != 8443 {
		t.Fatalf("expected disk-served 8443, got %v", n)
	}
	if !b.LastFetch().FromCache {
		t.Fatal("expected FromCache true")
	}
	select {
	case <-errCh: // background refresh failed and was reported
	case <-time.After(2 * time.Second):
		t.Fatal("expected a background refresh error")
	}
}

func TestTelemetryFlushOnStop(t *testing.T) {
	m := newMockServer(defaultKeys())
	defer m.Close()
	cfg := baseConfig(m.URL())
	cfg.TelemetryFlushInterval = 0 // enable (default 60s; timer won't fire in-test)
	c, _ := appswitch.New(cfg)
	_ = c.Start(ctx())

	_, _ = c.Number("main.lockport")
	_, _ = c.Number("main.lockport")
	_, _ = c.Bool("main.maintenance")
	_ = c.Stop() // flushes telemetry

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.batches) != 1 {
		t.Fatalf("expected 1 stats batch, got %d", len(m.batches))
	}
	var lockport int
	var host, version string
	for _, e := range m.batches[0].Entries {
		if e.Path == "main.lockport" {
			lockport = e.Count
			host = e.Host
			version = e.Version
		}
	}
	if lockport != 2 || host != "checkout-api" || version != "3.2.1" {
		t.Fatalf("telemetry = count %d host %q version %q", lockport, host, version)
	}
}
