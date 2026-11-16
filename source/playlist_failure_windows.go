//go:build windows

package main

import "time"

// Loading failure is a persistent view, not a timed status over an empty library.
func (g *nativeUI) showPlaylistLoadFailure() {
	if g.vlcCh != nil {
		g.stopVLC()
	} else {
		g.stopStartupLoading(false)
		g.clearHeldFrame()
	}
	g.playlistLoadFailed = true
	g.playlistFailureFocus = 0
	g.busy, g.checking = false, false
	g.reloadInProgress = false
	g.showLists, g.searchVisible, g.audioOpen, g.infoOpen = false, false, false, false
	g.overlayVisible = true
	g.overlayDeadline = time.Time{}
	g.overlayTimerArmed = false
	g.bottomStatus = ""
	g.bottomStatusEpoch++
	g.playingItem = channel{}
	g.status = playlistFailureMessage
	g.fitChildren()
	g.repaint()
}

func (g *nativeUI) playlistFailureButton(index int) rectF {
	// Stack buttons on smaller windows without changing the centred text.
	width := min32(320, max32(120, g.width-40))
	height := max32(40, min32(62, g.height/14))
	gap := max32(12, height/4)
	top := g.height/2 + max32(46, min32(72, g.height/10))
	return rectF{(g.width - width) / 2, top + int32(index)*(height+gap), width, height}
}

func (g *nativeUI) drawPlaylistLoadFailure(dc uintptr) {
	fill(dc, rectF{0, 0, g.width, g.height}, rgb(10, 16, 27))
	titleY := g.height/2 - max32(100, min32(140, g.height/5))
	line := max32(28, min32(42, g.height/22))
	centerLabel(dc, "Playlist Loading Failed",
		rectF{20, titleY, g.width - 40, line + 20}, g.fontLarge, rgb(235, 245, 248))
	centerLabel(dc, "Your subscription may have expired.",
		rectF{16, titleY + line + 36, g.width - 32, line}, g.fontNormal, rgb(172, 190, 201))
	centerLabel(dc, "Please contact your subscription vendor.",
		rectF{16, titleY + 2*line + 36, g.width - 32, line}, g.fontNormal, rgb(172, 190, 201))
	for i, text := range []string{"Retry playlist", "Enter activation code"} {
		r := g.playlistFailureButton(i)
		background, foreground := rgb(26, 45, 60), rgb(228, 240, 243)
		if i == g.playlistFailureFocus {
			background, foreground = rgb(82, 203, 229), rgb(10, 16, 27)
		}
		roundedFill(dc, r, 16, background)
		centerLabel(dc, text, r, g.fontNormal, foreground)
	}
}

func (g *nativeUI) playlistFailureAction(index int) {
	if g.busy || g.checking {
		return
	}
	if index == 0 {
		g.beginLoad("")
	} else {
		g.openActivation()
	}
}

func (g *nativeUI) playlistFailureKey(key uintptr) {
	switch key {
	case keyLeft, keyRight, keyUp, keyDown, 0x09: // arrows and Tab
		g.playlistFailureFocus = 1 - g.playlistFailureFocus
		g.repaint()
	case keyEnter, 0x20:
		g.playlistFailureAction(g.playlistFailureFocus)
	case keyEsc:
		g.openActivation()
	}
}
