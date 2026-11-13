package main

import "testing"

func TestAdvanceGroupCursorWrapsWithoutSwallowingRemote(t *testing.T) {
	cases := []struct{ cur, delta, n, want int }{{0, -1, 18, 17}, {17, 1, 18, 0}, {4, 1, 18, 5}, {0, -21, 18, 15}, {0, 1, 0, 0}}
	for _, c := range cases {
		if got := advanceGroupCursor(c.cur, c.delta, c.n); got != c.want {
			t.Errorf("advanceGroupCursor(%d,%d,%d)=%d; want %d", c.cur, c.delta, c.n, got, c.want)
		}
	}
}

func TestFullFocusPathKeepsSelectionSeparate(t *testing.T) {
	for _, c := range []struct {
		from   int
		key    uintptr
		want   int
		commit bool
	}{
		{0, 0x27, 1, true}, {1, 0x27, 2, false}, {2, 0x25, 1, false}, {1, 0x25, 0, false},
	} {
		f, commit := navigateListFocus(c.from, c.key)
		if f != c.want || commit != c.commit {
			t.Fatalf("focus %d key %x: %d %v", c.from, c.key, f, commit)
		}
	}
}

func TestHeldGroupArrowShowsSequentialRowsAndWraps(t *testing.T) {
	var gate navigationRepeat
	cursor := 0
	accepted := 0
	for ms := int64(0); ms <= 1000; ms += 10 {
		if gate.allow(0x28, ms != 0, ms) {
			cursor = advanceGroupCursor(cursor, 1, 7)
			accepted++
		}
	}
	if accepted != 11 || cursor != 4 {
		t.Fatalf("repeat skipped sequence: %d %d", accepted, cursor)
	}
	if !gate.allow(0x26, true, 1001) {
		t.Fatal("direction change swallowed")
	}
	if !gate.allow(0x28, false, 1002) {
		t.Fatal("separate press swallowed")
	}
}

func TestProgressFollowsFocusAtStartMiddleEndAndWrap(t *testing.T) {
	var previous int32 = -1
	for index := 0; index < 100; index++ {
		offset, size := listProgress(100, index, 10, 500)
		if size != 50 || offset < previous || offset+size > 500 {
			t.Fatalf("bad indicator %d: %d %d", index, offset, size)
		}
		previous = offset
	}
	if offset, size := listProgress(100, 99, 10, 500); offset+size != 500 {
		t.Fatal("end must reach bottom")
	}
	if offset, _ := listProgress(100, advanceGroupCursor(99, 1, 100), 10, 500); offset != 0 {
		t.Fatal("wrap must return to top")
	}
	if _, size := listProgress(0, 0, 10, 500); size != 0 {
		t.Fatal("empty list")
	}
	if offset, size := listProgress(1, 0, 10, 12); offset != 0 || size != 12 {
		t.Fatal("small track overflow")
	}
}
