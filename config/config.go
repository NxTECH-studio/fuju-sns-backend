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

	// Logging
	LogLevel string

	// CORS
	CORSAllowedOrigins string

	// OGP fetcher
	OGPUserAgent string
}

// Load loads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		ServerPort:                 getEnvInt("SERVER_PORT", 8080),
		Environment:                getEnv("ENVIRONMENT", "development"),
		DBHost:                     getEnv("DB_HOST", ""),
		DBPort:                     getEnvInt("DB_PORT", 5432),
		DBName:                     getEnv("DB_NAME", ""),
		DBUser:                     getEnv("DB_USER", ""),
		DBPassword:                 getEnv("DB_PASSWORD", ""),
		DatabaseURL:                getEnv("DATABASE_URL", ""),
		DBMaxConns:                 int32(getEnvInt("DB_MAX_CONNS", 0)),
		DBMinConns:                 int32(getEnvInt("DB_MIN_CONNS", 0)),
		repoBackendRaw:             getEnv("REPO_BACKEND", ""),
		AuthCoreBaseURL:            getEnv("AUTHCORE_BASE_URL", ""),
		AuthCoreClientID:           getEnv("AUTHCORE_CLIENT_ID", ""),
		AuthCoreClientSecret:       getEnv("AUTHCORE_CLIENT_SECRET", ""),
		AuthCoreIntrospectPath:     getEnv("AUTHCORE_INTROSPECT_PATH", "/v1/auth/introspect"),
		AuthCoreProfilePath:        getEnv("AUTHCORE_PROFILE_PATH", "/v1/user/profile"),
		AuthCoreProfileTTL:         getEnvDuration("AUTHCORE_PROFILE_TTL", time.Hour),
		AuthCoreIntrospectCacheTTL: getEnvDuration("AUTHCORE_INTROSPECT_CACHE_TTL", 30*time.Second),
		LogLevel:                   getEnv("LOG_LEVEL", "info"),
		CORSAllowedOrigins:         getEnv("CORS_ALLOWED_ORIGINS", "*"),
		OGPUserAgent:               getEnv("OGP_USER_AGENT", "FujuBot/1.0 (+https://fuju.example.com/bot)"),
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
	return nil
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
