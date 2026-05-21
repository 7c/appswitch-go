// Package appswitch is the official Go client SDK for appswitch — a typed,
// cached, read-only configuration client. See docs/CLIENT.md for the concept.
//
// Clients are read-only: no project/user management, no writes, no realtime push.
// After Start, reads are answered synchronously from an in-memory snapshot.
package appswitch

import "time"

// StageID is one of the three deployment stages. The stage is implied by the
// API key; clients never name it.
type StageID string

const (
	StageDev     StageID = "dev"
	StageStaging StageID = "staging"
	StageProd    StageID = "prod"
)

// DataType is the declared type of a config key (CONCEPT §2).
type DataType string

const (
	TypeNumber      DataType = "number"
	TypeString      DataType = "string"
	TypeURL         DataType = "url"
	TypeBoolean     DataType = "boolean"
	TypeDatetime    DataType = "datetime"
	TypeInterval    DataType = "interval"
	TypeArrayString DataType = "array<string>"
	TypeArrayNumber DataType = "array<number>"
	TypeEnumString  DataType = "enum<string>"
	TypeJSON        DataType = "json"
	TypeLink        DataType = "link"
)

// ResolvedKey is a key resolved for the API key's stage (server ResolvedKey).
type ResolvedKey struct {
	Path         string   `json:"path"`
	Type         DataType `json:"type"`
	Value        any      `json:"value"`
	ResolvedFrom StageID  `json:"resolvedFrom"`
	Explicit     bool     `json:"explicit"`
	Disabled     bool     `json:"disabled"`
	Deprecated   bool     `json:"deprecated"`
	StringMode   string   `json:"stringMode,omitempty"`
	EnumOptions  []string `json:"enumOptions,omitempty"`
	// Secret is a UI hint not currently exposed by /v1; treated as false when absent.
	Secret bool `json:"secret,omitempty"`
}

// keysResponse is the GET /v1/keys body.
type keysResponse struct {
	Stage   StageID       `json:"stage"`
	Project string        `json:"project"`
	Keys    []ResolvedKey `json:"keys"`
}

// EventKind classifies a change between snapshots (CLIENT.md §8).
type EventKind string

const (
	EventNew         EventKind = "new"
	EventModified    EventKind = "modified"
	EventRemoved     EventKind = "removed"
	EventDeprecated  EventKind = "deprecated"
	EventReactivated EventKind = "reactivated"
)

// Event describes one key change, delivered on the polling boundary.
type Event struct {
	Kind         EventKind
	Path         string
	Section      string
	Previous     any
	Current      any
	Type         DataType
	IsSecret     bool
	IsDisabled   bool
	IsDeprecated bool
	At           time.Time
}

// FetchMetadata describes the most recent successful fetch.
type FetchMetadata struct {
	ETag      string
	FetchedAt time.Time
	Stage     StageID
	Project   string
	FromCache bool
}

// Listener receives change events. Unsubscribe cancels a subscription.
type (
	Listener    func(Event)
	Unsubscribe func()
)
