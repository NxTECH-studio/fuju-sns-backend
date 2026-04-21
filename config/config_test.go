package config

import "testing"

func TestRepoBackend_explicit_overrides_environment(t *testing.T) {
	cases := []struct {
		raw         string
		environment string
		want        string
	}{
		{raw: RepoBackendInMemory, environment: "production", want: RepoBackendInMemory},
		{raw: RepoBackendPostgres, environment: "development", want: RepoBackendPostgres},
	}
	for _, tc := range cases {
		c := &Config{RepoBackendRaw: tc.raw, Environment: tc.environment}
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
		{environment: "development", want: RepoBackendInMemory},
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
		Environment:          "development",
		RepoBackendRaw:       RepoBackendInMemory,
		AuthCoreBaseURL:      "http://localhost",
		AuthCoreClientID:     "x",
		AuthCoreClientSecret: "y",
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("inmemory validate should pass without DB_*: %v", err)
	}
}

func TestValidate_postgres_requires_db_fields_when_no_url(t *testing.T) {
	c := &Config{
		Environment:          "production",
		RepoBackendRaw:       RepoBackendPostgres,
		AuthCoreBaseURL:      "http://localhost",
		AuthCoreClientID:     "x",
		AuthCoreClientSecret: "y",
	}
	if err := c.Validate(); err == nil {
		t.Fatalf("postgres without DB_* or DATABASE_URL should fail")
	}
}

func TestValidate_postgres_accepts_database_url_alone(t *testing.T) {
	c := &Config{
		Environment:          "production",
		RepoBackendRaw:       RepoBackendPostgres,
		DatabaseURL:          "postgres://u:p@h/d",
		AuthCoreBaseURL:      "http://localhost",
		AuthCoreClientID:     "x",
		AuthCoreClientSecret: "y",
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("DATABASE_URL alone should satisfy postgres: %v", err)
	}
}

func TestValidate_rejects_unknown_repo_backend(t *testing.T) {
	c := &Config{
		Environment:          "development",
		RepoBackendRaw:       "redis",
		AuthCoreBaseURL:      "http://localhost",
		AuthCoreClientID:     "x",
		AuthCoreClientSecret: "y",
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
