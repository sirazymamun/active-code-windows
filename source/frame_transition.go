package main

import "image"

type heldVideoFrame struct {
	width, height int32
	bgra          []byte
}

func makeHeldVideoFrame(picture image.Image) *heldVideoFrame {
	if picture == nil {
		return nil
	}
	b := picture.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 || b.Dx() > 1920 || b.Dy() > 1080 {
		return nil
	}
	f := &heldVideoFrame{width: int32(b.Dx()), height: int32(b.Dy()), bgra: make([]byte, b.Dx()*b.Dy()*4)}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, blue, _ := picture.At(b.Min.X+x, b.Min.Y+y).RGBA()
			k := (y*b.Dx() + x) * 4
			f.bgra[k], f.bgra[k+1], f.bgra[k+2], f.bgra[k+3] = byte(blue>>8), byte(g>>8), byte(r>>8), 255
		}
	}
	return f
}

// Decoder or Playing state alone is insufficient to uncover the new renderer.
func transitionCanReveal(displayed, playedAudio int32, videoKnown, videoExpected bool) bool {
	return displayed > 0 || (videoKnown && !videoExpected && playedAudio > 0)
}
