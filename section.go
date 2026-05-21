package appswitch

// AppSwitchSection is a handle to a dotted section / glob selector
// (client.Section("features.checkout") or client.Section("billing.*")).
type AppSwitchSection struct {
	c        *AppSwitch
	Selector string
}

// Keys returns the paths currently under this section.
func (s *AppSwitchSection) Keys() []string {
	out := make([]string, 0)
	for _, p := range s.c.paths() {
		if matchesSection(s.Selector, p) {
			out = append(out, p)
		}
	}
	return out
}

// Values returns current values keyed by path.
func (s *AppSwitchSection) Values() map[string]any {
	out := make(map[string]any)
	for _, p := range s.Keys() {
		if k, ok := s.c.lookup(p); ok {
			out[p] = k.Value
		}
	}
	return out
}

// OnChange subscribes to any change under this section.
func (s *AppSwitchSection) OnChange(fn Listener) Unsubscribe {
	return s.c.subscribe(func(e Event) bool { return matchesSection(s.Selector, e.Path) }, fn)
}

// On subscribes to a single kind under this section.
func (s *AppSwitchSection) On(kind EventKind, fn Listener) Unsubscribe {
	return s.c.subscribe(func(e Event) bool {
		return e.Kind == kind && matchesSection(s.Selector, e.Path)
	}, fn)
}
