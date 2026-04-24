package config

import (
	"cmp"
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
	StatusBucket    string
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
		StatusBucket:    os.Getenv("TF_STATUS_BUCKET"),
		StateBucket:     os.Getenv("TF_STATE_BUCKET"),
		StateRegion:     os.Getenv("TF_STATE_REGION"),
		StateLockTable:  os.Getenv("TF_STATE_DYNAMODB_TABLE"),
		TerraformBinary: strFromEnv("TF_BINARY_PATH", "/usr/local/bin/terraform"),
		WorkDir:         strFromEnv("TF_WORK_DIR", "/var/lib/tf-provisioner"),
		JobTimeout:      durationFromEnv("TF_JOB_TIMEOUT", 30*time.Minute),
	}
	cfg.StateRegion = cmp.Or(cfg.StateRegion, cfg.AWSRegion)
	cfg.PluginCacheDir = cmp.Or(os.Getenv("TF_PLUGIN_CACHE_DIR"), cfg.WorkDir+"/plugin-cache")

	required := map[string]string{
		"AWS_REGION":              cfg.AWSRegion,
		"TF_MODULE_BUCKET":        cfg.ModuleBucket,
		"TF_STATUS_BUCKET":        cfg.StatusBucket,
		"TF_STATE_BUCKET":         cfg.StateBucket,
		"TF_STATE_DYNAMODB_TABLE": cfg.StateLockTable,
	}
	var errs []error
	for name, val := range required {
		if val == "" {
			errs = append(errs, fmt.Errorf("missing required env var: %s", name))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func strFromEnv(key, def string) string {
	return cmp.Or(os.Getenv(key), def)
}

func intFromEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func durationFromEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
