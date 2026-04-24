package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port int

	AWSRegion      string
	AWSEndpointURL string

	ModuleBucket    string
	StateBucket     string
	StateRegion     string
	StateLockTable  string
	TerraformBinary string
	WorkDir         string
	PluginCacheDir  string

	JobTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Port:            intFromEnv("PORT", 8080),
		AWSRegion:       os.Getenv("AWS_REGION"),
		AWSEndpointURL:  os.Getenv("AWS_ENDPOINT_URL"),
		ModuleBucket:    os.Getenv("TF_MODULE_BUCKET"),
		StateBucket:     os.Getenv("TF_STATE_BUCKET"),
		StateRegion:     os.Getenv("TF_STATE_REGION"),
		StateLockTable:  os.Getenv("TF_STATE_DYNAMODB_TABLE"),
		TerraformBinary: strFromEnv("TF_BINARY_PATH", "/usr/local/bin/terraform"),
		WorkDir:         strFromEnv("TF_WORK_DIR", "/var/lib/tf-provisioner"),
		JobTimeout:      durationFromEnv("TF_JOB_TIMEOUT", 30*time.Minute),
	}
	if cfg.StateRegion == "" {
		cfg.StateRegion = cfg.AWSRegion
	}
	cfg.PluginCacheDir = strFromEnv("TF_PLUGIN_CACHE_DIR", cfg.WorkDir+"/plugin-cache")

	var missing []string
	if cfg.AWSRegion == "" {
		missing = append(missing, "AWS_REGION")
	}
	if cfg.ModuleBucket == "" {
		missing = append(missing, "TF_MODULE_BUCKET")
	}
	if cfg.StateBucket == "" {
		missing = append(missing, "TF_STATE_BUCKET")
	}
	if cfg.StateLockTable == "" {
		missing = append(missing, "TF_STATE_DYNAMODB_TABLE")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env vars: %v", missing)
	}
	return cfg, nil
}

func strFromEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func intFromEnv(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func durationFromEnv(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

var ErrMissingConfig = errors.New("missing required configuration")
