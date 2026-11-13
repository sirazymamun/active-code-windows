package main

import (
	"net/url"
	"strings"
	"time"
)

func boundedPlaybackDeadline(now time.Time, attempt time.Duration, overall time.Time) time.Time {
	deadline := now.Add(attempt)
	if overall.Before(deadline) {
		return overall
	}
	return deadline
}

func playbackProvider(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// Keep every compatibility alternative, but start with a provider's proven UA.
func playbackUserAgents(preferred string) []string {
	out := make([]string, 0, len(playlistUserAgents))
	for _, ua := range playlistUserAgents {
		if ua == preferred {
			out = append(out, ua)
			break
		}
	}
	for _, ua := range playlistUserAgents {
		if len(out) == 0 || ua != out[0] {
			out = append(out, ua)
		}
	}
	return out
}
