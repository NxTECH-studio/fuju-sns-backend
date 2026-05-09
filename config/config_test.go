package config

import (
	"net/http"
	"testing"
	"time"
)

const (
	sameSiteNone   = "None"
	envDevelopment = "development"
)

// base returns a Config with the minimum fields populated for a
// SessionCookie-focused test to pass Validate. Repo backend is left
// implicit (empty repoBackendRaw + Environment=development falls back
// to inmemory, which skips DB_* checks).
func base() *Config {
	return &Config{
		Environment:                 envDevelopment,
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

func TestRepoBackend_explicit_overrides_environment(t *testing.T) {
	cases := []struct {
		raw         string
		environment string
		want        string
	}{
		{raw: RepoBackendInMemory, environment: "production", want: RepoBackendInMemory},
		{raw: RepoBackendPostgres, environment: envDevelopment, want: RepoBackendPostgres},
	}
	for _, tc := range cases {
		c := &Config{repoBackendRaw: tc.raw, Environment: tc.environment}
		if got := c.RepoBackend(); got != tc.want {
			t.Errorf("raw=%q env=%q: want %s, got %s", tc.raw, tc.environment, tc.want, got)
		}
	}
}

func TestRepoBackend_auto_picks_by_environment(t *testing.T) {
	cases := []struct {
		environment string
		want        string
	}{
		{environment: envDevelopment, want: RepoBackendInMemory},
		{environment: "staging", want: RepoBackendPostgres},
		{environment: "production", want: RepoBackendPostgres},
		{environment: "", want: RepoBackendPostgres},
	}
	for _, tc := range cases {
		c := &Config{Environment: tc.environment}
		if got := c.RepoBackend(); got != tc.want {
			t.Errorf("env=%q: want %s, got %s", tc.environment, tc.want, got)
		}
	}
}

func TestValidate_inmemory_skips_db_checks(t *testing.T) {
	c := &Config{
		Environment:                 envDevelopment,
		repoBackendRaw:              RepoBackendInMemory,
		AuthCoreBaseURL:             "http://localhost",
		AuthCoreClientID:            "x",
		AuthCoreClientSecret:        "y",
		SessionCookieSameSite:       "Lax",
		SessionCookieSecure:         true,
		SessionCookieFallbackMaxAge: time.Hour,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("inmemory validate should pass without DB_*: %v", err)
	}
}

func TestValidate_postgres_requires_db_fields_when_no_url(t *testing.T) {
	c := &Config{
		Environment:                 "production",
		repoBackendRaw:              RepoBackendPostgres,
		AuthCoreBaseURL:             "http://localhost",
		AuthCoreClientID:            "x",
		AuthCoreClientSecret:        "y",
		SessionCookieSameSite:       "Lax",
		SessionCookieSecure:         true,
		SessionCookieFallbackMaxAge: time.Hour,
	}
	if err := c.Validate(); err == nil {
		t.Fatalf("postgres without DB_* or DATABASE_URL should fail")
	}
}

func TestValidate_postgres_accepts_database_url_alone(t *testing.T) {
	c := &Config{
		Environment:                 "production",
		repoBackendRaw:              RepoBackendPostgres,
		DatabaseURL:                 "postgres://u:p@h/d",
		AuthCoreBaseURL:             "http://localhost",
		AuthCoreClientID:            "x",
		AuthCoreClientSecret:        "y",
		SessionCookieSameSite:       "Lax",
		SessionCookieSecure:         true,
		SessionCookieFallbackMaxAge: time.Hour,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("DATABASE_URL alone should satisfy postgres: %v", err)
	}
}

func TestValidate_rejects_non_postgres_database_url_scheme(t *testing.T) {
	for _, dsn := range []string{
		"mysql://u:p@h/d",
		"file:///etc/passwd",
		"postgres.invalid://u:p@h/d",
		"http://u:p@h/d",
	} {
		c := &Config{
			Environment:                 "production",
			repoBackendRaw:              RepoBackendPostgres,
			DatabaseURL:                 dsn,
			AuthCoreBaseURL:             "http://localhost",
			AuthCoreClientID:            "x",
			AuthCoreClientSecret:        "y",
			SessionCookieSameSite:       "Lax",
			SessionCookieSecure:         true,
			SessionCookieFallbackMaxAge: time.Hour,
		}
		if err := c.Validate(); err == nil {
			t.Errorf("DATABASE_URL=%q should fail validation", dsn)
		}
	}
}

func TestValidate_accepts_postgresql_url_scheme(t *testing.T) {
	c := &Config{
		Environment:                 "production",
		repoBackendRaw:              RepoBackendPostgres,
		DatabaseURL:                 "postgresql://u:p@h/d",
		AuthCoreBaseURL:             "http://localhost",
		AuthCoreClientID:            "x",
		AuthCoreClientSecret:        "y",
		SessionCookieSameSite:       "Lax",
		SessionCookieSecure:         true,
		SessionCookieFallbackMaxAge: time.Hour,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("postgresql:// scheme should pass: %v", err)
	}
}

func TestValidate_rejects_unknown_repo_backend(t *testing.T) {
	c := &Config{
		Environment:                 envDevelopment,
		repoBackendRaw:              "redis",
		AuthCoreBaseURL:             "http://localhost",
		AuthCoreClientID:            "x",
		AuthCoreClientSecret:        "y",
		SessionCookieSameSite:       "Lax",
		SessionCookieSecure:         true,
		SessionCookieFallbackMaxAge: time.Hour,
	}
	if err := c.Validate(); err == nil {
		t.Fatalf("unknown REPO_BACKEND value should fail")
	}
}

func TestDSN_prefers_database_url(t *testing.T) {
	c := &Config{DatabaseURL: "postgres://custom"}
	if got := c.DSN(); got != "postgres://custom" {
		t.Errorf("expected raw DATABASE_URL, got %s", got)
	}
}

func TestDSN_builds_from_fields(t *testing.T) {
	c := &Config{
		DBHost:     "db.internal",
		DBPort:     5432,
		DBName:     "fuju",
		DBUser:     "fuju_user",
		DBPassword: "p@ss:word", // percent-escape target
	}
	got := c.DSN()
	want := "postgres://fuju_user:p%40ss%3Aword@db.internal:5432/fuju?sslmode=disable"
	if got != want {
		t.Errorf("DSN mismatch:\n  want %s\n  got  %s", want, got)
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
	c.Environment = envDevelopment
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

// withR2 fully populates the five R2 fields on the base() config.
func withR2(c *Config) *Config {
	c.R2Endpoint = "https://example.r2.cloudflarestorage.com"
	c.R2BucketName = "fuju-images"
	c.R2PublicDomain = "https://images.fuju.example.com"
	c.R2AccessKeyID = "ak"
	c.R2SecretAccessKey = "sk"
	return c
}

func TestR2Enabled_allFiveSet(t *testing.T) {
	c := withR2(base())
	if !c.R2Enabled() {
		t.Errorf("expected R2Enabled=true with all five fields set")
	}
}

func TestR2Enabled_allEmpty(t *testing.T) {
	c := base()
	if c.R2Enabled() {
		t.Errorf("expected R2Enabled=false with no R2 fields set")
	}
}

func TestR2Enabled_partialIsFalse(t *testing.T) {
	// Each pop-out of one field individually should report disabled.
	// (The same partial state is rejected by Validate, so callers will
	// not actually reach R2Enabled() in that shape — but the predicate
	// itself must remain conservative.)
	mutators := []func(*Config){
		func(c *Config) { c.R2Endpoint = "" },
		func(c *Config) { c.R2BucketName = "" },
		func(c *Config) { c.R2PublicDomain = "" },
		func(c *Config) { c.R2AccessKeyID = "" },
		func(c *Config) { c.R2SecretAccessKey = "" },
	}
	for i, m := range mutators {
		c := withR2(base())
		m(c)
		if c.R2Enabled() {
			t.Errorf("case %d: expected R2Enabled=false when one field is empty", i)
		}
	}
}

func TestValidate_acceptsAllFiveR2Fields(t *testing.T) {
	c := withR2(base())
	if err := c.Validate(); err != nil {
		t.Fatalf("fully-populated R2 config should pass Validate, got %v", err)
	}
}

func TestValidate_acceptsZeroR2Fields(t *testing.T) {
	// All five empty is the legitimate "image upload disabled" state.
	c := base()
	if err := c.Validate(); err != nil {
		t.Fatalf("empty R2 config should pass Validate, got %v", err)
	}
}

func TestValidate_rejectsPartialR2Config(t *testing.T) {
	// Drop each field one at a time from a fully-populated config; each
	// case must fail validation rather than silently disable uploads.
	mutators := []struct {
		name string
		mut  func(*Config)
	}{
		{"missing endpoint", func(c *Config) { c.R2Endpoint = "" }},
		{"missing bucket", func(c *Config) { c.R2BucketName = "" }},
		{"missing public domain", func(c *Config) { c.R2PublicDomain = "" }},
		{"missing access key id", func(c *Config) { c.R2AccessKeyID = "" }},
		{"missing secret access key", func(c *Config) { c.R2SecretAccessKey = "" }},
	}
	for _, tc := range mutators {
		t.Run(tc.name, func(t *testing.T) {
			c := withR2(base())
			tc.mut(c)
			if err := c.Validate(); err == nil {
				t.Errorf("expected partial R2 config to be rejected (%s)", tc.name)
			}
		})
	}
}
