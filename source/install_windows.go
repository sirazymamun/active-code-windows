//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

type installGUID struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}

var (
	installOLE            = syscall.NewLazyDLL("ole32.dll")
	installShell          = syscall.NewLazyDLL("shell32.dll")
	installClassShellLink = installGUID{0x00021401, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	installIIDShellLinkW  = installGUID{0x000214f9, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	installIIDPersistFile = installGUID{0x0000010b, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	installDesktopID      = installGUID{0xb4bfcc3a, 0xdb2c, 0x424c, [8]byte{0xb0, 0x29, 0x7f, 0xe9, 0x9a, 0x87, 0xc6, 0x41}}
)

func installHRESULT(value uintptr, operation string) error {
	if int32(value) < 0 {
		return fmt.Errorf("%s failed (0x%08x)", operation, uint32(value))
	}
	return nil
}

//go:uintptrescapes
func installCOMCall(object uintptr, index int, args ...uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(object))
	method := *(*uintptr)(unsafe.Pointer(vtable + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	all := append([]uintptr{object}, args...)
	result, _, _ := syscall.SyscallN(method, all...)
	return result
}

func installWideString(p *uint16) string {
	if p == nil {
		return ""
	}
	var n int
	for n < 32768 && *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// Resolves redirected/OneDrive/localized Desktop folders through Windows.
func installationDesktopPath() (string, error) {
	var path *uint16
	result, _, _ := installShell.NewProc("SHGetKnownFolderPath").Call(
		uintptr(unsafe.Pointer(&installDesktopID)), 0, 0, uintptr(unsafe.Pointer(&path)))
	if path != nil {
		defer installOLE.NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(path)))
	}
	if err := installHRESULT(result, "Find Desktop folder"); err != nil {
		return "", err
	}
	value := installWideString(path)
	if value == "" {
		return "", fmt.Errorf("Windows returned an empty Desktop path")
	}
	return value, nil
}

// COM stays on one OS thread. All interface references are released before return.
func withInstallShellLink(action func(uintptr, uintptr) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	status, _, _ := installOLE.NewProc("CoInitializeEx").Call(0, 2)
	if err := installHRESULT(status, "Initialize Desktop shortcut"); err != nil {
		return err
	}
	defer installOLE.NewProc("CoUninitialize").Call()
	var link uintptr
	status, _, _ = installOLE.NewProc("CoCreateInstance").Call(
		uintptr(unsafe.Pointer(&installClassShellLink)), 0, 1,
		uintptr(unsafe.Pointer(&installIIDShellLinkW)), uintptr(unsafe.Pointer(&link)))
	if err := installHRESULT(status, "Create Desktop shortcut"); err != nil {
		return err
	}
	defer installCOMCall(link, 2)
	var persist uintptr
	status = installCOMCall(link, 0, uintptr(unsafe.Pointer(&installIIDPersistFile)), uintptr(unsafe.Pointer(&persist)))
	if err := installHRESULT(status, "Save Desktop shortcut"); err != nil {
		return err
	}
	defer installCOMCall(persist, 2)
	return action(link, persist)
}

func writeDesktopShortcut(shortcut, target string) error {
	if err := os.MkdirAll(filepath.Dir(shortcut), 0700); err != nil {
		return err
	}
	return withInstallShellLink(func(link, persist uintptr) error {
		values := []struct {
			index     int
			value     string
			operation string
		}{
			{20, target, "Set shortcut target"},
			{9, filepath.Dir(target), "Set shortcut folder"},
			{7, "active-code IPTV player", "Set shortcut description"},
		}
		for _, value := range values {
			wide, err := syscall.UTF16PtrFromString(value.value)
			if err != nil {
				return err
			}
			status := installCOMCall(link, value.index, uintptr(unsafe.Pointer(wide)))
			runtime.KeepAlive(wide)
			if err := installHRESULT(status, value.operation); err != nil {
				return err
			}
		}
		icon, err := syscall.UTF16PtrFromString(target)
		if err != nil {
			return err
		}
		// Icon #0 is the embedded tv2u launcher icon, not a generic EXE icon.
		status := installCOMCall(link, 17, uintptr(unsafe.Pointer(icon)), 0)
		runtime.KeepAlive(icon)
		if err := installHRESULT(status, "Set tv2u shortcut icon"); err != nil {
			return err
		}
		if err := installHRESULT(installCOMCall(link, 15, 1), "Set shortcut window"); err != nil {
			return err
		}
		path, err := syscall.UTF16PtrFromString(shortcut)
		if err != nil {
			return err
		}
		status = installCOMCall(persist, 6, uintptr(unsafe.Pointer(path)), 1)
		runtime.KeepAlive(path)
		return installHRESULT(status, "Write Desktop shortcut")
	})
}

func prepareInstalledLaunch() (bool, error) {
	// CI uses the same compiled GUI without installing on the runner Desktop.
	for _, arg := range os.Args[1:] {
		if arg == "--portable" {
			return false, nil
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return false, err
	}
	source, err := os.Executable()
	if err != nil {
		return false, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return false, err
	}
	target := installedApplicationPath(base)
	desktop, err := installationDesktopPath()
	if err != nil {
		return false, err
	}
	shortcut := filepath.Join(desktop, "active-code.lnk")
	if sameInstallPath(source, target) {
		if len(os.Args) == 4 && os.Args[1] == "--finish-install" {
			pid, err := strconv.Atoi(os.Args[2])
			if err == nil && pid > 0 && pid != os.Getpid() {
				finishDesktopBootstrap(pid, os.Args[3], desktop, target)
			}
			// This short-lived instance performs cleanup only, never a second UI.
			return true, nil
		}
		if _, err := os.Stat(shortcut); os.IsNotExist(err) {
			if err := writeDesktopShortcut(shortcut, target); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if !identicalExecutableFiles(source, target) {
		if err := copyInstalledExecutable(source, target); err != nil {
			return false, err
		}
	}
	if err := writeDesktopShortcut(shortcut, target); err != nil {
		return false, err
	}
	// The installed GUI starts normally; no console/PowerShell/helper script.
	cmd := exec.Command(target)
	if err := cmd.Start(); err != nil {
		return false, err
	}
	_ = cmd.Process.Release()
	if desktopBootstrapEligible(source, desktop, target) {
		// The same installed EXE is also the invisible cleanup worker, so early
		// UI closure cannot leave the original Desktop bootstrap behind.
		cleanup := exec.Command(target, "--finish-install", strconv.Itoa(os.Getpid()), source)
		if err := cleanup.Start(); err != nil {
			return true, fmt.Errorf("App installed, but Desktop bootstrap cleanup could not start: %w", err)
		}
		_ = cleanup.Process.Release()
	}
	return true, nil
}

func finishDesktopBootstrap(pid int, source, desktop, installed string) {
	if !desktopBootstrapEligible(source, desktop, installed) {
		return
	}
	kernel := syscall.NewLazyDLL("kernel32.dll")
	handle, _, _ := kernel.NewProc("OpenProcess").Call(0x00100000, 0, uintptr(pid))
	if handle != 0 {
		status, _, _ := kernel.NewProc("WaitForSingleObject").Call(handle, 10000)
		kernel.NewProc("CloseHandle").Call(handle)
		if status != 0 {
			return
		}
	}
	// Remove only the launched Desktop bootstrap, and only if its exact bytes
	// still match the installed application. Other Desktop files are untouched.
	if !identicalExecutableFiles(source, installed) {
		return
	}
	for attempt := 0; attempt < 30; attempt++ {
		if err := os.Remove(source); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
