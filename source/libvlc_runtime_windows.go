//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Only the stable VideoLAN release is accepted. Never download latest/nightly/beta.
const vlcStableVersion = "3.0.24"
const vlcStableArchiveSHA256 = "fcf30850371ad10c9373cc4f0f4501e7dee49e3e9ae9f20c72fb2661a1ca6323"
const vlcMaxArchiveBytes = 90 << 20
const vlcMaxExpandedBytes = 450 << 20

var vlcInstallMu sync.Mutex

// Strictly offline runtime: the release MUST contain an embedded stable VLC archive.
// The user's PC will NEVER fetch VLC or codecs from any server.
// The embedded archive is verified against VideoLAN's pinned SHA-256 before extraction.
func acquireVLCPackage() ([]byte, error) {
	if len(bundledVLC) == 0 {
		return nil, errors.New("VLC 3.0.24 stable runtime was not embedded: refusing incomplete build")
	}
	return bundledVLC, nil
}
func archiveValid(data []byte) bool {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]) == vlcStableArchiveSHA256
}
func ensureVLCRuntime() (string, error) {
	vlcInstallMu.Lock()
	defer vlcInstallMu.Unlock()
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, "active-code", "libvlc-"+vlcStableVersion+"-x64")
	sentinel := filepath.Join(root, "verified.sha256")
	if existing, e := os.ReadFile(sentinel); e == nil && strings.TrimSpace(string(existing)) == vlcStableArchiveSHA256 {
		if isFile(filepath.Join(root, "libvlc.dll")) && isFile(filepath.Join(root, "libvlccore.dll")) && isDir(filepath.Join(root, "plugins")) {
			return root, nil
		}
	}
	data, err := acquireVLCPackage()
	if err != nil {
		return "", err
	}
	if !archiveValid(data) {
		return "", errors.New("Official VLC 3.0.24 archive SHA-256 mismatch: refusing unverified code")
	}
	temp := root + ".unpacking"
	os.RemoveAll(temp)
	if err = os.MkdirAll(temp, 0700); err != nil {
		return "", err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(temp)
		}
	}()
	if err = extractVLC(data, temp); err != nil {
		return "", err
	}
	for _, name := range []string{"libvlc.dll", "libvlccore.dll"} {
		if !isFile(filepath.Join(temp, name)) {
			return "", fmt.Errorf("VLC archive missing %s", name)
		}
	}
	if !isDir(filepath.Join(temp, "plugins")) {
		return "", errors.New("VLC archive missing plugins")
	}
	if err = os.WriteFile(filepath.Join(temp, "verified.sha256"), []byte(vlcStableArchiveSHA256+"\n"), 0600); err != nil {
		return "", err
	}
	// Cached runtime is fully pinned to the VideoLAN archive hash.
	if err = os.RemoveAll(root); err != nil {
		return "", err
	}
	if err = os.Rename(temp, root); err != nil {
		return "", err
	}
	success = true
	return root, nil
}
func isFile(name string) bool { stat, err := os.Stat(name); return err == nil && !stat.IsDir() }
func isDir(name string) bool  { stat, err := os.Stat(name); return err == nil && stat.IsDir() }
func extractVLC(data []byte, root string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	var total int64
	const prefix = "vlc-3.0.24/"
	var count int
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		relative := strings.TrimPrefix(name, prefix)
		if relative == "" {
			continue
		}
		// LibVLC + plugins and dependent DLLs, not duplicate VLC executable/UI binaries.
		if !(strings.HasPrefix(relative, "plugins/") || (!strings.Contains(relative, "/") && strings.HasSuffix(strings.ToLower(relative), ".dll"))) {
			continue
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Unsafe symlink in runtime: %s", relative)
		}
		cleaned := filepath.Clean(filepath.FromSlash(relative))
		if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return errors.New("Unsafe VLC archive entry")
		}
		dest := filepath.Join(root, cleaned)
		if !strings.HasPrefix(dest, root+string(filepath.Separator)) {
			return errors.New("VLC archive path traversal")
		}
		if f.FileInfo().IsDir() {
			if err = os.MkdirAll(dest, 0700); err != nil {
				return err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > vlcMaxExpandedBytes {
			return errors.New("VLC archive expanded size exceeds limit")
		}
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		stream, e := f.Open()
		if e != nil {
			return e
		}
		out, e := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if e != nil {
			stream.Close()
			return e
		}
		n, e := io.Copy(out, io.LimitReader(stream, int64(f.UncompressedSize64)+1))
		ce := out.Close()
		stream.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if n != int64(f.UncompressedSize64) {
			return errors.New("VLC archive size mismatch")
		}
		count++
	}
	if count < 20 {
		return fmt.Errorf("Incomplete VLC runtime: only %d files", count)
	}
	return nil
}
