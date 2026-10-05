package loggingsdk

import (
	"context"
	"regexp"
	"testing"
	"time"
)

var logIDRe = regexp.MustCompile(`^\d{8}-\d{4}-[0-9a-z]{6}$`)

func TestNewLogIDShape(t *testing.T) {
	got := NewLogID()
	if !logIDRe.MatchString(got) {
		t.Fatalf("NewLogID() = %q, want yyyymmdd-hhmm-postfix", got)
	}
}

func TestNewLogIDPrefixIsCurrentUTCMinute(t *testing.T) {
	before := time.Now().UTC().Add(-time.Minute).Format("20060102-1504")
	after := time.Now().UTC().Add(time.Minute).Format("20060102-1504")

	prefix := NewLogID()[:13]
	if prefix < before || prefix > after {
		t.Errorf("prefix %q outside [%q, %q]", prefix, before, after)
	}
}

func TestNewLogIDPostfixIsUnique(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := NewLogID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate log id %q after %d calls", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestNewLogIDFlowsThroughWithLogID(t *testing.T) {
	id := NewLogID()
	if got := logidFromCtx(WithLogID(context.Background(), id)); got != id {
		t.Errorf("logidFromCtx = %q, want %q", got, id)
	}
}
