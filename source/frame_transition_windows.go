//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var heldFrameCallback uintptr
var positionHeldFrame = user32win.NewProc("SetWindowPos")

func (g *nativeUI) showHeldFrame(frame *heldVideoFrame) bool {
	if frame == nil {
		return false
	}
	if g.frameHwnd == 0 {
		if heldFrameCallback == 0 {
			heldFrameCallback = syscall.NewCallback(heldFrameProcedure)
		}
		module, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
		name := winUTF16("ActiveCodeHeldVideoFrameV518")
		def := wndClass{Size: uint32(unsafe.Sizeof(wndClass{})), WndProc: heldFrameCallback, Instance: module, Name: name}
		atom, _, _ := winRegister.Call(uintptr(unsafe.Pointer(&def)))
		if atom == 0 {
			return false
		}
		// Opaque popup above VLC but below the transparent controls. Transparent
		// hit testing and NOACTIVATE preserve navigation during the transition.
		g.frameHwnd, _, _ = winCreate.Call(uintptr(wndExToolWindow|0x08000000|0x20), uintptr(unsafe.Pointer(name)), 0, uintptr(wndPopup), 0, 0, uintptr(g.width), uintptr(g.height), g.hwnd, 0, module, 0)
		if g.frameHwnd == 0 {
			return false
		}
	}
	g.heldFrame = frame
	g.syncHeldFrame()
	winInvalidate.Call(g.frameHwnd, 0, 0)
	user32win.NewProc("UpdateWindow").Call(g.frameHwnd)
	return true
}
func (g *nativeUI) syncHeldFrame() {
	if g.frameHwnd == 0 {
		return
	}
	if g.heldFrame == nil || !g.active || g.activationOpen || g.playlistLoadFailed {
		winShow.Call(g.frameHwnd, hideWindow)
		return
	}
	positionHeldFrame.Call(g.frameHwnd, g.overlayHwnd, 0, 0, uintptr(g.width), uintptr(g.height), 0x0010|0x0040)
}
func (g *nativeUI) clearHeldFrame() {
	if g.frameHwnd != 0 {
		winShow.Call(g.frameHwnd, hideWindow)
	}
	g.heldFrame = nil
}
func heldFrameProcedure(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == 0x0084 {
		return ^uintptr(0)
	} // HTTRANSPARENT
	if message == msgPaint && mainUI != nil {
		var paint winPaint
		dc, _, _ := winBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		g := mainUI
		f := g.heldFrame
		if dc != 0 && f != nil && len(f.bgra) > 0 {
			header := bitmapHeader{Size: 40, Width: f.width, Height: -f.height, Planes: 1, BitCount: 32}
			setStretchMode.Call(dc, 4)
			stretchDIBits.Call(dc, 0, 0, uintptr(g.width), uintptr(g.height), 0, 0, uintptr(f.width), uintptr(f.height), uintptr(unsafe.Pointer(&f.bgra[0])), uintptr(unsafe.Pointer(&header)), 0, 0x00CC0020)
		}
		winEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		return 0
	}
	return defaultWnd(hwnd, message, wParam, lParam)
}
