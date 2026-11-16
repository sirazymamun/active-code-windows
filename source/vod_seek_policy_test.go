package main

import (
	"math"
	"testing"
	"time"
)

func TestVodArrowsOnlySeekPlayableMoviesAndEpisodes(t *testing.T) {
	cases := []struct {
		item  channel
		lists bool
		key   uintptr
		want  bool
	}{
		{channel{Type: "Movies", URL: "file:///movie.mkv"}, false, 0x27, true},
		{channel{Type: "Series", URL: "file:///episode.mkv"}, false, 0x25, true},
		{channel{Type: "Live TV", Group: "Movies", URL: "https://example.test/live"}, false, 0x27, false},
		{channel{Type: "Series", URL: "https://example.test/series", SeriesID: "77"}, false, 0x27, false},
		{channel{Type: "Movies", URL: "file:///movie.mp4"}, true, 0x27, false},
		{channel{Type: "Movies", URL: "file:///movie.mp4"}, false, 0x26, false},
		{channel{Type: "Movies"}, false, 0x27, false},
	}
	for _, c := range cases {
		if got := canUseVodArrow(c.item, c.lists, c.key); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}
func TestVodHoldMeasuresTimeIndependentlyOfKeyboardRepeat(t *testing.T) {
	start := time.Unix(100, 0)
	h := newVodSeekHold(0x27, 5, 9, start)
	for _, c := range []struct {
		elapsed time.Duration
		want    int64
	}{{0, 10}, {399 * time.Millisecond, 10}, {400 * time.Millisecond, 20}, {649 * time.Millisecond, 20}, {650 * time.Millisecond, 30}, {2 * time.Second, 80}, {5 * time.Second, 200}} {
		h.advance(start.Add(c.elapsed))
		if h.seconds != c.want {
			t.Fatalf("hold %s: %ds, want %ds", c.elapsed, h.seconds, c.want)
		}
	}
	before := h.seconds
	if h.advance(start.Add(5*time.Second)) || h.seconds != before {
		t.Fatal("duplicate key/timer event changed target")
	}
	h.active = false
	if h.advance(start.Add(6 * time.Second)) {
		t.Fatal("released key continued scrubbing")
	}
}
func TestVodRewindHoldAndBounds(t *testing.T) {
	start := time.Unix(100, 0)
	h := newVodSeekHold(0x25, 1, 1, start)
	h.advance(start.Add(2 * time.Second))
	if h.seconds != -80 {
		t.Fatal("rewind sign")
	}
	for _, c := range []struct{ pos, delta, duration, want int64 }{{5000, -10, 60000, 0}, {10000, 10, 60000, 20000}, {50000, 100, 60000, 59000}, {0, 10, 500, 0}, {10000, 10, -1, 20000}, {math.MaxInt64 - 10, math.MaxInt64, 60000, 59000}, {100, math.MinInt64, 60000, 0}} {
		if got := boundedVodSeek(c.pos, c.delta, c.duration); got != c.want {
			t.Errorf("%+v: got %d", c, got)
		}
	}
}
func TestVodPreviewUsesOneGestureOriginAndFinalReleaseTarget(t *testing.T) {
	var p vodSeekPlan
	now := time.Unix(100, 0)
	if p.target(1, 1, 100000, 10, 3600000, now) != 110000 {
		t.Fatal("tap")
	}
	p.applied(110000, now)
	if p.target(1, 1, 111000, 80, 3600000, now.Add(2*time.Second)) != 180000 {
		t.Fatal("hold compounded against changing decoder position")
	}
	if p.target(1, 1, 112000, 80, 3600000, now.Add(2100*time.Millisecond)) != 180000 {
		t.Fatal("release changed preview target")
	}
}
func TestRapidTapsAccumulateAndNewContentNeverInheritsOldTarget(t *testing.T) {
	var p vodSeekPlan
	now := time.Unix(100, 0)
	first := p.target(1, 1, 10000, 10, 3600000, now)
	p.applied(first, now)
	next := p.target(1, 2, 10010, 10, 3600000, now.Add(100*time.Millisecond))
	if next != 30000 {
		t.Fatal("rapid second tap lost while VLC clock caught up")
	}
	p.applied(next, now.Add(100*time.Millisecond))
	if got := p.target(2, 3, 1000, 10, 3600000, now.Add(150*time.Millisecond)); got != 11000 {
		t.Fatal("old movie seek leaked into new content")
	}
}
func TestSeekTimeLabel(t *testing.T) {
	for ms, want := range map[int64]string{-1: "00:00", 10000: "00:10", 80000: "01:20", 3601000: "1:00:01"} {
		if got := seekTimeLabel(ms); got != want {
			t.Errorf("%d: %s", ms, got)
		}
	}
}
