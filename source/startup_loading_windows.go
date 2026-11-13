//go:build windows

package main

import (
	"math"
)

const loadingAnimationTimer = 3

var winSetTimer = user32win.NewProc("SetTimer")
var winKillTimer = user32win.NewProc("KillTimer")

func (g *nativeUI) startStartupLoading() {
	if g.everPresented {
		return
	}
	g.firstContentPending = true
	if g.hwnd != 0 {
		winSetTimer.Call(g.hwnd, loadingAnimationTimer, 80, 0)
	}
	g.repaint()
}
func (g *nativeUI) stopStartupLoading(presented bool) {
	if presented {
		g.everPresented = true
	}
	g.firstContentPending = false
	if g.hwnd != 0 {
		winKillTimer.Call(g.hwnd, loadingAnimationTimer)
	}
}
func (g *nativeUI) animateLoading() {
	if !g.firstContentPending {
		return
	}
	g.loadingPhase = (g.loadingPhase + 1) % 12
	g.repaint()
}
func (g *nativeUI) drawStartupLoading(dc uintptr) {
	fill(dc, rectF{0, 0, g.width, g.height}, rgb(10, 16, 27))
	radius := max32(20, min32(38, g.height/27))
	dot := max32(5, radius/5)
	cx, cy := g.width/2, g.height/2-26
	for i := 0; i < 12; i++ {
		angle := float64(i) * 2 * math.Pi / 12
		x := cx + int32(math.Cos(angle)*float64(radius))
		y := cy + int32(math.Sin(angle)*float64(radius))
		distance := (i - g.loadingPhase + 12) % 12
		shade := byte(55 + (12-distance)*15)
		brush, _, _ := winCreateBrush.Call(uintptr(rgb(20, shade, shade)))
		pen, _, _ := winCreatePen.Call(0, 1, uintptr(rgb(20, shade, shade)))
		oldBrush, _, _ := winSelect.Call(dc, brush)
		oldPen, _, _ := winSelect.Call(dc, pen)
		winEllipse.Call(dc, uintptr(x-dot), uintptr(y-dot), uintptr(x+dot), uintptr(y+dot))
		winSelect.Call(dc, oldPen)
		winSelect.Call(dc, oldBrush)
		winDelete.Call(pen)
		winDelete.Call(brush)
	}
	centerLabel(dc, "Loading! Please wait.", rectF{12, cy + radius + 18, g.width - 24, 52}, g.fontLarge, rgb(235, 245, 248))
}
