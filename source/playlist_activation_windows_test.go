//go:build windows

package main

import "testing"

func TestPlaylistMenuOpensActivationAndCancelRestoresPlayer(t *testing.T) {
	g := &nativeUI{active: true, codeBuffer: "old", activationError: "old error", width: 1920, height: 1080}
	g.action(1)
	if !g.active || !g.activationOpen || g.codeBuffer != "" || g.activationError != "" {
		t.Fatal("Playlist must open a fresh code wizard while retaining the current session")
	}
	g.handleCharacter('1')
	g.handleCharacter('2')
	g.handleCharacter('3')
	if g.codeBuffer != "123" {
		t.Fatal("wizard did not receive digit input")
	}
	g.handleKeys(keyBackspace)
	if g.codeBuffer != "12" {
		t.Fatal("wizard Backspace failed")
	}
	g.handleKeys(keyEnter)
	if g.activationError != "Enter exactly 10 digits" {
		t.Fatal("wizard did not validate the code")
	}
	g.handleKeys(keyEsc)
	if !g.active || g.activationOpen || !g.overlayVisible || g.codeBuffer != "" {
		t.Fatal("Esc must return to the existing player without deleting activation")
	}
	g.action(1)
	r := g.popupCloseRect()
	g.click(r.x+2, r.y+2)
	if !g.active || g.activationOpen {
		t.Fatal("close button must return to player")
	}
}

func TestFailedReplacementActivationPreservesExistingPlayer(t *testing.T) {
	g := &nativeUI{active: true, activationOpen: true, busy: true, checking: true,
		tasks: []uiTask{{kind: "auth", note: "Invalid code", preserveActive: true}}}
	g.processTasks()
	if !g.active || !g.activationOpen || g.checking || g.busy || g.activationError != "Invalid code" {
		t.Fatal("failed replacement code must not discard the previous active session")
	}
}

func TestSearchBackspaceRemovesWholeUnicodeCharacter(t *testing.T) {
	g := &nativeUI{active: true, searchVisible: true, searchText: "Aব"}
	g.handleKeys(keyBackspace)
	if g.searchText != "A" {
		t.Fatalf("Backspace left invalid text: %q", g.searchText)
	}
}

func TestOldPlaybackResultDoesNotReplaceCurrentChannelStatus(t *testing.T) {
	g := &nativeUI{active: true, playRequest: 2, tasks: []uiTask{
		{kind: "play", success: true, note: "Current channel playing", requestID: 2},
		{kind: "play", note: "Old channel failed", requestID: 1},
	}}
	g.processTasks()
	if g.status != "Current channel playing" {
		t.Fatal("stale channel result replaced current playback")
	}
}

func TestRevocationDropsQueuedChannelChangesWithoutBlockingUI(t *testing.T) {
	g := &nativeUI{playRequest: 1, vlcCh: make(chan vlcRequest, 1)}
	g.vlcCh <- vlcRequest{item: channel{URL: "https://example.test/old"}, requestID: 1}
	g.stopVLC()
	if cmd := <-g.vlcCh; !cmd.stop {
		t.Fatal("revocation did not supersede queued playback")
	}
	if g.playRequest != 2 {
		t.Fatal("revocation did not invalidate stale callbacks")
	}
}

func TestSuccessfulVerificationDismissesWizardBeforePlaylistDownloadEnds(t *testing.T) {
	g := &nativeUI{activationOpen: true, busy: true, checking: true, tasks: []uiTask{{kind: "authorized"}}}
	g.processTasks()
	if !g.active || g.activationOpen || g.checking || !g.busy || g.status != "Loading! Please wait." {
		t.Fatal("approved activation must show loading while playlist downloads")
	}
}
