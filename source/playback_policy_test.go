package main

import (
	"testing"
	"time"
)

func TestDecoderRetriesCannotRenewOverallStartupDeadline(t *testing.T) {
	start := time.Now()
	overall := start.Add(20 * time.Second)
	for _, elapsed := range []time.Duration{6 * time.Second, 8 * time.Second, 15 * time.Second, 19 * time.Second} {
		got := boundedPlaybackDeadline(start.Add(elapsed), 12*time.Second, overall)
		if got.After(overall) {
			t.Fatal("fallback extended the total startup budget")
		}
	}
	if !boundedPlaybackDeadline(start.Add(19*time.Second), 12*time.Second, overall).Equal(overall) {
		t.Fatal("late retry received a fresh timeout instead of its remaining budget")
	}
}

func TestSuccessfulProviderUserAgentIsFirstWithoutLosingFallbacks(t *testing.T) {
	for _, preferred := range append([]string{"", "unknown"}, playlistUserAgents...) {
		order := playbackUserAgents(preferred)
		seen := map[string]bool{}
		for _, ua := range order {
			if seen[ua] {
				t.Fatal("duplicate UA")
			}
			seen[ua] = true
		}
		for _, ua := range playlistUserAgents {
			if !seen[ua] {
				t.Fatal("lost compatibility UA")
			}
		}
		if seen[preferred] && order[0] != preferred {
			t.Fatal("successful UA was not first")
		}
	}
	if playbackProvider("http://Example.test:8080/play/private") != "example.test:8080" {
		t.Fatal("provider cache key must retain its port without retaining credentials")
	}
}
