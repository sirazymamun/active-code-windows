//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsInstallPathRecognizesShortAndLongNames(t *testing.T) {
	root := t.TempDir()
	path, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 32768)
	n, err := syscall.GetLongPathName(path, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		t.Fatalf("GetLongPathName: length=%d, error=%v", n, err)
	}
	long := syscall.UTF16ToString(buf[:n])
	if !sameInstallPath(root, long) {
		t.Fatalf("temporary directory alias mismatch: %q vs %q", root, long)
	}
}

func TestWindowsDesktopShortcutUsesInstalledTargetAndTV2UIcon(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Programs বাংলা", "active-code.exe")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("MZ-test-target"), 0600); err != nil {
		t.Fatal(err)
	}
	shortcut := filepath.Join(root, "Desktop বাংলা", "active-code.lnk")
	if err := writeDesktopShortcut(shortcut, target); err != nil {
		t.Fatal(err)
	}
	if err := withInstallShellLink(func(link, persist uintptr) error {
		path, err := syscall.UTF16PtrFromString(shortcut)
		if err != nil {
			return err
		}
		status := installCOMCall(persist, 5, uintptr(unsafe.Pointer(path)), 0)
		runtime.KeepAlive(path)
		if err := installHRESULT(status, "Read saved shortcut"); err != nil {
			return err
		}
		buf := make([]uint16, 32768)
		// Request the usable filesystem path, not SLGP_RAWPATH, which may
		// contain environment variables or a different saved path spelling.
		status = installCOMCall(link, 3, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
		if err := installHRESULT(status, "Read shortcut target"); err != nil {
			return err
		}
		actual := syscall.UTF16ToString(buf)
		if status != 0 || !sameInstallPath(actual, target) {
			t.Fatalf("shortcut target mismatch: got %q, want %q, HRESULT=0x%08x", actual, target, uint32(status))
		}
		var index int32
		status = installCOMCall(link, 16, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&index)))
		if err := installHRESULT(status, "Read shortcut icon"); err != nil {
			return err
		}
		if !sameInstallPath(syscall.UTF16ToString(buf), target) || index != 0 {
			t.Fatalf("shortcut icon mismatch: got %q (index %d), want %q (index 0)", syscall.UTF16ToString(buf), index, target)
		}
		runtime.KeepAlive(buf)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(filepath.Dir(shortcut))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "active-code.lnk" {
		t.Fatal("Desktop contains an EXE instead of just shortcut")
	}
}
