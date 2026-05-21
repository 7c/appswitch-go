package appswitch

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type snapshot map[string]ResolvedKey

func toSnapshot(keys []ResolvedKey) snapshot {
	s := make(snapshot, len(keys))
	for _, k := range keys {
		s[k.Path] = k
	}
	return s
}

// sectionOf returns everything before the last dot ("a.b.c" → "a.b").
func sectionOf(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i]
	}
	return ""
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func shapeChanged(a, b ResolvedKey) bool {
	return !jsonEqual(a.Value, b.Value) ||
		a.Type != b.Type ||
		a.Disabled != b.Disabled ||
		a.StringMode != b.StringMode ||
		!jsonEqual(a.EnumOptions, b.EnumOptions)
}

func makeEvent(kind EventKind, path string, before, after *ResolvedKey, at time.Time) Event {
	ref := after
	if ref == nil {
		ref = before
	}
	ev := Event{
		Kind:         kind,
		Path:         path,
		Section:      sectionOf(path),
		Type:         ref.Type,
		IsSecret:     ref.Secret,
		IsDisabled:   ref.Disabled,
		IsDeprecated: ref.Deprecated,
		At:           at,
	}
	if before != nil {
		ev.Previous = before.Value
	}
	if after != nil {
		ev.Current = after.Value
	}
	return ev
}

// diffSnapshots produces change events (CLIENT.md §8) in lexicographic path
// order. A deprecation flip yields deprecated/reactivated, not modified.
func diffSnapshots(prev, next snapshot, at time.Time) []Event {
	seen := make(map[string]struct{}, len(prev)+len(next))
	paths := make([]string, 0, len(prev)+len(next))
	for p := range prev {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			paths = append(paths, p)
		}
	}
	for p := range next {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	events := make([]Event, 0)
	for _, path := range paths {
		b, hasBefore := prev[path]
		a, hasAfter := next[path]
		switch {
		case hasBefore && !hasAfter:
			events = append(events, makeEvent(EventRemoved, path, &b, nil, at))
		case !hasBefore && hasAfter:
			events = append(events, makeEvent(EventNew, path, nil, &a, at))
		case hasBefore && hasAfter:
			switch {
			case !b.Deprecated && a.Deprecated:
				events = append(events, makeEvent(EventDeprecated, path, &b, &a, at))
			case b.Deprecated && !a.Deprecated:
				events = append(events, makeEvent(EventReactivated, path, &b, &a, at))
			case shapeChanged(b, a):
				events = append(events, makeEvent(EventModified, path, &b, &a, at))
			}
		}
	}
	return events
}

// matchesSection reports whether path falls under a section selector. A bare
// prefix matches itself and descendants; a trailing ".*"/"*" matches descendants.
func matchesSection(selector, path string) bool {
	base := selector
	switch {
	case strings.HasSuffix(selector, ".*"):
		base = selector[:len(selector)-2]
	case strings.HasSuffix(selector, "*"):
		base = strings.TrimSuffix(selector[:len(selector)-1], ".")
	}
	if base == "" {
		return true
	}
	return path == base || strings.HasPrefix(path, base+".")
}
