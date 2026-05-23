package appswitch

import (
	"encoding/json"
	"time"
)

func mismatch(key ResolvedKey, expected string) *Error {
	return newError(CodeTypeMismatch, "key \""+key.Path+"\" is "+string(key.Type)+", expected "+expected)
}

func expectType(key ResolvedKey, t DataType, label string) error {
	if key.Type != t {
		return mismatch(key, label)
	}
	return nil
}

func asNumber(key ResolvedKey) (float64, error) {
	if err := expectType(key, TypeNumber, "number"); err != nil {
		return 0, err
	}
	v, ok := key.Value.(float64)
	if !ok {
		return 0, mismatch(key, "number")
	}
	return v, nil
}

func asString(key ResolvedKey) (string, error) {
	if err := expectType(key, TypeString, "string"); err != nil {
		return "", err
	}
	v, ok := key.Value.(string)
	if !ok {
		return "", mismatch(key, "string")
	}
	return v, nil
}

func asBool(key ResolvedKey) (bool, error) {
	if err := expectType(key, TypeBoolean, "boolean"); err != nil {
		return false, err
	}
	v, ok := key.Value.(bool)
	if !ok {
		return false, mismatch(key, "boolean")
	}
	return v, nil
}

func asURL(key ResolvedKey) (string, error) {
	if err := expectType(key, TypeURL, "url"); err != nil {
		return "", err
	}
	v, ok := key.Value.(string)
	if !ok {
		return "", mismatch(key, "url")
	}
	return v, nil
}

func asDatetime(key ResolvedKey) (time.Time, error) {
	if err := expectType(key, TypeDatetime, "datetime"); err != nil {
		return time.Time{}, err
	}
	s, ok := key.Value.(string)
	if !ok {
		return time.Time{}, mismatch(key, "datetime")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, mismatch(key, "datetime")
	}
	return t, nil
}

func asInterval(key ResolvedKey) (time.Duration, error) {
	if err := expectType(key, TypeInterval, "interval"); err != nil {
		return 0, err
	}
	v, ok := key.Value.(float64)
	if !ok {
		return 0, mismatch(key, "interval")
	}
	return time.Duration(int64(v)) * time.Millisecond, nil
}

func asArrayString(key ResolvedKey) ([]string, error) {
	if err := expectType(key, TypeArrayString, "array<string>"); err != nil {
		return nil, err
	}
	raw, ok := key.Value.([]any)
	if !ok {
		return nil, mismatch(key, "array<string>")
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, mismatch(key, "array<string>")
		}
		out[i] = s
	}
	return out, nil
}

func asArrayNumber(key ResolvedKey) ([]float64, error) {
	if err := expectType(key, TypeArrayNumber, "array<number>"); err != nil {
		return nil, err
	}
	raw, ok := key.Value.([]any)
	if !ok {
		return nil, mismatch(key, "array<number>")
	}
	out := make([]float64, len(raw))
	for i, item := range raw {
		n, ok := item.(float64)
		if !ok {
			return nil, mismatch(key, "array<number>")
		}
		out[i] = n
	}
	return out, nil
}

func asEnum(key ResolvedKey) (string, error) {
	if err := expectType(key, TypeEnumString, "enum<string>"); err != nil {
		return "", err
	}
	v, ok := key.Value.(string)
	if !ok {
		return "", mismatch(key, "enum<string>")
	}
	return v, nil
}

// asSemver returns the canonical semver string (already validated server-side).
func asSemver(key ResolvedKey) (string, error) {
	if err := expectType(key, TypeSemver, "semver"); err != nil {
		return "", err
	}
	v, ok := key.Value.(string)
	if !ok || !IsSemver(v) {
		return "", mismatch(key, "semver")
	}
	return v, nil
}

// asSemverObject returns a parsed *Semver with comparison helpers.
func asSemverObject(key ResolvedKey) (*Semver, error) {
	s, err := asSemver(key)
	if err != nil {
		return nil, err
	}
	return ParseSemver(s)
}

// asJSON decodes a json-typed key's value into out (CLIENT.md §7).
func asJSON(key ResolvedKey, out any) error {
	if err := expectType(key, TypeJSON, "json"); err != nil {
		return err
	}
	data, err := json.Marshal(key.Value)
	if err != nil {
		return mismatch(key, "json")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return wrapError(CodeTypeMismatch, "decode json key \""+key.Path+"\"", err)
	}
	return nil
}
