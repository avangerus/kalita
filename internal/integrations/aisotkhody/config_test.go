package aisotkhody

import "testing"

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv(envAPIURL, "https://ais.example.local/api")
	t.Setenv(envAPIKey, "secret-key")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if cfg.APIURL != "https://ais.example.local/api" {
		t.Fatalf("APIURL = %q", cfg.APIURL)
	}
	if cfg.APIKey != "secret-key" {
		t.Fatalf("APIKey = %q", cfg.APIKey)
	}
}

func TestLoadConfigFromEnvRequiresValues(t *testing.T) {
	t.Setenv(envAPIURL, "")
	t.Setenv(envAPIKey, "")

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("LoadConfigFromEnv() error = nil, want error")
	}
}
