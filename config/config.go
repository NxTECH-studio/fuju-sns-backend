// Package config provides configuration loading and management.
//
// Note: DB_* fields are kept as configuration shape but the Go process
// currently runs on an in-memory repository (DB wiring is not implemented).
// They are retained so that db/init.sh, the Makefile db-* targets, and the
// docker-compose postgres service remain usable for migration / local tooling.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration.
type Config struct {
	// Server
	ServerPort  int
	Environment string

	// Database
	DBHost     string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string

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
		ServerPort:                  getEnvInt("SERVER_PORT", 8080),
		Environment:                 getEnv("ENVIRONMENT", "development"),
		DBHost:                      getEnv("DB_HOST", ""),
		DBPort:                      getEnvInt("DB_PORT", 5432),
		DBName:                      getEnv("DB_NAME", ""),
		DBUser:                      getEnv("DB_USER", ""),
		DBPassword:                  getEnv("DB_PASSWORD", ""),
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
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.DBHost == "" {
		return fmt.Errorf("DB_HOST is required")
	}
	if c.DBName == "" {
		return fmt.Errorf("DB_NAME is required")
	}
	if c.DBUser == "" {
		return fmt.Errorf("DB_USER is required")
	}
	if c.DBPassword == "" {
		return fmt.Errorf("DB_PASSWORD is required")
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
	case "Lax", "Strict", "None":
	default:
		return fmt.Errorf("SESSION_COOKIE_SAMESITE must be Lax, Strict, or None, got %q", c.SessionCookieSameSite)
	}
	// SameSite=None requires Secure: browsers reject the combination
	// otherwise. Catch it at boot rather than in ad-hoc request logs.
	if c.SessionCookieSameSite == "None" && !c.SessionCookieSecure {
		return fmt.Errorf("SESSION_COOKIE_SAMESITE=None requires SESSION_COOKIE_SECURE=true")
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
