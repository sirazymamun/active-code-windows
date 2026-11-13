//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"image/png"
	"unsafe"
)

// Retained as a source asset for reproducible offline builds, then embedded
// directly in the single Windows executable. No logo file is needed at runtime.
//
//go:embed assets/tv2u-logo.png
var embeddedLogoPNG []byte

var stretchDIBits = gdi32win.NewProc("StretchDIBits")
var setStretchMode = gdi32win.NewProc("SetStretchBltMode")

type bitmapHeader struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, ImageSize uint32
	XPels, YPels           int32
	ClrUsed, ClrImportant  uint32
}

var logoBGRA []byte
var cornerLogoBGRA []byte
var logoHeader bitmapHeader

func initializeLogo() {
	imageData, err := png.Decode(bytes.NewReader(embeddedLogoPNG))
	if err != nil {
		return
	}
	b := imageData.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return
	}
	logoBGRA = make([]byte, width*height*4)
	cornerLogoBGRA = make([]byte, width*height*4)
	// Composite the transparent PNG onto the welcome-screen navy background.
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, blue, a := imageData.At(b.Min.X+x, b.Min.Y+y).RGBA()
			alpha := int(a >> 8)
			k := (y*width + x) * 4
			logoBGRA[k] = byte((int(blue>>8)*alpha + 27*(255-alpha)) / 255)
			logoBGRA[k+1] = byte((int(g>>8)*alpha + 16*(255-alpha)) / 255)
			logoBGRA[k+2] = byte((int(r>>8)*alpha + 10*(255-alpha)) / 255)
			logoBGRA[k+3] = 255
			// RGBA() already returns alpha-premultiplied colour components.
			// A black background is the layered overlay's transparent colour key.
			cornerLogoBGRA[k] = byte(blue >> 8)
			cornerLogoBGRA[k+1] = byte(g >> 8)
			cornerLogoBGRA[k+2] = byte(r >> 8)
			cornerLogoBGRA[k+3] = 255
		}
	}
	logoHeader = bitmapHeader{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
}

func drawCornerLogo(hdc uintptr, r rectF) bool {
	if len(cornerLogoBGRA) == 0 || r.w <= 0 || r.h <= 0 {
		return false
	}
	setStretchMode.Call(hdc, 4)
	out, _, _ := stretchDIBits.Call(hdc, uintptr(r.x), uintptr(r.y), uintptr(r.w), uintptr(r.h), 0, 0, uintptr(logoHeader.Width), uintptr(-logoHeader.Height), uintptr(unsafe.Pointer(&cornerLogoBGRA[0])), uintptr(unsafe.Pointer(&logoHeader)), 0, 0x00CC0020)
	return int32(out) > 0
}

func drawWelcomeLogo(hdc uintptr, r rectF) bool {
	if len(logoBGRA) == 0 || r.w <= 0 || r.h <= 0 {
		return false
	}
	setStretchMode.Call(hdc, 4) // HALFTONE
	out, _, _ := stretchDIBits.Call(hdc, uintptr(r.x), uintptr(r.y), uintptr(r.w), uintptr(r.h), 0, 0, uintptr(logoHeader.Width), uintptr(-logoHeader.Height), uintptr(unsafe.Pointer(&logoBGRA[0])), uintptr(unsafe.Pointer(&logoHeader)), 0, 0x00CC0020)
	return int32(out) > 0
}
