package main

import (
	"image"
	"image/color"
	"testing"
)

func TestFrameTransitionWaitsForPresentedVideo(t *testing.T) {
	cases := []struct {
		displayed, audio   int32
		known, video, want bool
	}{
		{0, 0, false, false, false}, {0, 42, false, false, false}, {0, 42, true, true, false},
		{0, 42, true, false, true}, {1, 0, false, false, true}, {1, 0, true, true, true},
	}
	for _, c := range cases {
		if got := transitionCanReveal(c.displayed, c.audio, c.known, c.video); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}
func TestHeldFrameOwnsTopDownPixelsAndHonorsNonzeroBounds(t *testing.T) {
	im := image.NewRGBA(image.Rect(5, 7, 7, 9))
	im.Set(5, 7, color.RGBA{R: 255, A: 255})
	im.Set(6, 8, color.RGBA{B: 255, A: 255})
	f := makeHeldVideoFrame(im)
	if f == nil || f.width != 2 || f.height != 2 || len(f.bgra) != 16 {
		t.Fatal("frame dimensions")
	}
	if f.bgra[2] != 255 || f.bgra[0] != 0 || f.bgra[12] != 255 || f.bgra[14] != 0 {
		t.Fatal("flipped or swapped video pixels")
	}
	im.Set(5, 7, color.Black)
	if f.bgra[2] != 255 {
		t.Fatal("frame did not retain independent pixels")
	}
	if makeHeldVideoFrame(nil) != nil || makeHeldVideoFrame(image.NewRGBA(image.Rect(0, 0, 1921, 1))) != nil {
		t.Fatal("unbounded/empty frame accepted")
	}
}
