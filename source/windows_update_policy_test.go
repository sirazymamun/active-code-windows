package main

import (
	"strings"
	"testing"
	"time"
)

func TestRecordingGetsFullStartupBudget(t *testing.T) {
	start := time.Unix(100, 0)
	for _, delay := range []time.Duration{0, 8 * time.Second, 12 * time.Second, 19 * time.Second} {
		if recordingHasTimedOut(start, start.Add(delay), 0) {
			t.Fatalf("recording aborted early at %s", delay)
		}
	}
	if !recordingHasTimedOut(start, start.Add(20*time.Second), 0) {
		t.Fatal("empty recording can wait forever")
	}
	if recordingHasTimedOut(start, start.Add(time.Hour), 188) {
		t.Fatal("valid recording stopped for lack of data")
	}
}
func TestRecordingOptionsPreserveNativePathsAndSameInput(t *testing.T) {
	opts := nativeRecordingOptions(`C:\Users\O'Neil\Videos\বাংলা test.ts`)
	joined := strings.Join(opts, "\n")
	for _, want := range []string{`#duplicate{dst=display,dst=std{access=file{append,overwrite},mux=ts,`, `C:/Users/O'Neil/Videos/বাংলা test.ts`, ":sout-mux-caching=200", ":sout-file-append"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("recording option missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "http://") || strings.Contains(joined, "https://") {
		t.Fatal("recording opened a second provider input")
	}
}
func TestOwnerRoutesAllListNavigationKeys(t *testing.T) {
	for _, key := range []uintptr{0x25, 0x26, 0x27, 0x28, 0x0D, 0x1B, 0x21, 0x22} {
		if !isPlayerNavigationKey(key) {
			t.Fatalf("navigation key swallowed: %x", key)
		}
	}
	for _, key := range []uintptr{'A', '1', 0x20} {
		if isPlayerNavigationKey(key) {
			t.Fatalf("character key intercepted: %x", key)
		}
	}
}

func TestCrossColumnNavigationPreservesChannelCursor(t *testing.T) {
	if focus, commit := navigateListFocus(1, 0x25); focus != 0 || commit {
		t.Fatal("Left must enter groups without reloading channels")
	}
	if focus, commit := navigateListFocus(0, 0x27); focus != 1 || !commit {
		t.Fatal("Right must commit selected group and enter channels")
	}
	if focus, commit := navigateListFocus(1, 0x27); focus != 2 || commit {
		t.Fatal("Right in channels must preserve the current cursor")
	}
}
func TestInformationLayoutHasCompactReadableRows(t *testing.T) {
	for _, screen := range [][2]int32{{1280, 720}, {1366, 768}, {1920, 1080}, {3840, 2160}} {
		w, h, row, top := streamInfoPanelMetrics(screen[0], screen[1], 14)
		font := max(int32(20), min(int32(42), screen[1]/38))
		if row != font+2 || w > screen[0] || h > screen[1] || top+14*row > h {
			t.Fatalf("bad info layout at %v", screen)
		}
	}
}
