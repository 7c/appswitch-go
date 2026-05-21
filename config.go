package appswitch

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

// SDKVersion is embedded in the default User-Agent.
const SDKVersion = "0.1.0"

const defaultEndpoint = "https://api.appswitch.example.com"

// Config configures an AppSwitch client. Name, Version, and APIKey are required.
//
// Zero-value semantics (Go has no "unset"):
//   - CacheTTL == 0      → unlimited (default).
//   - PollInterval == 0  → default 5m; negative → disabled (manual Refresh only).
//   - TelemetryFlushInterval == 0 → default 60s; negative → disabled.
//   - MaxStaleness == 0  → infinite (always serve any snapshot).
//   - StaleIfError == nil → true (use Bool to override).
//   - CacheFolder == ""  → OS default; set DisableDiskCache to run in-memory only.
type Config struct {
	Name    string // required — identifies the deployable in User-Agent/telemetry/audit
	Version string // required
	APIKey  string // required — per-stage key (ws_pk_(live|test)_…)

	Endpoint string

	CacheTTL               time.Duration
	PollInterval           time.Duration
	MaxStaleness           time.Duration
	NegativeCacheTTL       time.Duration
	TelemetryFlushInterval time.Duration
	RequestTimeout         time.Duration

	CacheFolder      string
	DisableDiskCache bool

	StaleIfError *bool

	UserAgent string
	OnError   func(error)
}

// Bool returns a pointer to b — for setting *bool config fields inline.
func Bool(b bool) *bool { return &b }

type resolvedConfig struct {
	name, version, apiKey, endpoint string
	cacheTTL                        time.Duration // 0 = unlimited
	pollInterval                    time.Duration // 0 = disabled
	maxStaleness                    time.Duration // 0 = infinite
	negativeCacheTTL                time.Duration
	telemetryFlush                  time.Duration // 0 = disabled
	requestTimeout                  time.Duration
	cacheFolder                     string // "" = disk caching disabled
	staleIfError                    bool
	userAgent                       string
	onError                         func(error)
}

var unsafeFolderChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// defaultCacheFolder picks the SDK-owned cache folder per platform (CLIENT.md §5).
func defaultCacheFolder(name string) string {
	safe := unsafeFolderChars.ReplaceAllString(name, "_")
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "appswitch", safe)
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "appswitch", safe)
}

func normalizeConfig(c Config) (resolvedConfig, error) {
	if c.Name == "" {
		return resolvedConfig{}, newError(CodeConfig, "Name is required")
	}
	if c.Version == "" {
		return resolvedConfig{}, newError(CodeConfig, "Version is required")
	}
	if c.APIKey == "" {
		return resolvedConfig{}, newError(CodeConfig, "APIKey is required")
	}

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	for len(endpoint) > 0 && endpoint[len(endpoint)-1] == '/' {
		endpoint = endpoint[:len(endpoint)-1]
	}

	poll := c.PollInterval
	switch {
	case poll == 0:
		poll = 5 * time.Minute
	case poll < 0:
		poll = 0 // disabled
	}

	telemetry := c.TelemetryFlushInterval
	switch {
	case telemetry == 0:
		telemetry = 60 * time.Second
	case telemetry < 0:
		telemetry = 0 // disabled
	}

	negative := c.NegativeCacheTTL
	if negative == 0 {
		negative = 5 * time.Minute
	}
	timeout := c.RequestTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	staleIfError := true
	if c.StaleIfError != nil {
		staleIfError = *c.StaleIfError
	}

	cacheFolder := ""
	if !c.DisableDiskCache {
		cacheFolder = c.CacheFolder
		if cacheFolder == "" {
			cacheFolder = defaultCacheFolder(c.Name)
		}
	}

	ua := c.UserAgent
	if ua == "" {
		ua = "appswitch-go/" + SDKVersion + " " + c.Name + "/" + c.Version
	}

	onError := c.OnError
	if onError == nil {
		onError = func(error) {} // default: silent (Go callers usually log via OnError)
	}

	return resolvedConfig{
		name:             c.Name,
		version:          c.Version,
		apiKey:           c.APIKey,
		endpoint:         endpoint,
		cacheTTL:         c.CacheTTL,
		pollInterval:     poll,
		maxStaleness:     c.MaxStaleness,
		negativeCacheTTL: negative,
		telemetryFlush:   telemetry,
		requestTimeout:   timeout,
		cacheFolder:      cacheFolder,
		staleIfError:     staleIfError,
		userAgent:        ua,
		onError:          onError,
	}, nil
}
