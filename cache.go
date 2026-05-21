package appswitch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type diskMeta struct {
	ETag      string    `json:"etag"`
	FetchedAt time.Time `json:"fetchedAt"`
}

type loadedCache struct {
	response  keysResponse
	etag      string
	fetchedAt time.Time
}

// diskCache is the SDK-owned on-disk snapshot (CLIENT.md §5): snapshot.json plus
// meta.json under a folder the SDK fully owns. All operations are best-effort —
// failures degrade to in-memory only and never propagate to the caller.
type diskCache struct {
	folder string // "" disables disk caching
}

func newDiskCache(folder string) *diskCache { return &diskCache{folder: folder} }

func (d *diskCache) enabled() bool { return d.folder != "" }

func (d *diskCache) snapshotFile() string { return filepath.Join(d.folder, "snapshot.json") }
func (d *diskCache) metaFile() string     { return filepath.Join(d.folder, "meta.json") }

func (d *diskCache) load() (*loadedCache, bool) {
	if !d.enabled() {
		return nil, false
	}
	snapData, err := os.ReadFile(d.snapshotFile())
	if err != nil {
		return nil, false
	}
	metaData, err := os.ReadFile(d.metaFile())
	if err != nil {
		return nil, false
	}
	var resp keysResponse
	if err := json.Unmarshal(snapData, &resp); err != nil {
		return nil, false
	}
	var meta diskMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, false
	}
	return &loadedCache{response: resp, etag: meta.ETag, fetchedAt: meta.FetchedAt}, true
}

func (d *diskCache) save(resp keysResponse, etag string, fetchedAt time.Time) {
	if !d.enabled() {
		return
	}
	if err := os.MkdirAll(d.folder, 0o700); err != nil {
		return
	}
	if snap, err := json.Marshal(resp); err == nil {
		_ = os.WriteFile(d.snapshotFile(), snap, 0o600)
	}
	if meta, err := json.Marshal(diskMeta{ETag: etag, FetchedAt: fetchedAt}); err == nil {
		_ = os.WriteFile(d.metaFile(), meta, 0o600)
	}
}
