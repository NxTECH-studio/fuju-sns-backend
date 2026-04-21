// Package config provides configuration loading and management.
package config

import (
	"fmt"
	"os"
	"strconv"
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
	DBMaxConn  int
	DBMinConn  int

	// Redis
	RedisURL string

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

	// Frontend
	FrontendURL string
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
		DBMaxConn:                  getEnvInt("DB_MAX_CONN", 25),
		DBMinConn:                  getEnvInt("DB_MIN_CONN", 5),
		RedisURL:                   getEnv("REDIS_URL", ""),
		AuthCoreBaseURL:            getEnv("AUTHCORE_BASE_URL", ""),
		AuthCoreClientID:           getEnv("AUTHCORE_CLIENT_ID", ""),
		AuthCoreClientSecret:       getEnv("AUTHCORE_CLIENT_SECRET", ""),
		AuthCoreIntrospectPath:     getEnv("AUTHCORE_INTROSPECT_PATH", "/v1/auth/introspect"),
		AuthCoreProfilePath:        getEnv("AUTHCORE_PROFILE_PATH", "/v1/user/profile"),
		AuthCoreProfileTTL:         getEnvDuration("AUTHCORE_PROFILE_TTL", time.Hour),
		AuthCoreIntrospectCacheTTL: getEnvDuration("AUTHCORE_INTROSPECT_CACHE_TTL", 30*time.Second),
		LogLevel:                   getEnv("LOG_LEVEL", "info"),
		CORSAllowedOrigins:         getEnv("CORS_ALLOWED_ORIGINS", "*"),
		FrontendURL:                getEnv("FRONTEND_URL", ""),
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
	if c.RedisURL == "" {
		return fmt.Errorf("REDIS_URL is required")
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
