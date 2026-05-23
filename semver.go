package appswitch

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SemverRE is the SemVer 2.0.0 canonical-form regex (docs/semver.md §2).
// MUST stay in sync with packages/core/src/semver/regex.ts and the OpenAPI
// pattern in docs/openapi.yaml.
var SemverRE = regexp.MustCompile(
	`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
		`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`,
)

// IsSemver reports whether s is a SemVer 2.0.0 canonical string.
func IsSemver(s string) bool { return SemverRE.MatchString(s) }

// Semver is a parsed SemVer 2.0.0 version with precedence-comparison helpers.
// The original canonical string is preserved verbatim (build metadata included).
type Semver struct {
	Value      string
	Major      int
	Minor      int
	Patch      int
	Prerelease []prereleaseIdent
	Build      []string
}

// prereleaseIdent represents one pre-release identifier — either a number
// (Numeric=true) or an alphanumeric string. Numeric identifiers have lower
// precedence than alphanumeric ones (SemVer §11.4.3).
type prereleaseIdent struct {
	Numeric bool
	Num     uint64
	Str     string
}

// ParseSemver parses s into a Semver. Returns nil + error if s is not a
// SemVer 2.0.0 canonical string.
func ParseSemver(s string) (*Semver, error) {
	m := SemverRE.FindStringSubmatch(s)
	if m == nil {
		return nil, newError(CodeTypeMismatch, "invalid semver \""+s+"\"")
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])

	var pre []prereleaseIdent
	if m[4] != "" {
		for _, id := range strings.Split(m[4], ".") {
			pre = append(pre, parsePrereleaseIdent(id))
		}
	}
	var build []string
	if m[5] != "" {
		build = strings.Split(m[5], ".")
	}
	return &Semver{
		Value:      s,
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: pre,
		Build:      build,
	}, nil
}

// MustParseSemver is the panic-on-error variant of ParseSemver. Intended for
// package-level constants in callers that own the input.
func MustParseSemver(s string) *Semver {
	v, err := ParseSemver(s)
	if err != nil {
		panic(err)
	}
	return v
}

func parsePrereleaseIdent(id string) prereleaseIdent {
	if n, err := strconv.ParseUint(id, 10, 64); err == nil && !strings.HasPrefix(id, "0x") {
		// numeric identifier (the regex already forbids leading zeros)
		return prereleaseIdent{Numeric: true, Num: n}
	}
	return prereleaseIdent{Str: id}
}

// String returns the canonical semver string.
func (v *Semver) String() string { return v.Value }

// Compare returns -1, 0, or 1 per SemVer 2.0.0 §11. Build metadata is IGNORED.
func (v *Semver) Compare(other *Semver) int {
	if c := cmpInt(v.Major, other.Major); c != 0 {
		return c
	}
	if c := cmpInt(v.Minor, other.Minor); c != 0 {
		return c
	}
	if c := cmpInt(v.Patch, other.Patch); c != 0 {
		return c
	}
	return cmpPrerelease(v.Prerelease, other.Prerelease)
}

// CompareString is Compare against a raw string. Returns an error if other
// is not a valid semver.
func (v *Semver) CompareString(other string) (int, error) {
	o, err := ParseSemver(other)
	if err != nil {
		return 0, err
	}
	return v.Compare(o), nil
}

// Eq, Lt, Lte, Gt, Gte are convenience predicates against another Semver.
func (v *Semver) Eq(o *Semver) bool  { return v.Compare(o) == 0 }
func (v *Semver) Lt(o *Semver) bool  { return v.Compare(o) < 0 }
func (v *Semver) Lte(o *Semver) bool { return v.Compare(o) <= 0 }
func (v *Semver) Gt(o *Semver) bool  { return v.Compare(o) > 0 }
func (v *Semver) Gte(o *Semver) bool { return v.Compare(o) >= 0 }

// Bump increments one field per SemVer §6/§7/§8, clearing pre-release/build.
// field must be "major", "minor", or "patch".
func (v *Semver) Bump(field string) (*Semver, error) {
	switch field {
	case "major":
		return ParseSemver(fmt.Sprintf("%d.0.0", v.Major+1))
	case "minor":
		return ParseSemver(fmt.Sprintf("%d.%d.0", v.Major, v.Minor+1))
	case "patch":
		return ParseSemver(fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch+1))
	default:
		return nil, newError(CodeTypeMismatch, "bump field must be major|minor|patch, got "+field)
	}
}

// CompareSemver is the package-level equivalent of (*Semver).Compare for raw
// strings. Returns an error if either input is invalid.
func CompareSemver(a, b string) (int, error) {
	pa, err := ParseSemver(a)
	if err != nil {
		return 0, err
	}
	pb, err := ParseSemver(b)
	if err != nil {
		return 0, err
	}
	return pa.Compare(pb), nil
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpPrerelease(a, b []prereleaseIdent) int {
	// SemVer §11.3: pre-release versions have lower precedence than the
	// associated normal version.
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if c := cmpIdent(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a), len(b))
}

func cmpIdent(a, b prereleaseIdent) int {
	switch {
	case a.Numeric && b.Numeric:
		return cmpUint(a.Num, b.Num)
	case a.Numeric:
		return -1
	case b.Numeric:
		return 1
	}
	return cmpStr(a.Str, b.Str)
}
