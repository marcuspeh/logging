// Package config loads service settings from the environment.
//
// All values are read once via Load(). Defaults match the docker-compose
// service defined in PLAN §9.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the runtime configuration for the logging-backend service.
type Config struct {
	KafkaBrokers       []string
	KafkaTopic         string
	KafkaGroupID       string
	ParquetDir         string
	ParquetRotateBytes int64
	HTTPAddr           string
	ShutdownTimeout    time.Duration
}

// Defaults returns the default configuration used when no env overrides are
// provided.
func Defaults() Config {
	return Config{
		KafkaBrokers:       []string{"localhost:9092"},
		KafkaTopic:         "logs",
		KafkaGroupID:       "logging-collector",
		ParquetDir:         "/data/parquet",
		ParquetRotateBytes: 128 * 1024 * 1024,
		HTTPAddr:           ":8080",
		ShutdownTimeout:    10 * time.Second,
	}
}

// Load reads configuration from the process environment, falling back to
// Defaults() for any value that is not set or is empty.
//
// Returns an error if a value is present but malformed or if a required
// value is missing.
func Load() (Config, error) {
	cfg := Defaults()

	if v := os.Getenv("KAFKA_BROKERS"); v != "" {
		brokers := splitCSV(v)
		if len(brokers) == 0 {
			return cfg, errors.New("config: KAFKA_BROKERS is set but empty")
		}
		cfg.KafkaBrokers = brokers
	}
	if v := os.Getenv("KAFKA_TOPIC"); v != "" {
		cfg.KafkaTopic = v
	}
	if v := os.Getenv("KAFKA_GROUP_ID"); v != "" {
		cfg.KafkaGroupID = v
	}
	if v := os.Getenv("PARQUET_DIR"); v != "" {
		cfg.ParquetDir = v
	}
	if v := os.Getenv("PARQUET_ROTATE_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("config: PARQUET_ROTATE_BYTES=%q is not a valid integer: %w", v, err)
		}
		if n <= 0 {
			return cfg, fmt.Errorf("config: PARQUET_ROTATE_BYTES must be > 0, got %d", n)
		}
		cfg.ParquetRotateBytes = n
	}
	if v := os.Getenv("HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if v := os.Getenv("SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("config: SHUTDOWN_TIMEOUT=%q is not a valid duration: %w", v, err)
		}
		if d <= 0 {
			return cfg, fmt.Errorf("config: SHUTDOWN_TIMEOUT must be > 0, got %s", d)
		}
		cfg.ShutdownTimeout = d
	}
	return cfg, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}