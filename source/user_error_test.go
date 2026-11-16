package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPrivateErrorsNeverReachUI(t *testing.T) {
	for _, raw := range []string{
		"Post https://private.invalid/api.php?token=SECRET: read tcp 192.0.2.1:443: timeout",
		"dial tcp: lookup private.invalid: no such host",
		"SQLSTATE database password SECRET",
		"provider.internal/api.php",
	} {
		got := userError(errors.New(raw))
		if strings.Contains(got, "SECRET") || got == raw || strings.Contains(got, "private.invalid") {
			t.Fatal("private details reached UI")
		}
	}
}
func TestActivationFailurePrivacy(t *testing.T) {
	for _, status := range []int{0, 400, 401, 403, 404, 409, 410, 429, 500, 502, 503} {
		a := &application{client: &http.Client{Transport: activationTransport(func(r *http.Request) (*http.Response, error) {
			if status == 0 {
				return nil, errors.New("dial tcp private.invalid:443 SECRET")
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"https://private.invalid/api.php SECRET"}`))}, nil
		})}}
		for _, action := range []string{"activate", "device-check"} {
			_, err := a.remote(action, map[string]string{})
			if err == nil {
				t.Fatal("expected failure")
			}
			if strings.Contains(err.Error(), "private.invalid") || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "http") {
				t.Fatal("raw error escaped remote")
			}
			if got := userError(err); got != err.Error() {
				t.Fatalf("public error lost: %s", got)
			}
		}
	}
}
