package cookie

import (
	"net/http"
	"testing"
	"time"
)

func TestBuild_defaultsPathToRoot(t *testing.T) {
	c := Build(Attrs{Name: "x", Value: "v"})
	if c.Path != "/" {
		t.Errorf("expected default Path=/, got %q", c.Path)
	}
}

func TestBuild_respectsExplicitPath(t *testing.T) {
	c := Build(Attrs{Name: "x", Value: "v", Path: "/api"})
	if c.Path != "/api" {
		t.Errorf("expected explicit Path=/api, got %q", c.Path)
	}
}

func TestBuild_setsHttpOnly(t *testing.T) {
	c := Build(Attrs{Name: "x", Value: "v"})
	if !c.HttpOnly {
		t.Errorf("cookies from this builder must always be HttpOnly")
	}
}

func TestBuild_maxAgePositiveSecondsRounding(t *testing.T) {
	c := Build(Attrs{Name: "x", MaxAge: 90 * time.Minute})
	if c.MaxAge != 5400 {
		t.Errorf("expected 90m → 5400s, got %d", c.MaxAge)
	}
}

func TestBuild_maxAgeZeroEmitsSessionCookie(t *testing.T) {
	c := Build(Attrs{Name: "x"})
	if c.MaxAge != 0 {
		t.Errorf("expected MaxAge=0 (session cookie), got %d", c.MaxAge)
	}
}

func TestBuild_negativeMaxAgeNormalizedToMinusOne(t *testing.T) {
	c := Build(Attrs{Name: "x", MaxAge: -5 * time.Hour})
	if c.MaxAge != -1 {
		t.Errorf("expected MaxAge=-1 (delete), got %d", c.MaxAge)
	}
}

func TestBuild_passesThroughAttrs(t *testing.T) {
	c := Build(Attrs{
		Name:     "fuju_access",
		Value:    "abc",
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Domain:   ".fuju.example",
	})
	if c.Name != "fuju_access" || c.Value != "abc" {
		t.Errorf("name/value mismatch: %+v", c)
	}
	if !c.Secure {
		t.Errorf("expected Secure=true")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite mismatch: %v", c.SameSite)
	}
	if c.Domain != ".fuju.example" {
		t.Errorf("Domain mismatch: %q", c.Domain)
	}
}

func TestParseSameSite_known(t *testing.T) {
	cases := []struct {
		in   string
		want http.SameSite
	}{
		{"Lax", http.SameSiteLaxMode},
		{"Strict", http.SameSiteStrictMode},
		{"None", http.SameSiteNoneMode},
	}
	for _, tc := range cases {
		got, err := ParseSameSite(tc.in)
		if err != nil {
			t.Errorf("ParseSameSite(%q) returned err: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseSameSite(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseSameSite_unknownReturnsError(t *testing.T) {
	_, err := ParseSameSite("Loose")
	if err == nil {
		t.Errorf("expected error for unknown SameSite value")
	}
}

func TestParseSameSite_caseSensitive(t *testing.T) {
	// The config documentation capitalizes the values and we want
	// case-sensitive matching to prevent accidental typos like "LAX"
	// slipping through.
	if _, err := ParseSameSite("lax"); err == nil {
		t.Errorf("lowercase should be rejected to avoid ambiguity")
	}
}
