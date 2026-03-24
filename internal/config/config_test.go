package config

import "testing"

func TestLoadWithPathPrefersDatabaseURLEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://from-database-url")
	t.Setenv("KALITA_DB_URL", "postgres://from-kalita-db-url")

	cfg := LoadWithPath("/non-existent-config.json")
	if cfg.DBURL != "postgres://from-database-url" {
		t.Fatalf("DBURL = %q, want DATABASE_URL value", cfg.DBURL)
	}
}
