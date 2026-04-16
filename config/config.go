// Package config provides configuration loading and management.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all application configuration
type Config struct {
	// Server
	ServerPort  int    `default:"8080"`
	Environment string `default:"development"`

	// Database
	DBHost     string `required:"true"`
	DBPort     int    `default:"5432"`
	DBName     string `required:"true"`
	DBUser     string `required:"true"`
	DBPassword string `required:"true"`
	DBMaxConn  int    `default:"25"`
	DBMinConn  int    `default:"5"`

	// Redis
	RedisURL string `required:"true"`

	// OAuth2
	OAuthClientID     string `required:"true"`
	OAuthClientSecret string `required:"true"`
	OAuthRedirectURL  string `required:"true"`

	// JWT
	JWTSecret     string `required:"true"`
	JWTExpiration int    `default:"1800"` // 30 minutes

	// Session
	SessionSecret   string `required:"true"`
	SessionDuration int    `default:"86400"` // 24 hours

	// Logging
	LogLevel string `default:"info"`

	// CORS
	CORSAllowedOrigins string `default:"*"`
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		ServerPort:         getEnvInt("SERVER_PORT", 8080),
		Environment:        getEnv("ENVIRONMENT", "development"),
		DBHost:             getEnv("DB_HOST", ""),
		DBPort:             getEnvInt("DB_PORT", 5432),
		DBName:             getEnv("DB_NAME", ""),
		DBUser:             getEnv("DB_USER", ""),
		DBPassword:         getEnv("DB_PASSWORD", ""),
		DBMaxConn:          getEnvInt("DB_MAX_CONN", 25),
		DBMinConn:          getEnvInt("DB_MIN_CONN", 5),
		RedisURL:           getEnv("REDIS_URL", ""),
		OAuthClientID:      getEnv("OAUTH_CLIENT_ID", ""),
		OAuthClientSecret:  getEnv("OAUTH_CLIENT_SECRET", ""),
		OAuthRedirectURL:   getEnv("OAUTH_REDIRECT_URL", ""),
		JWTSecret:          getEnv("JWT_SECRET", ""),
		JWTExpiration:      getEnvInt("JWT_EXPIRATION", 1800),
		SessionSecret:      getEnv("SESSION_SECRET", ""),
		SessionDuration:    getEnvInt("SESSION_DURATION", 86400),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "*"),
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate validates the configuration
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
	if c.OAuthClientID == "" {
		return fmt.Errorf("OAUTH_CLIENT_ID is required")
	}
	if c.OAuthClientSecret == "" {
		return fmt.Errorf("OAUTH_CLIENT_SECRET is required")
	}
	if c.OAuthRedirectURL == "" {
		return fmt.Errorf("OAUTH_REDIRECT_URL is required")
	}
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	if c.SessionSecret == "" {
		return fmt.Errorf("SESSION_SECRET is required")
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
