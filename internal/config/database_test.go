package config

import (
	"net/url"
	"testing"
)

func TestPostgresDSNBuildsURLAndEscapesCredentials(t *testing.T) {
	cfg := &Config{
		DBHost:     "db.example.com",
		DBPort:     "5432",
		DBUser:     "ride user",
		DBPassword: "p@ss/word",
		DBName:     "ride match",
		DBSSLMode:  "require",
	}

	parsed, err := url.Parse(cfg.PostgresDSN())
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	if parsed.Scheme != "postgres" || parsed.Host != "db.example.com:5432" || parsed.Path != "/ride match" {
		t.Fatalf("unexpected PostgreSQL DSN: %s", parsed)
	}
	if username := parsed.User.Username(); username != cfg.DBUser {
		t.Errorf("username = %q, want %q", username, cfg.DBUser)
	}
	if password, _ := parsed.User.Password(); password != cfg.DBPassword {
		t.Errorf("password = %q, want original credential", password)
	}
	if sslmode := parsed.Query().Get("sslmode"); sslmode != "require" {
		t.Errorf("sslmode = %q, want require", sslmode)
	}
}

func TestPostgresDSNUsesProvidedDatabaseURL(t *testing.T) {
	want := "postgres://user:password@host:5432/app?sslmode=require"
	cfg := &Config{DBURL: want}
	if got := cfg.PostgresDSN(); got != want {
		t.Errorf("PostgresDSN() = %q, want %q", got, want)
	}
}

func TestDatabaseDriverDefaultsAndInfersFromURL(t *testing.T) {
	tests := []struct {
		name        string
		configured  string
		databaseURL string
		wantDriver  string
	}{
		{name: "default remains mysql", wantDriver: "mysql"},
		{name: "infer postgres url", databaseURL: "postgres://user:pass@host/db", wantDriver: "postgres"},
		{name: "infer postgresql url", databaseURL: "postgresql://user:pass@host/db", wantDriver: "postgres"},
		{name: "explicit mysql wins", configured: "MySQL", databaseURL: "postgres://user:pass@host/db", wantDriver: "mysql"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := databaseDriver(test.configured, test.databaseURL); got != test.wantDriver {
				t.Errorf("databaseDriver() = %q, want %q", got, test.wantDriver)
			}
		})
	}
}
