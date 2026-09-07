package main

import (
	"errors"
	"net"
	"testing"
)

func TestQueryCountURL(t *testing.T) {
	got := buildQueryURL("http://localhost:4665", map[string]string{"project": "alpha", "logid": "x"})
	want := "http://localhost:4665/query?logid=x&project=alpha"
	if got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}

	got = buildQueryURL("http://localhost:4665", map[string]string{})
	want = "http://localhost:4665/query?project=billing-service"
	if got != want {
		t.Errorf("URL empty = %q, want %q", got, want)
	}
}

func TestIsTransientNet(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("dial tcp 127.0.0.1:9092: connect: connection refused"), true},
		{errors.New("read tcp: connection reset by peer"), true},
		{errors.New("unexpected EOF"), true},
		{errors.New("some other error"), false},
		{&net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
	}
	for _, tc := range cases {
		if got := isTransientNet(tc.err); got != tc.want {
			t.Errorf("isTransientNet(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
