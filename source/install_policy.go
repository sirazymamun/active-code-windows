package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func sameInstallPath(a, b string) bool {
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	// The Windows shell can return a long path for an 8.3 input (RUNNER~1),
	// or another resolved spelling of the same file. Compare filesystem IDs
	// as well so setup does not reinstall itself through a path alias.
	first, firstErr := os.Stat(a)
	second, secondErr := os.Stat(b)
	return firstErr == nil && secondErr == nil && os.SameFile(first, second)
}

func installedApplicationPath(localAppData string) string {
	return filepath.Join(localAppData, "Programs", "active-code", "active-code.exe")
}

// Only the exact bootstrap EXE directly on the resolved Desktop is eligible
// for cleanup. Downloads, source folders, shortcuts and other paths are not.
func desktopBootstrapEligible(source, desktop, installed string) bool {
	return !sameInstallPath(source, installed) &&
		sameInstallPath(filepath.Dir(source), desktop) &&
		strings.EqualFold(filepath.Ext(source), ".exe")
}

func identicalExecutableFiles(a, b string) bool {
	first, err := os.Stat(a)
	if err != nil || !first.Mode().IsRegular() || first.Size() == 0 {
		return false
	}
	second, err := os.Stat(b)
	if err != nil || !second.Mode().IsRegular() || second.Size() != first.Size() {
		return false
	}
	digest := func(path string) ([32]byte, int64, error) {
		var value [32]byte
		f, err := os.Open(path)
		if err != nil {
			return value, 0, err
		}
		defer f.Close()
		h := sha256.New()
		n, err := io.Copy(h, f)
		copy(value[:], h.Sum(nil))
		return value, n, err
	}
	x, nx, ex := digest(a)
	y, ny, ey := digest(b)
	return ex == nil && ey == nil && nx > 0 && nx == ny && x == y
}

// Stage a complete copy in the installation directory, never on the Desktop.
// A failed copy leaves an existing installed executable unchanged.
func copyInstalledExecutable(source, destination string) error {
	if sameInstallPath(source, destination) {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(destination), ".active-code-*.tmp")
	if err != nil {
		return err
	}
	name := staged.Name()
	defer os.Remove(name)
	_, copyErr := io.Copy(staged, input)
	if copyErr == nil {
		copyErr = staged.Sync()
	}
	closeErr := staged.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, destination); err != nil {
		return fmt.Errorf("Close active-code before installing this update: %w", err)
	}
	return nil
}
