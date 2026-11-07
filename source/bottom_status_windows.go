//go:build windows

package main

import "time"

const msgBottomStatusHide = 0x8004

func (g *nativeUI) bottomStatusVisible() bool {
	return g.bottomStatus != "" && (g.bottomStatusUntil.IsZero() || time.Now().Before(g.bottomStatusUntil))
}
func (g *nativeUI) showBottomStatus(message string, duration time.Duration) {
	g.bottomStatus = message
	g.bottomStatusEpoch++
	g.bottomStatusUntil = time.Time{}
	if duration > 0 {
		g.bottomStatusUntil = time.Now().Add(duration)
		epoch := g.bottomStatusEpoch
		time.AfterFunc(duration, func() { winPostMessage.Call(g.hwnd, msgBottomStatusHide, uintptr(epoch), 0) })
	}
	g.fitChildren()
	g.repaint()
}
func (g *nativeUI) drawBottomStatus(dc uintptr) {
	if !g.bottomStatusVisible() {
		return
	}
	// Leave the logo's lower-right corner free. A temporary notice is independent
	// of the list overlay and survives immediate playback/status updates.
	h := max32(40, min32(68, g.height/18))
	w := min32(g.width-32, max32(300, g.width*78/100))
	r := rectF{(g.width - w) / 2, g.height - h - 14, w, h}
	roundedFill(dc, r, 8, rgb(12, 29, 36))
	centerLabel(dc, g.bottomStatus, rectF{r.x + 12, r.y, r.w - 24, r.h}, g.fontNormal, rgb(235, 245, 248))
}
