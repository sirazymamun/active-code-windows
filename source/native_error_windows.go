//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func showError(s string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	body, _ := syscall.UTF16PtrFromString(s)
	title, _ := syscall.UTF16PtrFromString("active-code v5.1.8")
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x40)
}
