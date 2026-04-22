package config

import (
	"net/http"
	"testing"
	"time"
)

const sameSiteNone = "None"

func base() *Config {
	return &Config{
		Environment:                 "development",
		DBHost:                      "localhost",
		DBName:                      "fuju",
		DBUser:                      "u",
		DBPassword:                  "p",
		AuthCoreBaseURL:             "http://authcore",
		AuthCoreClientID:            "cid",
		AuthCoreClientSecret:        "csec",
		SessionCookieName:           "fuju_access",
		SessionCookieSecure:         true,
		SessionCookieSameSite:       "Lax",
		SessionCookieFallbackMaxAge: time.Hour,
	}
}

func TestValidate_passesWithDefaults(t *testing.T) {
	if err := base().Validate(); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestValidate_rejectsUnknownSameSite(t *testing.T) {
	c := base()
	c.SessionCookieSameSite = "Loose"
	if err := c.Validate(); err == nil {
		t.Fatalf("expected error for bogus SameSite")
	}
}

func TestValidate_rejectsNoneWithoutSecure(t *testing.T) {
	// Browsers silently drop SameSite=None cookies that lack Secure,
	// so we must fail boot rather than wait for runtime symptoms.
	c := base()
	c.SessionCookieSameSite = sameSiteNone
	c.SessionCookieSecure = false
	if err := c.Validate(); err == nil {
		t.Fatalf("expected SameSite=None + Secure=false to be rejected")
	}
}

func TestValidate_acceptsNoneWithSecure(t *testing.T) {
	c := base()
	c.SessionCookieSameSite = sameSiteNone
	c.SessionCookieSecure = true
	if err := c.Validate(); err != nil {
		t.Fatalf("SameSite=None + Secure=true must pass, got %v", err)
	}
}

func TestValidate_rejectsSecureFalseOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"staging", "production", ""} {
		c := base()
		c.Environment = env
		c.SessionCookieSecure = false
		if err := c.Validate(); err == nil {
			t.Errorf("Secure=false must be rejected for Environment=%q", env)
		}
	}
}

func TestValidate_acceptsSecureFalseInDevelopment(t *testing.T) {
	c := base()
	c.Environment = "development"
	c.SessionCookieSecure = false
	if err := c.Validate(); err != nil {
		t.Fatalf("Secure=false + Environment=development must pass, got %v", err)
	}
}

func TestValidate_rejectsZeroFallbackMaxAge(t *testing.T) {
	c := base()
	c.SessionCookieFallbackMaxAge = 0
	if err := c.Validate(); err == nil {
		t.Fatalf("FallbackMaxAge=0 must be rejected (would emit unbounded cookie)")
	}
	c.SessionCookieFallbackMaxAge = -1 * time.Second
	if err := c.Validate(); err == nil {
		t.Fatalf("negative FallbackMaxAge must be rejected")
	}
}

func TestValidate_cachesParsedSameSite(t *testing.T) {
	cases := []struct {
		in   string
		want http.SameSite
	}{
		{"Lax", http.SameSiteLaxMode},
		{"Strict", http.SameSiteStrictMode},
		{sameSiteNone, http.SameSiteNoneMode},
	}
	for _, tc := range cases {
		c := base()
		c.SessionCookieSameSite = tc.in
		if tc.in == sameSiteNone {
			c.SessionCookieSecure = true
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: unexpected error %v", tc.in, err)
		}
		if got := c.SessionCookieSameSiteMode(); got != tc.want {
			t.Errorf("%s: cached mode %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestGetEnvBool_knownTrueValues(t *testing.T) {
	for _, v := range []string{"true", "1", "yes", "on", "TRUE", "Yes"} {
		t.Setenv("TEST_BOOL", v)
		if !getEnvBool("TEST_BOOL", false) {
			t.Errorf("value %q should parse as true", v)
		}
	}
}

func TestGetEnvBool_knownFalseValues(t *testing.T) {
	for _, v := range []string{"false", "0", "no", "off", "False"} {
		t.Setenv("TEST_BOOL", v)
		if getEnvBool("TEST_BOOL", true) {
			t.Errorf("value %q should parse as false", v)
		}
	}
}

func TestGetEnvBool_unsetReturnsDefault(t *testing.T) {
	t.Setenv("TEST_BOOL_UNSET", "")
	if got := getEnvBool("TEST_BOOL_UNSET", true); !got {
		t.Errorf("empty value should return default")
	}
}

func TestGetEnvBool_invalidReturnsDefault(t *testing.T) {
	t.Setenv("TEST_BOOL", "maybe")
	// Fail-soft on unknown values: default wins so a typo doesn't
	// silently flip production security posture.
	if got := getEnvBool("TEST_BOOL", true); !got {
		t.Errorf("unknown value should fall back to default")
	}
}
