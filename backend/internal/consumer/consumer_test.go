package consumer

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/marcuspeh/logging-backend/internal/model"
)

// silentLogger discards output for test runs.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestDecodeValid(t *testing.T) {
	ts := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(model.LogEvent{
		Timestamp: ts,
		Project:   "billing",
		LogID:     "req-1",
		Level:     "INFO",
		Message:   "ok",
	})
	ev, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !ev.Timestamp.Equal(ts) {
		t.Errorf("timestamp = %v, want %v", ev.Timestamp, ts)
	}
	if ev.Project != "billing" || ev.LogID != "req-1" || ev.Level != "INFO" || ev.Message != "ok" {
		t.Errorf("decoded event mismatch: %+v", ev)
	}
}

func TestDecodeBackfillsTimestamp(t *testing.T) {
	body, _ := json.Marshal(map[string]string{
		"project": "p", "logid": "l", "level": "INFO", "message": "m",
	})
	before := time.Now().UTC()
	ev, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	after := time.Now().UTC()
	if ev.Timestamp.Before(before) || ev.Timestamp.After(after) {
		t.Errorf("backfilled timestamp %v not in [%v, %v]", ev.Timestamp, before, after)
	}
}

func TestDecodeMissingRequired(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"bad json", `not-json`, "decode json"},
		{"missing project", `{"logid":"l","level":"INFO","message":"m"}`, "project is required"},
		{"missing logid", `{"project":"p","level":"INFO","message":"m"}`, "logid is required"},
		{"missing level", `{"project":"p","logid":"l","message":"m"}`, "level is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode([]byte(tc.body))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !bytes.Contains([]byte(err.Error()), []byte(tc.want)) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestNewRejectsBadOptions(t *testing.T) {
	sink := &fakeSink{}
	if _, err := New(Options{}, sink); err == nil {
		t.Error("expected error for empty brokers")
	}
	if _, err := New(Options{Brokers: []string{"x:1"}}, sink); err == nil {
		t.Error("expected error for empty topic")
	}
	if _, err := New(Options{Brokers: []string{"x:1"}, Topic: "t"}, sink); err == nil {
		t.Error("expected error for empty group id")
	}
	if _, err := New(Options{Brokers: []string{"x:1"}, Topic: "t", GroupID: "g"}, nil); err == nil {
		t.Error("expected error for nil sink")
	}
}

func TestNewDefaultsLogger(t *testing.T) {
	c, err := New(Options{Brokers: []string{"x:1"}, Topic: "t", GroupID: "g"}, &fakeSink{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.logger == nil {
		t.Error("expected default logger to be assigned")
	}
}

func TestStatsZero(t *testing.T) {
	c, err := New(Options{Brokers: []string{"x:1"}, Topic: "t", GroupID: "g", Logger: silentLogger()}, &fakeSink{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d, s := c.Stats()
	if d != 0 || s != 0 {
		t.Errorf("Stats = (%d, %d), want (0, 0)", d, s)
	}
}

// fakeSink is a no-op Sink that counts how many writes happened. Useful
// for the New() validation tests; the Run() integration is exercised
// against a real Kafka broker in the smoke test (Task 8).
type fakeSink struct {
	written int
}

func (f fakeSink) Write(ev model.LogEvent) error {
	f.written++
	_ = ev
	_ = context.TODO()
	return nil
}
