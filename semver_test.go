package appswitch_test

import (
	"context"
	"testing"

	appswitch "github.com/7c/appswitch-go"
)

func TestIsSemver(t *testing.T) {
	good := []string{
		"0.0.0", "1.2.3", "1.2.3-rc.1", "1.2.3-rc.1+sha.abc", "1.2.3+build.42",
	}
	bad := []string{
		"1.2", "1.2.3.4", "01.2.3", "1.2.3-01", "1.2.3-", "v1.2.3", "", "latest",
	}
	for _, s := range good {
		if !appswitch.IsSemver(s) {
			t.Errorf("IsSemver(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if appswitch.IsSemver(s) {
			t.Errorf("IsSemver(%q) = true, want false", s)
		}
	}
}

func TestParseSemverRoundtrip(t *testing.T) {
	for _, s := range []string{"1.2.3", "1.2.3-rc.1", "1.2.3+build.42", "1.2.3-rc.1+sha.abc"} {
		v, err := appswitch.ParseSemver(s)
		if err != nil {
			t.Fatalf("ParseSemver(%q): %v", s, err)
		}
		if v.String() != s {
			t.Errorf("roundtrip %q != %q", v.String(), s)
		}
	}
}

func TestParseSemverInvalid(t *testing.T) {
	if _, err := appswitch.ParseSemver("v1.2.3"); err == nil {
		t.Fatal("expected error for v1.2.3")
	}
}

func TestCompareSemverPrecedence(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// SemVer 2.0.0 §11 example 11
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "2.1.0", -1},
		{"2.1.0", "2.1.1", -1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-alpha.beta", "1.0.0-beta", -1},
		{"1.0.0-beta", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-beta.11", "1.0.0-rc.1", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		// build metadata ignored
		{"1.0.0+sha.a", "1.0.0+sha.b", 0},
		{"1.0.0+a", "1.0.0", 0},
		// equality
		{"1.2.3", "1.2.3", 0},
	}
	for _, c := range cases {
		got, err := appswitch.CompareSemver(c.a, c.b)
		if err != nil {
			t.Fatalf("CompareSemver(%q,%q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("CompareSemver(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
		rev, _ := appswitch.CompareSemver(c.b, c.a)
		if rev != -c.want {
			t.Errorf("CompareSemver(%q,%q) = %d, want %d (reverse)", c.b, c.a, rev, -c.want)
		}
	}
}

func TestSemverBump(t *testing.T) {
	v := appswitch.MustParseSemver("1.2.3-rc.1+build.42")
	if next, _ := v.Bump("patch"); next.String() != "1.2.4" {
		t.Errorf("Bump patch = %v", next)
	}
	if next, _ := v.Bump("minor"); next.String() != "1.3.0" {
		t.Errorf("Bump minor = %v", next)
	}
	if next, _ := v.Bump("major"); next.String() != "2.0.0" {
		t.Errorf("Bump major = %v", next)
	}
	if _, err := v.Bump("invalid"); err == nil {
		t.Error("expected error for invalid bump field")
	}
}

func TestSemverPredicates(t *testing.T) {
	a := appswitch.MustParseSemver("1.2.3")
	b := appswitch.MustParseSemver("1.2.4")
	if !a.Lt(b) || b.Lte(a) || !b.Gt(a) || a.Gte(b) {
		t.Fatal("predicate mismatch")
	}
	if !a.Eq(appswitch.MustParseSemver("1.2.3+build.99")) {
		t.Fatal("build metadata should not affect equality")
	}
}

func TestSemverClientAccessor(t *testing.T) {
	m := newMockServer([]appswitch.ResolvedKey{
		key("mobile.minSupportedVersion", appswitch.TypeSemver, "3.4.0"),
		key("main.lockport", appswitch.TypeNumber, 8443),
	})
	defer m.Close()
	c, _ := appswitch.New(baseConfig(m.URL()))
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	if s, err := c.Semver("mobile.minSupportedVersion"); err != nil || s != "3.4.0" {
		t.Fatalf("Semver = %q err=%v", s, err)
	}
	v, err := c.SemverObject("mobile.minSupportedVersion")
	if err != nil {
		t.Fatal(err)
	}
	if v.Major != 3 || v.Minor != 4 || v.Patch != 0 {
		t.Fatalf("parsed = %+v", v)
	}
	if !v.Gt(appswitch.MustParseSemver("3.3.99")) {
		t.Error("expected 3.4.0 > 3.3.99")
	}

	// type mismatch
	if _, err := c.Semver("main.lockport"); appswitch.CodeOf(err) != appswitch.CodeTypeMismatch {
		t.Fatalf("expected TYPE_MISMATCH, got %v", err)
	}

	// fallback path
	if s, _ := c.Semver("missing.key", "0.0.0"); s != "0.0.0" {
		t.Fatalf("fallback = %q", s)
	}

	// key handle
	if s, err := c.Key("mobile.minSupportedVersion").Semver(); err != nil || s != "3.4.0" {
		t.Fatalf("handle Semver = %q err=%v", s, err)
	}
}
