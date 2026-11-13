package main

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedSessionFirstDenialStopsRetriesUntilNewActivation(t *testing.T) {
	a, err := newApplication(filepath.Join(t.TempDir(), "session.dat"))
	if err != nil {
		t.Fatal(err)
	}
	a.token = strings.Repeat("a", 64)
	checks, activations := 0, 0
	a.client = &http.Client{Transport: activationTransport(func(r *http.Request) (*http.Response, error) {
		status, body := 403, `{"ok":false}`
		if r.URL.Query().Get("action") == "activate" {
			activations++
			status, body = 200, `{"ok":true,"allowed":true,"remaining_seconds":60,"playlist_url":"https://example.test/list","device_token":"`+strings.Repeat("b", 64)+`"}`
		} else {
			checks++
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if !a.shouldMonitorSession() {
		t.Fatal("saved subscription must be checked on launch")
	}
	if _, err = a.ensure(true); err == nil {
		t.Fatal("expected denial")
	}
	if a.shouldMonitorSession() {
		t.Fatal("denied token must not auto retry")
	}
	for n := 0; n < 10; n++ {
		a.ensure(true)
		if _, err = a.activate(strings.Repeat("1", n)); err == nil {
			t.Fatal("short code accepted")
		}
	}
	if checks != 1 || activations != 0 {
		t.Fatalf("checks=%d activations=%d", checks, activations)
	}
	if _, err = a.activate("0123456789"); err != nil {
		t.Fatal(err)
	}
	if !a.shouldMonitorSession() {
		t.Fatal("new activation must resume monitoring")
	}
}

func TestTransientServerFailureRemainsRetryable(t *testing.T) {
	a := &application{token: strings.Repeat("a", 64)}
	calls := 0
	a.client = &http.Client{Transport: activationTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		status, body := 503, `{"ok":false}`
		if calls > 1 {
			status, body = 200, `{"ok":true,"allowed":true,"remaining_seconds":60,"playlist_url":"https://example.test/list"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if _, err := a.ensure(true); err == nil {
		t.Fatal("expected temporary failure")
	}
	if !a.shouldMonitorSession() {
		t.Fatal("network/server failure must allow automatic retry")
	}
	if g, err := a.ensure(true); err != nil || !g.Allowed {
		t.Fatalf("retry failed: %v", err)
	}
}
