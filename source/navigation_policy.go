package main

// Route navigation from every application child, including LibVLC video output.
// Character keys still pass through TranslateMessage for text search/input.
func isPlayerNavigationKey(key uintptr) bool {
	switch key {
	case 0x25, 0x26, 0x27, 0x28, 0x0D, 0x1B, 0xA6, 0x21, 0x22, 0x72, 0x7A:
		return true
	}
	return false
}

func navigateListFocus(current int, key uintptr) (focus int, commit bool) {
	if key == 0x25 {
		if current > 0 {
			return current - 1, false
		}
		return 0, false
	}
	if key == 0x27 {
		return minFocus(2, current+1), current == 0
	}
	return current, false
}

// advanceGroupCursor wraps the category cursor in either direction, even for
// multi-row jumps. A zero-length list returns 0 and cannot divide by zero.
func advanceGroupCursor(current, delta, size int) int {
	if size <= 0 {
		return 0
	}
	next := ((current+delta)%size + size) % size
	return next
}

func minFocus(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// At most one sequential row per visible repeat frame. Never multiply by the
// Windows repeat count: queued repeats otherwise starve WM_PAINT.
type navigationRepeat struct {
	key uintptr
	at  int64
}

func (r *navigationRepeat) allow(key uintptr, repeated bool, now int64) bool {
	if !repeated || key != r.key || now-r.at >= 100 {
		r.key, r.at = key, now
		return true
	}
	return false
}

// Position follows the focused row (including cyclic wrap), not wheel input.
func listProgress(total, index, visible int, height int32) (int32, int32) {
	if total <= 0 || height <= 0 {
		return 0, 0
	}
	if index < 0 {
		index = 0
	}
	if index >= total {
		index = total - 1
	}
	if visible < 1 {
		visible = 1
	}
	size := int32(int64(height) * int64(visible) / int64(total))
	if size < 18 {
		size = 18
	}
	if size > height {
		size = height
	}
	if total == 1 {
		return 0, size
	}
	return int32(int64(height-size) * int64(index) / int64(total-1)), size
}
