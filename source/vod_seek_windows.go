//go:build windows

package main

import (
	"strings"
	"sync/atomic"
	"time"
)

const vodSeekTimer = 4

var asyncKeyState = user32win.NewProc("GetAsyncKeyState")

func (g *nativeUI) sendVodSeek(preview, commit bool) bool {
	if !g.seekHold.active || g.seekHold.requestID != atomic.LoadUint64(&g.playRequest) {
		return false
	}
	cmd := vlcRequest{seek: true, seekPreview: preview, seekCommit: commit, seekSeconds: g.seekHold.seconds, seekToken: g.seekHold.token, requestID: g.seekHold.requestID}
	select {
	case g.vlcCh <- cmd:
		return true
	default:
		if !preview {
			g.showBottomStatus("Player busy; please retry seeking", 2*time.Second)
		}
		return false
	}
}
func (g *nativeUI) beginVodSeek(key uintptr) {
	if g.seekHold.active && g.seekHold.key == key {
		return
	} // Ignore OS repeat; the timer measures actual hold duration.
	g.finishVodSeek(true)
	g.seekToken++
	g.seekHold = newVodSeekHold(key, atomic.LoadUint64(&g.playRequest), g.seekToken, time.Now())
	if !g.sendVodSeek(false, false) {
		g.seekHold.active = false
		return
	}
	if g.hwnd != 0 {
		winSetTimer.Call(g.hwnd, vodSeekTimer, 100, 0)
	}
}
func (g *nativeUI) tickVodSeek() {
	if !g.seekHold.active {
		return
	}
	if g.activationOpen || g.searchVisible || g.infoOpen || g.audioOpen || (g.showLists && g.overlayVisible) {
		g.finishVodSeek(true)
		return
	}
	down, _, _ := asyncKeyState.Call(g.seekHold.key)
	if g.seekHold.requestID != atomic.LoadUint64(&g.playRequest) {
		g.finishVodSeek(false)
		return
	}
	if down&0x8000 == 0 {
		g.finishVodSeek(true)
		return
	}
	if g.seekHold.advance(time.Now()) {
		g.sendVodSeek(true, false)
	}
}
func (g *nativeUI) finishVodSeek(commit bool) {
	if !g.seekHold.active {
		return
	}
	if commit && g.seekHold.requestID == atomic.LoadUint64(&g.playRequest) {
		g.seekHold.advance(time.Now())
		// Tap already sought once; a hold commits only one final target on release.
		if g.seekHold.seconds > vodSeekStepSeconds || g.seekHold.seconds < -vodSeekStepSeconds {
			g.sendVodSeek(false, true)
		}
	}
	g.seekHold.active = false
	if !commit {
		g.seekToken++
	}
	if g.hwnd != 0 {
		winKillTimer.Call(g.hwnd, vodSeekTimer)
	}
	if !commit && strings.HasPrefix(g.bottomStatus, "Seek ") {
		g.showVodSeekNotice("", 0)
	}
}
func (g *nativeUI) releaseVodSeek(key uintptr) bool {
	if !g.seekHold.active || g.seekHold.key != key {
		return false
	}
	g.finishVodSeek(true)
	return true
}

// Scrubbing updates only overlay pixels/opacity: never MoveWindow on the VLC
// video surface for each preview tick, and never steal keyboard focus.
func (g *nativeUI) showVodSeekNotice(message string, duration time.Duration) {
	g.bottomStatus = message
	g.bottomStatusEpoch++
	g.bottomStatusUntil = time.Time{}
	if duration > 0 {
		g.bottomStatusUntil = time.Now().Add(duration)
		epoch := g.bottomStatusEpoch
		time.AfterFunc(duration, func() { winPostMessage.Call(g.hwnd, msgBottomStatusHide, uintptr(epoch), 0) })
	}
	if g.overlayHwnd != 0 && g.active && !g.activationOpen {
		alpha := uintptr(overlayHiddenAlpha)
		if g.overlayVisible || g.audioOpen || g.firstContentPending || g.bottomStatusVisible() {
			alpha = uintptr(overlayVisibleAlpha)
		}
		winLayerAttrs.Call(g.overlayHwnd, uintptr(rgb(0, 0, 0)), alpha, uintptr(layerColorKey|layerAlpha))
		winShow.Call(g.overlayHwnd, 4)
	}
	g.repaintOverlay()
}
