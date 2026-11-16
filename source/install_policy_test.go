package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPathRecognizesFileIdentityWithoutMatchingUnrelatedCopies(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "installed.exe")
	alias := filepath.Join(root, "alias.exe")
	other := filepath.Join(root, "other.exe")
	for _, path := range []string{target, other} {
		if err := os.WriteFile(path, []byte("same bytes, different file"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(target, alias); err != nil {
		t.Fatal(err)
	}
	if !sameInstallPath(target, alias) {
		t.Fatal("same filesystem object not recognized through alias")
	}
	if sameInstallPath(target, other) {
		t.Fatal("unrelated copy mistaken for installed executable")
	}
}

func TestInstallPathKeepsTheExecutableOffDesktop(t *testing.T) {
	base := filepath.Join(t.TempDir(), "Local AppData বাংলা")
	want := filepath.Join(base, "Programs", "active-code", "active-code.exe")
	if installedApplicationPath(base) != want {
		t.Fatal("installed executable is not under per-user Programs")
	}
}

func TestBootstrapCleanupOnlyTargetsDirectDesktopEXE(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "OneDrive", "Desktop বাংলা")
	installed := installedApplicationPath(filepath.Join(root, "Local"))
	for _, tc := range []struct {
		path     string
		eligible bool
	}{
		{filepath.Join(desktop, "active-code.exe"), true},
		{filepath.Join(desktop, "active-code (2).EXE"), true},
		{filepath.Join(desktop, "active-code.lnk"), false},
		{filepath.Join(desktop, "Source", "active-code.exe"), false},
		{filepath.Join(root, "Downloads", "active-code.exe"), false},
		{installed, false},
	} {
		if desktopBootstrapEligible(tc.path, desktop, installed) != tc.eligible {
			t.Fatalf("wrong bootstrap cleanup decision: %q", tc.path)
		}
	}
}

func TestInstallCopyIsCompleteAndRetainsPreviousFileOnInputFailure(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "downloaded app.exe")
	target := installedApplicationPath(filepath.Join(root, "Local"))
	if err := os.WriteFile(source, []byte("MZ-executable-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyInstalledExecutable(source, target); err != nil {
		t.Fatal(err)
	}
	if !identicalExecutableFiles(source, target) {
		t.Fatal("installed copy differs from input")
	}
	if err := copyInstalledExecutable(filepath.Join(root, "missing.exe"), target); err == nil {
		t.Fatal("missing installer input succeeded")
	}
	if !identicalExecutableFiles(source, target) {
		t.Fatal("failed install damaged existing executable")
	}
	if err := os.WriteFile(source, []byte("unrelated desktop file"), 0600); err != nil {
		t.Fatal(err)
	}
	if identicalExecutableFiles(source, target) {
		t.Fatal("unrelated files accepted for bootstrap cleanup")
	}
	files, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "active-code.exe" {
		t.Fatal("temporary installation files left behind")
	}
}
