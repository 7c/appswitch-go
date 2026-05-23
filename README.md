# appswitch-go

Official Go client SDK for **appswitch** — typed, cached, read-only config access.

- **Read once, serve forever** — after `Start`, reads are answered from an in-memory snapshot.
- **Caching on by default** — in-memory always; SDK-owned disk cache, unlimited TTL.
- **Resilient** — keeps serving the last good snapshot when a refresh fails; per-call fallback.
- **Typed accessors** — `Number`, `Bool`, `URL`, `Semver`, `ArrayString`, `Enum`, `JSON(&out)`, … each `(T, error)`.
- **Change hooks** — whole-config, section/glob, and per-key, fired on the polling boundary.
- **Best-effort telemetry** — batched read counters flushed to `/v1/_stats`.

Read-only by design: no management, no writes, no realtime push (clients poll). See
[`docs/CLIENT.md`](../../docs/CLIENT.md) for the full concept. Stdlib-only; Go ≥ 1.24.

## Install

```bash
go get github.com/7c/appswitch-go
```

## Usage

```go
import (
    "context"
    "log"
    "os"
    "time"

    appswitch "github.com/7c/appswitch-go"
)

client, err := appswitch.New(appswitch.Config{
    Name:         "checkout-api",         // required
    Version:      "3.2.1",                // required
    APIKey:       os.Getenv("APPSWITCH_KEY"),
    Endpoint:     "https://api.appswitch.example.com",
    PollInterval: 5 * time.Minute,        // 0 = default 5m; negative = disabled
    CacheTTL:     0,                       // 0 = unlimited (default)
    StaleIfError: appswitch.Bool(true),    // default true
})
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
if err := client.Start(ctx); err != nil {
    log.Fatal(err)
}
defer client.Stop()

port, _ := client.Number("main.lockport")          // 8443
theme, _ := client.Enum("features.checkout.theme")  // "dark"
minVer, _ := client.Semver("mobile.minSupportedVersion") // "3.4.0"
if v, err := client.SemverObject("mobile.minSupportedVersion"); err == nil && v.Lt(appswitch.MustParseSemver("4.0.0")) {
    // force upgrade
}

var shipping struct {
    Flat    float64  `json:"flat"`
    Regions []string `json:"regions"`
}
_ = client.JSON("features.checkout.shippingMatrix", &shipping)

// handles
client.Key("main.lockport").OnChange(func(e appswitch.Event) {
    log.Printf("port %v -> %v", e.Previous, e.Current)
})
client.Section("billing").OnChange(func(e appswitch.Event) {
    log.Printf("billing changed: %s", e.Path)
})
```

## Surface

| Concept | Method |
|---|---|
| Construct | `appswitch.New(cfg)` |
| Lifecycle | `Start(ctx)`, `Stop()`, `<-Ready()` |
| Read | `Get(path, fallback...)`, `Raw(path, fallback...)` |
| Typed read | `Number` · `String` · `Bool` · `URL` · `Datetime` · `Interval` · `Semver` · `SemverObject` · `ArrayString` · `ArrayNumber` · `Enum` · `JSON(path, &out)` |
| Semver helpers | `ParseSemver`, `CompareSemver`, `IsSemver` — package-level; `(*Semver).Compare`, `.Lt`/`.Gt`, `.Bump` |
| Snapshot | `Snapshot()`, `LastFetch()` |
| Refresh | `Refresh(ctx)` |
| Handles | `Key(path)` → `*AppSwitchKey`, `Section(prefix)` → `*AppSwitchSection` |
| Hooks | `OnChange`, `OnSection`, `OnNew`, `OnModify`, `OnRemove` |

Typed accessors return `(T, error)`. A trailing variadic `fallback` is returned for availability
errors (not ready / not found / network / stale); type mismatches always error. Inspect errors with
`appswitch.CodeOf(err)` or `errors.As(err, &ae)` for an `*appswitch.Error`.

## Config

| Field | Type | Default |
|---|---|---|
| `Name` / `Version` / `APIKey` | string | **required** |
| `Endpoint` | string | `https://api.appswitch.example.com` |
| `CacheTTL` | `time.Duration` | `0` = unlimited |
| `PollInterval` | `time.Duration` | `0` → 5m; negative → disabled |
| `MaxStaleness` | `time.Duration` | `0` = infinite |
| `NegativeCacheTTL` | `time.Duration` | 5m |
| `TelemetryFlushInterval` | `time.Duration` | `0` → 60s; negative → disabled |
| `RequestTimeout` | `time.Duration` | 5s |
| `CacheFolder` / `DisableDiskCache` | string / bool | OS cache dir; `DisableDiskCache` for in-memory only |
| `StaleIfError` | `*bool` (use `appswitch.Bool`) | `true` |
| `UserAgent` | string | `appswitch-go/<sdk> <name>/<version>` |
| `OnError` | `func(error)` | no-op |

The client is safe for concurrent use and verified with `go test -race`.
