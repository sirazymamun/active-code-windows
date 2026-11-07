package main

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type activationTransport func(*http.Request) (*http.Response, error)

func (f activationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestActivationRequestsJSON(t *testing.T) {
	a := &application{client: &http.Client{Transport: activationTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Accept") != "application/json" {
			t.Fatalf("activation API Accept = %q", r.Header.Get("Accept"))
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("activation API requires a JSON POST")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"allowed":true,"remaining_seconds":60,"playlist_url":"https://example.test/playlist.m3u"}`))}, nil
	})}}
	for _, action := range []string{"activate", "device-check"} {
		if _, err := a.remote(action, map[string]string{"device_id": "test"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOldAccessCheckCannotRevokeNewActivation(t *testing.T) {
	a, err := newApplication(filepath.Join(t.TempDir(), "state.dat"))
	if err != nil {
		t.Fatal(err)
	}
	a.token = strings.Repeat("a", 64)
	started, release := make(chan struct{}), make(chan struct{})
	a.client = &http.Client{Transport: activationTransport(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{"ok":true,"allowed":true,"subscription_id":2,"remaining_seconds":60,"playlist_url":"https://example.test/new.m3u","device_token":"`+strings.Repeat("b", 64)+`"}`
		if r.URL.Query().Get("action") == "device-check" {
			close(started)
			<-release
			status, body = 403, `{"error":"Old token revoked"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	result := make(chan error, 1)
	go func() {
		g, e := a.ensure(true)
		if e == nil && g.SubscriptionID != 2 {
			e = io.ErrUnexpectedEOF
		}
		result <- e
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("old check did not start")
	}
	if _, err := a.activate("1234567890"); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("old check did not finish")
	}
}
