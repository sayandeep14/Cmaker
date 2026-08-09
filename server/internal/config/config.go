// Package config loads the API server's runtime configuration from
// environment variables - kept deliberately tiny (no viper/etc.) since
// M1's whole config surface is one connection string and a listen port.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
	Port        string

	R2AccountID       string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2Bucket          string
}

// Load reads Config from the environment, erroring on a genuinely
// missing required value - PORT defaults to 8080 (Fly.io's own
// convention) when unset.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		Port:              os.Getenv("PORT"),
		R2AccountID:       os.Getenv("R2_ACCOUNT_ID"),
		R2AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		R2Bucket:          os.Getenv("R2_BUCKET"),
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	var missing []string
	for _, pair := range [][2]string{
		{"DATABASE_URL", cfg.DatabaseURL},
		{"R2_ACCOUNT_ID", cfg.R2AccountID},
		{"R2_ACCESS_KEY_ID", cfg.R2AccessKeyID},
		{"R2_SECRET_ACCESS_KEY", cfg.R2SecretAccessKey},
		{"R2_BUCKET", cfg.R2Bucket},
	} {
		if pair[1] == "" {
			missing = append(missing, pair[0])
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %v", missing)
	}
	return cfg, nil
}
