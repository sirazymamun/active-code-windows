//go:build windows

package main

// appicon_windows_amd64.syso embeds the same Android launcher in the executable
// as 16..256px ICO resources. Both Explorer and native windows use resource #1.
func loadApplicationIcons(module uintptr) (large, small uintptr) {
	load := user32win.NewProc("LoadImageW")
	large, _, _ = load.Call(module, 1, 1, 32, 32, 0x8000)
	small, _, _ = load.Call(module, 1, 1, 16, 16, 0x8000)
	return
}
