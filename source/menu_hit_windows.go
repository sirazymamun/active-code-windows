//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// A transparent glyph's hollow centre is normally click-through on a layered
// window. Invisible alpha=1 hit windows preserve the complete menu click area
// without painting visible button tiles behind the symbols.
var menuHitCallback uintptr
var videoClickCallback uintptr

func (g *nativeUI) createMenuHitWindows(module, cursor uintptr) {
	if videoClickCallback == 0 {
		videoClickCallback = syscall.NewCallback(videoClickProcedure)
	}
	videoClass := winUTF16("ActiveCodeVideoClickCaptureV518")
	videoDef := wndClass{Size: uint32(unsafe.Sizeof(wndClass{})), WndProc: videoClickCallback, Instance: module, Cursor: cursor, Name: videoClass}
	videoAtom, _, _ := winRegister.Call(uintptr(unsafe.Pointer(&videoDef)))
	if videoAtom != 0 {
		g.videoClickHwnd, _, _ = winCreate.Call(uintptr(wndExLayered|wndExToolWindow|0x08000000), uintptr(unsafe.Pointer(videoClass)), 0, uintptr(wndPopup), 0, 0, uintptr(g.width), uintptr(g.height), g.hwnd, 0, module, 0)
		if g.videoClickHwnd != 0 {
			winLayerAttrs.Call(g.videoClickHwnd, 0, 1, uintptr(layerAlpha))
		}
	}
	if menuHitCallback == 0 {
		menuHitCallback = syscall.NewCallback(menuHitProcedure)
	}
	name := winUTF16("ActiveCodeInvisibleMenuHitV518")
	class := wndClass{Size: uint32(unsafe.Sizeof(wndClass{})), WndProc: menuHitCallback, Instance: module, Cursor: cursor, Name: name}
	atom, _, _ := winRegister.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return
	}
	g.menuHits = make(map[uintptr]int)
	for i := 0; i < 5; i++ {
		r := g.menuRect(i)
		hwnd, _, _ := winCreate.Call(uintptr(wndExLayered|wndExToolWindow|0x08000000), uintptr(unsafe.Pointer(name)), 0, uintptr(wndPopup), uintptr(r.x), uintptr(r.y), uintptr(r.w), uintptr(r.h), g.hwnd, 0, module, 0)
		if hwnd != 0 {
			g.menuHits[hwnd] = i
			winLayerAttrs.Call(hwnd, 0, 1, uintptr(layerAlpha))
		}
	}
}
func (g *nativeUI) syncMenuHitWindows() {
	if g.videoClickHwnd != 0 {
		if g.active && !g.activationOpen && !(g.busy && len(g.channels) == 0) {
			winMove.Call(g.videoClickHwnd, 0, 0, uintptr(g.width), uintptr(g.height), 1)
			winShow.Call(g.videoClickHwnd, 4)
		} else {
			winShow.Call(g.videoClickHwnd, hideWindow)
		}
	}
	visible := g.active && g.overlayVisible && !g.firstContentPending && !g.activationOpen && !g.infoOpen && !g.audioOpen && !g.searchVisible && !(g.busy && len(g.channels) == 0)
	for hwnd, index := range g.menuHits {
		if visible {
			r := g.menuRect(index)
			winMove.Call(hwnd, uintptr(r.x), uintptr(r.y), uintptr(r.w), uintptr(r.h), 1)
			winShow.Call(hwnd, 4)
		} else {
			winShow.Call(hwnd, hideWindow)
		}
	}
}

func videoClickProcedure(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	g := mainUI
	if g == nil {
		return defaultWnd(hwnd, message, wParam, lParam)
	}
	if message == msgPaint {
		var paint winPaint
		dc, _, _ := winBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		if dc != 0 {
			fill(dc, rectF{0, 0, g.width, g.height}, rgb(1, 1, 1))
		}
		winEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		return 0
	}
	// Use the same input handler over both transparent video and visible lists.
	return overlayWindowProcedure(hwnd, message, wParam, lParam)
}
func menuHitProcedure(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	g := mainUI
	if g == nil {
		return defaultWnd(hwnd, message, wParam, lParam)
	}
	index, ok := g.menuHits[hwnd]
	if !ok {
		return defaultWnd(hwnd, message, wParam, lParam)
	}
	switch message {
	case msgPaint:
		var paint winPaint
		dc, _, _ := winBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		r := g.menuRect(index)
		if dc != 0 {
			fill(dc, rectF{0, 0, r.w, r.h}, rgb(1, 1, 1))
		}
		winEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		return 0
	case msgLUp:
		if g.active && g.overlayVisible && !g.activationOpen && !g.infoOpen && !g.audioOpen {
			g.action(index)
			winSetFocus.Call(g.hwnd)
		}
		return 0
	case msgMouseMove:
		r := g.menuRect(index)
		if g.meaningfulMouseMove(r.x+lowSigned(lParam), r.y+highSigned(lParam)) {
			g.armOverlayHide()
		}
		return 0
	}
	return defaultWnd(hwnd, message, wParam, lParam)
}
