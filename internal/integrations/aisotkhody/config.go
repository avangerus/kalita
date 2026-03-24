package aisotkhody

import (
	"fmt"
	"os"
	"strings"
)

const (
	envAPIURL = "AIS_API_URL"
	envAPIKey = "AIS_API_KEY"
)

type Config struct {
	APIURL string
	APIKey string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		APIURL: strings.TrimSpace(os.Getenv(envAPIURL)),
		APIKey: strings.TrimSpace(os.Getenv(envAPIKey)),
	}
	if cfg.APIURL == "" {
		return Config{}, fmt.Errorf("%s is required", envAPIURL)
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("%s is required", envAPIKey)
	}
	return cfg, nil
}
