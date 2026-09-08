package loggingsdk

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func ctxWithLogID(id string) context.Context {
	return WithLogID(context.Background(), id)
}

func TestNewRejectsBadArgs(t *testing.T) {
	if _, err := New("", "p"); err == nil {
		t.Error("expected error for empty bootstrap")
	}
	if _, err := New("k:9092", ""); err == nil {
		t.Error("expected error for empty project")
	}
}

func TestProjectAndStats(t *testing.T) {
	c, err := New("k:9092", "billing")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Project(); got != "billing" {
		t.Errorf("Project = %q, want billing", got)
	}
	if s := c.Stats(); s.Sent != 0 || s.Failed != 0 || s.Dropped != 0 {
		t.Errorf("fresh Stats = %+v, want all zero", s)
	}
}

func TestMinLevelDropsBelowThreshold(t *testing.T) {
	c, _ := New("k:9092", "p", WithMinLevel(LevelWarn))
	defer c.Close()

	c.Debug(ctxWithLogID("x"), "should be dropped")
	c.Info(ctxWithLogID("x"), "should be dropped")
	if s := c.Stats(); s.Dropped != 0 {
		t.Errorf("Stats = %+v, want Dropped=0 for below-threshold calls", s)
	}
}

func TestEventShape(t *testing.T) {
	payload, err := encodeEvent(event{
		Timestamp: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
		Project:   "billing",
		LogID:     "req-1",
		Level:     "INFO",
		Message:   "ok",
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"timestamp", "project", "logid", "level", "message"} {
		if _, ok := got[k]; !ok {
			t.Errorf("encoded payload missing %q: %v", k, got)
		}
	}
	if got["project"] != "billing" {
		t.Errorf("project = %v, want billing", got["project"])
	}
	if got["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", got["level"])
	}
	if got["logid"] != "req-1" {
		t.Errorf("logid = %v, want req-1", got["logid"])
	}
}

func TestLevelStringRoundTrip(t *testing.T) {
	for _, l := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal} {
		if _, ok := ParseLevel(l.String()); !ok {
			t.Errorf("ParseLevel(%q) failed", l.String())
		}
	}
	if _, ok := ParseLevel("bogus"); ok {
		t.Error("ParseLevel(bogus) should fail")
	}
}

func TestLogIDFromContext(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"with logid", ctxWithLogID("req-7f2c"), "req-7f2c"},
		{"empty string falls back to dash", ctxWithLogID(""), "-"},
		{"missing key falls back to dash", context.Background(), "-"},
		{"wrong type falls back to dash",
			context.WithValue(context.Background(), LogIDKey, 42),
			"-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := logidFromCtx(tc.ctx); got != tc.want {
				t.Errorf("logidFromCtx = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPublishSyncNoBrokerRecordsFailure(t *testing.T) {
	c, _ := New("127.0.0.1:1", "p")
	defer c.Close()

	c.Info(ctxWithLogID("x"), "msg")
	if s := c.Stats(); s.Failed == 0 {
		t.Errorf("expected Failed > 0 after write to unreachable broker, got %+v", s)
	}
}

func TestAsyncOverflowDropsOldest(t *testing.T) {
	c, _ := New("127.0.0.1:1", "p", WithAsync(2))
	defer c.Close()

	for i := 0; i < 100; i++ {
		c.Info(ctxWithLogID("x"), "msg %d", i)
	}
	s := c.Stats()
	if s.Dropped == 0 {
		t.Errorf("expected Dropped > 0 after overflow, got %+v", s)
	}
}

func TestSlogAdapterConstruction(t *testing.T) {
	c, _ := New("127.0.0.1:1", "p")
	defer c.Close()

	if LoggingHandler(c) == nil {
		t.Fatal("LoggingHandler returned nil")
	}
}

func TestFormatMessage(t *testing.T) {
	if got := formatMessage("hello", nil); got != "hello" {
		t.Errorf("no-args = %q, want %q", got, "hello")
	}
	if got := formatMessage("count=%d", []any{42}); got != "count=42" {
		t.Errorf("with-args = %q, want %q", got, "count=42")
	}
}

func TestWithLogID(t *testing.T) {
	ctx := WithLogID(context.Background(), "abc")
	if got := ctx.Value(LogIDKey); got != "abc" {
		t.Errorf("ctx.Value = %v, want abc", got)
	}
}
