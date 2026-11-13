package main

import (
	"os"
	"strings"
	"testing"
)

// Prevent regression to the former first-run VLC downloader.
func TestVLCRuntimeIsEmbeddedOnly(t *testing.T) {
	contents, err := os.ReadFile("libvlc_runtime_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(contents)
	for _, forbidden := range []string{"net/http", "http.Client", "response.Body", "vlcStableArchiveURL"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("forbidden VLC runtime downloader: %s", forbidden)
		}
	}
	if !strings.Contains(s, "return bundledVLC, nil") {
		t.Fatal("embedded runtime not required")
	}
}
