// Package config provides configuration loading and management.
//
// Repository backend selection: REPO_BACKEND explicitly chooses
// "inmemory" or "postgres"; when unset, development environments
// default to inmemory and everything else (staging / production) to
// postgres. The DB_* fields are the authoritative connection source
// for the postgres backend; DATABASE_URL, when set, overrides them
// so cloud deployments can pass a single URL and container specs can
// still rely on DB_* individually.
package config

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Repository backend identifiers. Values are stable — cfg file / env
// var / docs all reference these constants.
const (
	RepoBackendInMemory = "inmemory"
	RepoBackendPostgres = "postgres"
)

// Config holds all application configuration.
type Config struct {
	// Server
	ServerPort  int
	Environment string

	// Database
	DBHost      string
	DBPort      int
	DBName      string
	DBUser      string
	DBPassword  string
	DatabaseURL string // optional: full DSN, overrides DB_* when set
	DBMaxConns  int32  // optional; 0 = pgx default
	DBMinConns  int32  // optional; 0 = pgx default

	// repoBackendRaw is the literal REPO_BACKEND env value. Callers
	// use the RepoBackend() method to resolve the effective backend;
	// keeping the raw string unexported forces that indirection.
	repoBackendRaw string

	// AuthCore
	AuthCoreBaseURL            string
	AuthCoreClientID           string
	AuthCoreClientSecret       string
	AuthCoreIntrospectPath     string
	AuthCoreProfilePath        string
	AuthCoreProfileTTL         time.Duration
	AuthCoreIntrospectCacheTTL time.Duration

	// Session cookie (handoff flow: POST /v1/auth/session → Set-Cookie:
	// fuju_access=...). The cookie value is the AuthCore access token
	// verbatim; no server-side session store is involved. See
	// docs/tasks/10-cookie-session-handoff.md.
	SessionCookieName           string        // default "fuju_access"
	SessionCookieSecure         bool          // default true; dev may set false
	SessionCookieSameSite       string        // "Lax" | "Strict" | "None"; default "Lax"
	SessionCookieDomain         string        // optional; empty = host-only cookie
	SessionCookieFallbackMaxAge time.Duration // default 1h; used when the access token lacks an `exp` claim

	// sessionCookieSameSiteMode caches the parsed form of
	// SessionCookieSameSite. Populated by Validate so wiring code does
	// not have to re-run the same switch. Do not access directly; use
	// SessionCookieSameSiteMode() which guards the init order.
	sessionCookieSameSiteMode http.SameSite

	// Logging
	LogLevel string

	// CORS
	CORSAllowedOrigins string

	// OGP fetcher
	OGPUserAgent string

	// Fuju emotion model integration. When FujuModelBaseURL is empty
	// the dispatcher boots in disabled mode and the server-side commit
	// hooks (post / like / follow) become no-ops. This lets local dev
	// / CI run without a fuju instance reachable.
	FujuModelBaseURL       string
	FujuModelTenantID      string
	FujuModelScope         string
	FujuModelBatchSize     int
	FujuModelFlushInterval time.Duration
	FujuModelSendTimeout   time.Duration
	FujuModelQueueCapacity int

	// Cloudflare R2 (S3-compatible) image storage. All five fields must
	// be set together; any partial configuration is rejected by Validate
	// to surface misconfiguration at boot rather than at first upload.
	// When all are empty the image upload routes are not registered —
	// see R2Enabled().
	R2Endpoint        string
	R2BucketName      string
	R2PublicDomain    string
	R2AccessKeyID     string
	R2SecretAccessKey string
}

// Load loads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		ServerPort:                  getEnvInt("SERVER_PORT", 8080),
		Environment:                 getEnv("ENVIRONMENT", "development"),
		DBHost:                      getEnv("DB_HOST", ""),
		DBPort:                      getEnvInt("DB_PORT", 5432),
		DBName:                      getEnv("DB_NAME", ""),
		DBUser:                      getEnv("DB_USER", ""),
		DBPassword:                  getEnv("DB_PASSWORD", ""),
		DatabaseURL:                 getEnv("DATABASE_URL", ""),
		DBMaxConns:                  int32(getEnvInt("DB_MAX_CONNS", 0)),
		DBMinConns:                  int32(getEnvInt("DB_MIN_CONNS", 0)),
		repoBackendRaw:              getEnv("REPO_BACKEND", ""),
		AuthCoreBaseURL:             getEnv("AUTHCORE_BASE_URL", ""),
		AuthCoreClientID:            getEnv("AUTHCORE_CLIENT_ID", ""),
		AuthCoreClientSecret:        getEnv("AUTHCORE_CLIENT_SECRET", ""),
		AuthCoreIntrospectPath:      getEnv("AUTHCORE_INTROSPECT_PATH", "/v1/auth/introspect"),
		AuthCoreProfilePath:         getEnv("AUTHCORE_PROFILE_PATH", "/v1/user/profile"),
		AuthCoreProfileTTL:          getEnvDuration("AUTHCORE_PROFILE_TTL", time.Hour),
		AuthCoreIntrospectCacheTTL:  getEnvDuration("AUTHCORE_INTROSPECT_CACHE_TTL", 30*time.Second),
		SessionCookieName:           getEnv("SESSION_COOKIE_NAME", "fuju_access"),
		SessionCookieSecure:         getEnvBool("SESSION_COOKIE_SECURE", true),
		SessionCookieSameSite:       getEnv("SESSION_COOKIE_SAMESITE", "Lax"),
		SessionCookieDomain:         getEnv("SESSION_COOKIE_DOMAIN", ""),
		SessionCookieFallbackMaxAge: getEnvDuration("SESSION_COOKIE_FALLBACK_MAX_AGE", time.Hour),
		LogLevel:                    getEnv("LOG_LEVEL", "info"),
		CORSAllowedOrigins:          getEnv("CORS_ALLOWED_ORIGINS", "*"),
		OGPUserAgent:                getEnv("OGP_USER_AGENT", "FujuBot/1.0 (+https://fuju.example.com/bot)"),
		FujuModelBaseURL:            getEnv("FUJU_MODEL_BASE_URL", ""),
		FujuModelTenantID:           getEnv("FUJU_MODEL_TENANT_ID", ""),
		FujuModelScope:              getEnv("FUJU_MODEL_SCOPE", "ingest:events"),
		FujuModelBatchSize:          getEnvInt("FUJU_MODEL_BATCH_SIZE", 50),
		FujuModelFlushInterval:      getEnvDuration("FUJU_MODEL_FLUSH_INTERVAL", 5*time.Second),
		FujuModelSendTimeout:        getEnvDuration("FUJU_MODEL_SEND_TIMEOUT", 5*time.Second),
		FujuModelQueueCapacity:      getEnvInt("FUJU_MODEL_QUEUE_CAPACITY", 0),
		R2Endpoint:                  getEnv("R2_ENDPOINT", ""),
		R2BucketName:                getEnv("R2_BUCKET_NAME", ""),
		R2PublicDomain:              getEnv("R2_PUBLIC_DOMAIN", ""),
		R2AccessKeyID:               getEnv("R2_ACCESS_KEY_ID", ""),
		R2SecretAccessKey:           getEnv("R2_SECRET_ACCESS_KEY", ""),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// RepoBackend resolves the effective repository backend:
//   - explicit REPO_BACKEND wins
//   - unset + Environment == "development" → inmemory (fast local start)
//   - unset + anything else → postgres (production-safe default)
func (c *Config) RepoBackend() string {
	switch c.repoBackendRaw {
	case RepoBackendInMemory, RepoBackendPostgres:
		return c.repoBackendRaw
	}
	if c.Environment == "development" {
		return RepoBackendInMemory
	}
	return RepoBackendPostgres
}

// MaxConns exposes the configured pool max for pkg/db.NewPool. Zero
// means "inherit pgx default".
func (c *Config) MaxConns() int32 { return c.DBMaxConns }

// MinConns exposes the configured pool min for pkg/db.NewPool. Zero
// means "inherit pgx default".
func (c *Config) MinConns() int32 { return c.DBMinConns }

// DSN returns the connection string for the postgres backend. When
// DATABASE_URL is set it wins verbatim (so cloud secrets that embed
// sslmode / pool params pass through unchanged); otherwise a DSN is
// assembled from the DB_* fields with sslmode=disable (acceptable
// for dev and behind-VPC production; public deployments must use
// DATABASE_URL with sslmode=require).
func (c *Config) DSN() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}
	// url.UserPassword handles percent-encoding of special chars in
	// passwords (e.g. '@' or ':') that would otherwise break the DSN
	// parser.
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.DBUser, c.DBPassword),
		Host:     fmt.Sprintf("%s:%d", c.DBHost, c.DBPort),
		Path:     "/" + c.DBName,
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

// Validate validates the configuration. DB_* fields are required only
// when the effective backend is postgres AND DATABASE_URL is absent;
// running the in-memory backend in dev should not demand a DB config.
func (c *Config) Validate() error {
	if c.repoBackendRaw != "" &&
		c.repoBackendRaw != RepoBackendInMemory &&
		c.repoBackendRaw != RepoBackendPostgres {
		return fmt.Errorf("REPO_BACKEND must be %q or %q, got %q",
			RepoBackendInMemory, RepoBackendPostgres, c.repoBackendRaw)
	}

	if c.RepoBackend() == RepoBackendPostgres {
		if c.DatabaseURL != "" {
			// Reject non-postgres schemes up front; pgx would eventually
			// fail in NewPool, but catching it at Load keeps startup
			// error messages close to the config source.
			if !strings.HasPrefix(c.DatabaseURL, "postgres://") && !strings.HasPrefix(c.DatabaseURL, "postgresql://") {
				return fmt.Errorf("DATABASE_URL must start with postgres:// or postgresql://")
			}
		} else {
			if c.DBHost == "" {
				return fmt.Errorf("DB_HOST is required when REPO_BACKEND=postgres and DATABASE_URL is unset")
			}
			if c.DBName == "" {
				return fmt.Errorf("DB_NAME is required when REPO_BACKEND=postgres and DATABASE_URL is unset")
			}
			if c.DBUser == "" {
				return fmt.Errorf("DB_USER is required when REPO_BACKEND=postgres and DATABASE_URL is unset")
			}
			if c.DBPassword == "" {
				return fmt.Errorf("DB_PASSWORD is required when REPO_BACKEND=postgres and DATABASE_URL is unset")
			}
		}
	}

	if c.AuthCoreBaseURL == "" {
		return fmt.Errorf("AUTHCORE_BASE_URL is required")
	}
	if c.AuthCoreClientID == "" {
		return fmt.Errorf("AUTHCORE_CLIENT_ID is required")
	}
	if c.AuthCoreClientSecret == "" {
		return fmt.Errorf("AUTHCORE_CLIENT_SECRET is required")
	}
	switch c.SessionCookieSameSite {
	case "Lax":
		c.sessionCookieSameSiteMode = http.SameSiteLaxMode
	case "Strict":
		c.sessionCookieSameSiteMode = http.SameSiteStrictMode
	case "None":
		c.sessionCookieSameSiteMode = http.SameSiteNoneMode
	default:
		return fmt.Errorf("SESSION_COOKIE_SAMESITE must be Lax, Strict, or None, got %q", c.SessionCookieSameSite)
	}
	// SameSite=None requires Secure: browsers reject the combination
	// otherwise. Catch it at boot rather than in ad-hoc request logs.
	if c.SessionCookieSameSite == "None" && !c.SessionCookieSecure {
		return fmt.Errorf("SESSION_COOKIE_SAMESITE=None requires SESSION_COOKIE_SECURE=true")
	}
	// Anything beyond local dev must ship a Secure cookie — a plain-HTTP
	// staging / production deployment would leak the access token on
	// the wire. "development" is the only environment where Secure=false
	// is legitimate (localhost http).
	if !c.SessionCookieSecure && c.Environment != "development" {
		return fmt.Errorf("SESSION_COOKIE_SECURE=true is required when ENVIRONMENT != development (got %q)", c.Environment)
	}
	// A zero fallback would emit a session cookie (no Max-Age) whenever
	// the access token lacks an exp claim; that is never what we want
	// for an auth cookie. Reject up-front.
	if c.SessionCookieFallbackMaxAge <= 0 {
		return fmt.Errorf("SESSION_COOKIE_FALLBACK_MAX_AGE must be > 0 (got %s)", c.SessionCookieFallbackMaxAge)
	}
	// Fuju model integration: BaseURL + TenantID are paired (both empty
	// = disabled, both set = enabled, anything else = misconfiguration).
	if (c.FujuModelBaseURL == "") != (c.FujuModelTenantID == "") {
		return fmt.Errorf("FUJU_MODEL_BASE_URL and FUJU_MODEL_TENANT_ID must be set together (got base=%q tenant=%q)", c.FujuModelBaseURL, c.FujuModelTenantID)
	}
	// R2 image storage: all five fields are paired (all empty = disabled,
	// all set = enabled). A partial configuration is almost certainly a
	// deployment mistake, so fail boot instead of silently disabling
	// uploads or starting with an unauthenticated S3 client.
	if err := c.validateR2(); err != nil {
		return err
	}
	return nil
}

// validateR2 enforces the all-or-nothing rule on the R2 fields. Returns
// nil for the two valid states (all empty, all set) and an error that
// names the partially-set fields otherwise.
func (c *Config) validateR2() error {
	fields := []struct {
		name  string
		value string
	}{
		{"R2_ENDPOINT", c.R2Endpoint},
		{"R2_BUCKET_NAME", c.R2BucketName},
		{"R2_PUBLIC_DOMAIN", c.R2PublicDomain},
		{"R2_ACCESS_KEY_ID", c.R2AccessKeyID},
		{"R2_SECRET_ACCESS_KEY", c.R2SecretAccessKey},
	}
	var setNames, unsetNames []string
	for _, f := range fields {
		if f.value == "" {
			unsetNames = append(unsetNames, f.name)
		} else {
			setNames = append(setNames, f.name)
		}
	}
	if len(setNames) == 0 || len(unsetNames) == 0 {
		return nil
	}
	return fmt.Errorf("R2 configuration is partial: set=%v, unset=%v (all five must be set together or all empty)", setNames, unsetNames)
}

// FujuModelEnabled reports whether the fuju-emotion-model integration is
// configured. Used at boot to gate the dispatcher / hooks / endpoint.
func (c *Config) FujuModelEnabled() bool {
	return c.FujuModelBaseURL != "" && c.FujuModelTenantID != ""
}

// R2Enabled reports whether all five R2 fields are populated. Validate
// guarantees the all-or-nothing rule, so checking any one would suffice
// in practice — the explicit AND is documentation. Used at boot to gate
// the image upload routes.
func (c *Config) R2Enabled() bool {
	return c.R2Endpoint != "" &&
		c.R2BucketName != "" &&
		c.R2PublicDomain != "" &&
		c.R2AccessKeyID != "" &&
		c.R2SecretAccessKey != ""
}

// SessionCookieSameSiteMode returns the http.SameSite value matching
// SessionCookieSameSite. Validate populates this; callers that bypass
// Validate get http.SameSiteDefaultMode (the zero value), which is
// never a configuration we want to emit — but since every real code
// path runs Validate via Load, the guard is belt-and-suspenders.
func (c *Config) SessionCookieSameSiteMode() http.SameSite {
	return c.sessionCookieSameSiteMode
}

// Helper functions

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return intValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}
	return d
}
