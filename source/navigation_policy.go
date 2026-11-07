package main

// Route navigation from every application child, including LibVLC video output.
// Character keys still pass through TranslateMessage for text search/input.
func isPlayerNavigationKey(key uintptr) bool {
	switch key {
	case 0x25, 0x26, 0x27, 0x28, 0x0D, 0x1B, 0x21, 0x22, 0x72, 0x7A:
		return true
	}
	return false
}

func navigateListFocus(current int, key uintptr) (focus int, commit bool) {
	if key == 0x25 {
		return 0, false
	}
	if key == 0x27 {
		return 1, current == 0
	}
	return current, false
}
