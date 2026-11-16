//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Runs on a real Windows GitHub runner, without activation or provider access.
// Tests the embedded bytes, extraction, DLL dependencies, exports and player creation.
func TestWindowsEmbeddedVLCInitialization(t *testing.T) {
	// VLC/plugins may keep dependent DLLs loaded until process exit even after
	// libvlc_release/FreeLibrary. The parent owns ALL temporary paths but never
	// loads VLC; it waits for the child to exit before Go removes those paths.
	// Decode, seek, audio selection, metadata and recording assertions still run
	// in the child, and any child failure must fail this test.
	const childFlag = "ACTIVE_CODE_VLC_SMOKE_CHILD"
	const workspaceKey = "ACTIVE_CODE_VLC_SMOKE_WORKSPACE"
	if os.Getenv(childFlag) != "1" {
		workspace := t.TempDir()
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable,
			"-test.run=^TestWindowsEmbeddedVLCInitialization$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), childFlag+"=1", workspaceKey+"="+workspace)
		output, err := cmd.CombinedOutput()
		t.Logf("Isolated native VLC test:\n%s", output)
		if ctx.Err() != nil {
			t.Fatalf("native VLC subprocess timed out: %v", ctx.Err())
		}
		if err != nil {
			t.Fatalf("native VLC subprocess failed: %v", err)
		}
		// t.TempDir cleanup still verifies deletion, now AFTER process exit.
		return
	}
	workspace := os.Getenv(workspaceKey)
	if workspace == "" {
		t.Fatal("native VLC child requires a parent-owned workspace")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.Setenv("LOCALAPPDATA", filepath.Join(workspace, "cache"))
	t.Setenv("VLC_PLUGIN_PATH", "")
	if !archiveValid(bundledVLC) {
		t.Fatal("embedded VLC checksum mismatch")
	}
	z, err := zip.NewReader(bytes.NewReader(bundledVLC), int64(len(bundledVLC)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, r)
		r.Close()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
	}
	root, err := ensureVLCRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "plugins", "codec")); err != nil {
		t.Fatal(err)
	}
	again, err := ensureVLCRuntime()
	if err != nil || again != root {
		t.Fatalf("cached runtime: %q, %v", again, err)
	}
	var v libvlcNative
	defer v.close()
	if err := v.loadWithOptions([]string{"--vout=dummy", "--aout=dummy", "--avcodec-hw=none"}); err != nil {
		t.Fatal(err)
	}
	if v.instance == 0 || v.player == 0 {
		t.Fatal("VLC instance/player missing")
	}
	fixture, err := filepath.Abs("testdata/generated-h264-aac.ts")
	if err != nil {
		t.Fatal(err)
	}
	location := (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(fixture)}).String()
	if err := v.change(channel{URL: location}, "active-code decoder test", true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		video, audio, _, ok := v.decodedProgress()
		if ok && video > 0 && audio > 0 {
			t.Logf("H.264 decoded frames=%d, AAC decoded buffers=%d", video, audio)
			// This generated local MPEG-TS fixture is seekable. Exercise the same
			// exported APIs used by Movie/Series controls before recording it.
			if !v.canSeek() {
				t.Fatal("local VOD fixture was not seekable")
			}
			v.seekTo(2000)
			seekDeadline := time.Now().Add(4 * time.Second)
			moved := false
			for time.Now().Before(seekDeadline) {
				position, _ := v.seekTimeline()
				if position >= 1500 && position < 5000 {
					moved = true
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if !moved {
				t.Fatal("VLC did not seek forward in local VOD")
			}
			v.seekTo(0)
			seekDeadline = time.Now().Add(4 * time.Second)
			moved = false
			for time.Now().Before(seekDeadline) {
				position, _ := v.seekTimeline()
				if position >= 0 && position < 1000 {
					moved = true
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if !moved {
				t.Fatal("VLC did not rewind local VOD")
			}
			var sampler streamInfoSampler
			tracks := v.audioTracks()
			if len(tracks) < 2 {
				t.Fatal("multi-language fixture audio tracks were not discovered")
			}
			if !v.selectAudio(tracks[1].ID) {
				t.Fatal("VLC refused a valid second audio track")
			}
			info := strings.Join(sampler.snapshot(&v, true), "\n")
			if !strings.Contains(info, "320 x 180") || !strings.Contains(info, "Audio:") {
				t.Fatalf("native stream metadata missing: %s", info)
			}
			dir := filepath.Join(workspace, "recording test বাংলা")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "same-input-recording.ts")
			v.recordFile = file
			if err := v.change(channel{URL: location}, "active-code recorder test", true); err != nil {
				t.Fatal(err)
			}
			recordDeadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(recordDeadline) {
				frames, buffers, _, ok := v.decodedProgress()
				if ok && frames >= 10 && buffers > 0 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			v.stop.Call(v.player)
			v.recordFile = ""
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) < 188*3 || data[0] != 0x47 || data[188] != 0x47 {
				t.Fatal("VLC same-input recorder did not produce MPEG-TS packets")
			}
			t.Logf("same-input MPEG-TS recording bytes=%d", len(data))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("embedded Windows VLC failed to decode the generated H.264/AAC sample")
}
