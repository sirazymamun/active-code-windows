package main

import (
	"os"
	"strings"
	"testing"
)

// Git checkout uses CRLF on Windows runners and LF on Linux. The guard checks
// the same source on both, without mistaking line endings for missing behavior.
func playlistFailureWiringError(source string) string {
	s := strings.ReplaceAll(source, "\r\n", "\n")
	a := strings.Index(s, "case \"load\":")
	if a < 0 {
		return "playlist load handler is missing"
	}
	offset := strings.Index(s[a:], "case \"play\":")
	if offset < 0 {
		return "playlist load handler boundary is missing"
	}
	load := s[a : a+offset]
	if !strings.Contains(load, "if task.note != \"\" || len(g.channels) == 0 {\n\t\t\t\tg.showPlaylistLoadFailure()") {
		return "both failed and empty import outcomes must enter the persistent recovery view"
	}
	for _, marker := range []string{
		"if g.playlistLoadFailed {\n\t\tg.drawPlaylistLoadFailure(hdc)",
		"g.playlistFailureAction(i)",
		"g.playlistFailureKey(key)",
		"g.playlistLoadFailed = false\n\tg.loadEpoch++",
	} {
		if !strings.Contains(s, marker) {
			return "missing failure view transition: " + marker
		}
	}
	return ""
}

func TestFailedOrEmptyLoadCannotFallThroughToBlankLists(t *testing.T) {
	body, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	lf := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, tc := range []struct {
		name   string
		source string
	}{
		{"LF", lf},
		{"CRLF", strings.ReplaceAll(lf, "\n", "\r\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if problem := playlistFailureWiringError(tc.source); problem != "" {
				t.Fatal(problem)
			}
		})
	}
	// Normalizing line endings must not weaken the real empty-playlist guard.
	t.Run("missing empty-playlist guard is rejected", func(t *testing.T) {
		broken := strings.Replace(lf, "if task.note != \"\" || len(g.channels) == 0 {\n\t\t\t\tg.showPlaylistLoadFailure()",
			"if task.note != \"\" {\n\t\t\t\tg.showPlaylistLoadFailure()", 1)
		if playlistFailureWiringError(broken) == "" {
			t.Fatal("missing empty-playlist recovery must still fail the check")
		}
	})
	for _, file := range []string{"menu_hit_windows.go", "frame_transition_windows.go"} {
		body, err := os.ReadFile(file)
		if err != nil || !strings.Contains(string(body), "g.playlistLoadFailed") {
			t.Fatalf("%s must keep old playback/input layers away from the failure view", file)
		}
	}
}
