package appswitch

import "time"

// AppSwitchKey is a handle to a single key (client.Key(path)). Typed accessors
// return an error on type mismatch; availability failures fall back when a
// fallback is supplied.
type AppSwitchKey struct {
	c    *AppSwitch
	Path string
}

func (k *AppSwitchKey) Get(fallback ...any) (any, error) { return k.c.Get(k.Path, fallback...) }
func (k *AppSwitchKey) Raw(fallback ...any) (any, error) { return k.c.Raw(k.Path, fallback...) }
func (k *AppSwitchKey) Number(fallback ...float64) (float64, error) {
	return k.c.Number(k.Path, fallback...)
}
func (k *AppSwitchKey) String(fallback ...string) (string, error) {
	return k.c.String(k.Path, fallback...)
}
func (k *AppSwitchKey) Bool(fallback ...bool) (bool, error) { return k.c.Bool(k.Path, fallback...) }
func (k *AppSwitchKey) URL(fallback ...string) (string, error) {
	return k.c.URL(k.Path, fallback...)
}
func (k *AppSwitchKey) Datetime(fallback ...time.Time) (time.Time, error) {
	return k.c.Datetime(k.Path, fallback...)
}
func (k *AppSwitchKey) Interval(fallback ...time.Duration) (time.Duration, error) {
	return k.c.Interval(k.Path, fallback...)
}
func (k *AppSwitchKey) ArrayString(fallback ...[]string) ([]string, error) {
	return k.c.ArrayString(k.Path, fallback...)
}
func (k *AppSwitchKey) ArrayNumber(fallback ...[]float64) ([]float64, error) {
	return k.c.ArrayNumber(k.Path, fallback...)
}
func (k *AppSwitchKey) Enum(fallback ...string) (string, error) {
	return k.c.Enum(k.Path, fallback...)
}
func (k *AppSwitchKey) JSON(out any) error { return k.c.JSON(k.Path, out) }

// Exists reports whether the key is in the current snapshot.
func (k *AppSwitchKey) Exists() bool {
	_, ok := k.c.lookup(k.Path)
	return ok
}

func (k *AppSwitchKey) Type() DataType {
	rk, _ := k.c.lookup(k.Path)
	return rk.Type
}
func (k *AppSwitchKey) ResolvedFrom() StageID {
	rk, _ := k.c.lookup(k.Path)
	return rk.ResolvedFrom
}
func (k *AppSwitchKey) IsDisabled() bool {
	rk, _ := k.c.lookup(k.Path)
	return rk.Disabled
}
func (k *AppSwitchKey) IsDeprecated() bool {
	rk, _ := k.c.lookup(k.Path)
	return rk.Deprecated
}
func (k *AppSwitchKey) IsSecret() bool {
	rk, _ := k.c.lookup(k.Path)
	return rk.Secret
}

// OnChange subscribes to changes for this key (fires on the polling boundary).
func (k *AppSwitchKey) OnChange(fn Listener) Unsubscribe {
	return k.c.subscribe(func(e Event) bool { return e.Path == k.Path }, fn)
}
