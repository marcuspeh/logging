package config

import (
	"testing"
	"time"
)

// withEnv sets env vars for the duration of the test and restores them on
// cleanup.
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestDefaults(t *testing.T) {
	// Ensure no relevant env vars leak in from the host.
	t.Setenv("KAFKA_BROKERS", "")
	t.Setenv("KAFKA_TOPIC", "")
	t.Setenv("KAFKA_GROUP_ID", "")
	t.Setenv("PARQUET_DIR", "")
	t.Setenv("PARQUET_ROTATE_BYTES", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Defaults()
	if cfg.KafkaBrokers[0] != want.KafkaBrokers[0] ||
		cfg.KafkaTopic != want.KafkaTopic ||
		cfg.KafkaGroupID != want.KafkaGroupID ||
		cfg.ParquetDir != want.ParquetDir ||
		cfg.ParquetRotateBytes != want.ParquetRotateBytes ||
		cfg.HTTPAddr != want.HTTPAddr ||
		cfg.ShutdownTimeout != want.ShutdownTimeout {
		t.Fatalf("Load() with no overrides = %+v, want %+v", cfg, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	withEnv(t, map[string]string{
		"KAFKA_BROKERS":        "kafka1:9092, kafka2:9092 ,kafka3:9092",
		"KAFKA_TOPIC":          "events",
		"KAFKA_GROUP_ID":       "group-7",
		"PARQUET_DIR":          "/var/log/rows",
		"PARQUET_ROTATE_BYTES": "1024",
		"HTTP_ADDR":            ":9000",
		"SHUTDOWN_TIMEOUT":     "5s",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.KafkaBrokers, []string{"kafka1:9092", "kafka2:9092", "kafka3:9092"}; !equalStrings(got, want) {
		t.Errorf("KafkaBrokers = %v, want %v", got, want)
	}
	if cfg.KafkaTopic != "events" {
		t.Errorf("KafkaTopic = %q, want %q", cfg.KafkaTopic, "events")
	}
	if cfg.KafkaGroupID != "group-7" {
		t.Errorf("KafkaGroupID = %q, want %q", cfg.KafkaGroupID, "group-7")
	}
	if cfg.ParquetDir != "/var/log/rows" {
		t.Errorf("ParquetDir = %q, want %q", cfg.ParquetDir, "/var/log/rows")
	}
	if cfg.ParquetRotateBytes != 1024 {
		t.Errorf("ParquetRotateBytes = %d, want %d", cfg.ParquetRotateBytes, 1024)
	}
	if cfg.HTTPAddr != ":9000" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":9000")
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, 5*time.Second)
	}
}

func TestLoadInvalidBytes(t *testing.T) {
	t.Setenv("PARQUET_ROTATE_BYTES", "not-a-number")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for invalid PARQUET_ROTATE_BYTES, got nil")
	}
}

func TestLoadNonPositiveBytes(t *testing.T) {
	t.Setenv("PARQUET_ROTATE_BYTES", "0")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for PARQUET_ROTATE_BYTES=0, got nil")
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "definitely-not-a-duration")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for invalid SHUTDOWN_TIMEOUT, got nil")
	}
}

func TestLoadEmptyBrokers(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "  ,  ,  ")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for KAFKA_BROKERS containing only whitespace, got nil")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
