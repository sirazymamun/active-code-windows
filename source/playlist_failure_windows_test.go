//go:build windows

package main

import (
	"testing"
	"time"
)

func TestPlaylistFailureReplacesEmptyLibraryAndStopsAutoHide(t *testing.T) {
	g := &nativeUI{
		active: true, busy: true, checking: true,
		firstContentPending: true, everPresented: true,
		showLists: true, searchVisible: true, infoOpen: true, audioOpen: true,
		reloadInProgress: true, overlayTimerArmed: true,
		overlayDeadline: time.Now().Add(time.Second),
		bottomStatus:    "Reloading playlist…",
	}
	g.showPlaylistLoadFailure()
	if !g.playlistLoadFailed || g.firstContentPending || g.busy || g.checking {
		t.Fatal("failure must replace loading and preserve a visible recovery state")
	}
	if !g.active || g.activationOpen {
		t.Fatal("provider loading failure must preserve the app activation")
	}
	if g.showLists || g.searchVisible || g.infoOpen || g.audioOpen {
		t.Fatal("empty library and unrelated panels must not cover the failure view")
	}
	if !g.overlayDeadline.IsZero() || g.overlayTimerArmed || g.bottomStatus != "" {
		t.Fatal("failure must not expire or disappear behind a transient status")
	}
	g.armOverlayHide()
	if !g.overlayDeadline.IsZero() {
		t.Fatal("pointer motion must not arm failure screen auto-hide")
	}
}

func TestPlaylistFailureControlsStayCentredAndSupportKeyboard(t *testing.T) {
	for _, size := range [][2]int32{{640, 360}, {1366, 768}, {1920, 1080}} {
		g := &nativeUI{width: size[0], height: size[1], active: true, playlistLoadFailed: true}
		first, second := g.playlistFailureButton(0), g.playlistFailureButton(1)
		for _, button := range []rectF{first, second} {
			if button.x < 0 || button.y < 0 || button.x+button.w > g.width || button.y+button.h > g.height {
				t.Fatalf("recovery button outside %v: %+v", size, button)
			}
			if delta := (button.x*2 + button.w) - g.width; delta < -1 || delta > 1 {
				t.Fatal("recovery button is not horizontally centred")
			}
		}
		if second.y <= first.y+first.h {
			t.Fatal("recovery buttons overlap")
		}
		g.handleKeys(keyRight)
		if g.playlistFailureFocus != 1 {
			t.Fatal("keyboard must reach activation button")
		}
		g.handleKeys(keyLeft)
		if g.playlistFailureFocus != 0 {
			t.Fatal("keyboard must return to Retry")
		}
	}
}
