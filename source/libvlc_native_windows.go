//go:build windows

package main

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type libvlcNative struct {
	dll                       *syscall.DLL
	instance                  uintptr
	player                    uintptr
	media                     uintptr
	newMedia                  *syscall.Proc
	mediaRelease              *syscall.Proc
	mediaSet                  *syscall.Proc
	mediaAddOption            *syscall.Proc
	play                      *syscall.Proc
	stop                      *syscall.Proc
	releasePlayer             *syscall.Proc
	releaseInstance           *syscall.Proc
	setHWND                   *syscall.Proc
	getTime                   *syscall.Proc
	getLength                 *syscall.Proc
	isSeekable                *syscall.Proc
	setTime                   *syscall.Proc
	getState                  *syscall.Proc
	videoSetScale             *syscall.Proc
	videoSetAspect            *syscall.Proc
	mediaGetStats             *syscall.Proc
	videoTrackCount           *syscall.Proc
	takeSnapshot              *syscall.Proc
	recordFile                string
	videoKnown, videoExpected bool
}

// VLC 3.x libvlc_media_stats_t, whose C fields are all 32-bit values.
// https://videolan.videolan.me/vlc-3.0/structlibvlc__media__stats__t.html
type vlcMediaStats struct {
	ReadBytes                            int32
	InputBitrate                         float32
	DemuxReadBytes                       int32
	DemuxBitrate                         float32
	DemuxCorrupted, DemuxDiscontinuity   int32
	DecodedVideo, DecodedAudio           int32
	DisplayedPictures, LostPictures      int32
	PlayedAudioBuffers, LostAudioBuffers int32
	SentPackets, SentBytes               int32
	SendBitrate                          float32
}

func cBytes(value string) []byte { return append([]byte(value), 0) }
func (v *libvlcNative) load() error {
	return v.loadWithOptions(nil)
}

func (v *libvlcNative) loadWithOptions(extra []string) error {
	folder, err := ensureVLCRuntime()
	if err != nil {
		return err
	}
	if err = os.Setenv("VLC_PLUGIN_PATH", filepath.Join(folder, "plugins")); err != nil {
		return err
	}
	kernel := syscall.NewLazyDLL("kernel32.dll")
	p, err := syscall.UTF16PtrFromString(folder)
	if err != nil {
		return err
	}
	ret, _, _ := kernel.NewProc("SetDllDirectoryW").Call(uintptr(unsafe.Pointer(p)))
	if ret == 0 {
		return errors.New("Cannot register VLC native library directory")
	}
	v.dll, err = syscall.LoadDLL(filepath.Join(folder, "libvlc.dll"))
	if err != nil {
		return fmt.Errorf("LibVLC failed to load: %w", err)
	}
	look := func(name string) (*syscall.Proc, error) {
		proc, e := v.dll.FindProc(name)
		if e != nil {
			return nil, fmt.Errorf("LibVLC missing %s: %w", name, e)
		}
		return proc, nil
	}
	names := []struct {
		key  string
		dest **syscall.Proc
	}{
		{"libvlc_media_new_location", &v.newMedia},
		{"libvlc_media_release", &v.mediaRelease},
		{"libvlc_media_player_set_media", &v.mediaSet},
		{"libvlc_media_add_option", &v.mediaAddOption},
		{"libvlc_media_player_play", &v.play},
		{"libvlc_media_player_stop", &v.stop},
		{"libvlc_media_player_release", &v.releasePlayer},
		{"libvlc_release", &v.releaseInstance},
		{"libvlc_media_player_set_hwnd", &v.setHWND},
		{"libvlc_media_player_get_time", &v.getTime},
		{"libvlc_media_player_get_length", &v.getLength},
		{"libvlc_media_player_is_seekable", &v.isSeekable},
		{"libvlc_media_player_set_time", &v.setTime},
		{"libvlc_media_player_get_state", &v.getState},
		{"libvlc_video_set_scale", &v.videoSetScale},
		{"libvlc_video_set_aspect_ratio", &v.videoSetAspect},
		{"libvlc_media_get_stats", &v.mediaGetStats},
		{"libvlc_video_get_track_count", &v.videoTrackCount},
		{"libvlc_video_take_snapshot", &v.takeSnapshot},
	}
	for _, entry := range names {
		proc, e := look(entry.key)
		if e != nil {
			return e
		}
		*entry.dest = proc
	}
	newInstance, e := look("libvlc_new")
	if e != nil {
		return e
	}
	// No embedded VLC UI: video/audio are controlled by active-code.
	opts := []string{"--intf=dummy", "--no-video-title-show", "--quiet", "--no-osd", "--no-snapshot-preview", "--stats", "--drop-late-frames", "--skip-frames"}
	opts = append(opts, extra...)
	argv := make([][]byte, len(opts))
	ptrs := make([]uintptr, len(opts))
	for i, opt := range opts {
		argv[i] = cBytes(opt)
		ptrs[i] = uintptr(unsafe.Pointer(&argv[i][0]))
	}
	v.instance, _, _ = newInstance.Call(uintptr(len(ptrs)), uintptr(unsafe.Pointer(&ptrs[0])))
	runtime.KeepAlive(argv)
	runtime.KeepAlive(ptrs)
	if v.instance == 0 {
		return errors.New("LibVLC initialization failed; codec plugin may be missing")
	}
	makePlayer, e := look("libvlc_media_player_new")
	if e != nil {
		return e
	}
	v.player, _, _ = makePlayer.Call(v.instance)
	if v.player == 0 {
		return errors.New("LibVLC video player could not be created")
	}
	// Input belongs to the app's group/channel navigation, not VLC seek keys.
	if input, e := look("libvlc_video_set_key_input"); e == nil {
		input.Call(v.player, 0)
	}
	return nil
}
func (v *libvlcNative) render(hwnd uintptr) {
	v.setHWND.Call(v.player, hwnd)
	if v.videoSetScale != nil {
		v.videoSetScale.Call(v.player, 0)
	}
	var bounds winRect
	result, _, _ := winGetRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	if result != 0 && bounds.Right > bounds.Left && bounds.Bottom > bounds.Top {
		// Android uses SURFACE_FILL: video fills the complete screen.
		aspect := cBytes(fmt.Sprintf("%d:%d", bounds.Right-bounds.Left, bounds.Bottom-bounds.Top))
		v.videoSetAspect.Call(v.player, uintptr(unsafe.Pointer(&aspect[0])))
		runtime.KeepAlive(aspect)
	}
}

func (v *libvlcNative) addOption(media uintptr, option string) {
	if media == 0 || v.mediaAddOption == nil || option == "" {
		return
	}
	data := cBytes(option)
	v.mediaAddOption.Call(media, uintptr(unsafe.Pointer(&data[0])))
	runtime.KeepAlive(data)
}

func (v *libvlcNative) change(item channel, fallbackUA string, softwareDecode bool) error {
	v.videoKnown = false
	v.videoExpected = false
	url := item.URL
	if v.media != 0 {
		v.stop.Call(v.player)
		v.mediaRelease.Call(v.media)
		v.media = 0
	}
	c := cBytes(url)
	media, _, _ := v.newMedia.Call(v.instance, uintptr(unsafe.Pointer(&c[0])))
	runtime.KeepAlive(c)
	if media == 0 {
		return errors.New("LibVLC refused the stream URL")
	}
	// Match the Android fallback player: network/live cache is short, reconnect is
	// enabled per media (never as a global LibVLC CLI option), and playlist
	// User-Agent/Referer directives are honored. Global --http-reconnect caused
	// LibVLC initialization failures on some Windows PCs, so it must stay here.
	v.addOption(media, ":network-caching=750")
	v.addOption(media, ":live-caching=750")
	v.addOption(media, ":http-reconnect")
	if v.recordFile != "" {
		for _, option := range nativeRecordingOptions(v.recordFile) {
			v.addOption(media, option)
		}
	}

	if softwareDecode {
		// Retry without a Windows GPU decoder when automatic decoding fails.
		v.addOption(media, ":avcodec-hw=none")
		v.addOption(media, ":avcodec-skiploopfilter=3")
	}
	ua := strings.TrimSpace(item.Headers["User-Agent"])
	if ua == "" {
		ua = strings.TrimSpace(fallbackUA)
	}
	if ua != "" {
		v.addOption(media, ":http-user-agent="+ua)
	}
	if ref := strings.TrimSpace(item.Headers["Referer"]); ref != "" {
		v.addOption(media, ":http-referrer="+ref)
	}
	v.media = media
	v.mediaSet.Call(v.player, media)
	status, _, _ := v.play.Call(v.player)
	if int32(status) != 0 {
		return fmt.Errorf("LibVLC could not start playback (code %d)", int32(status))
	}
	return nil
}

func (v *libvlcNative) state() int {
	if v.player == 0 || v.getState == nil {
		return 0
	}
	state, _, _ := v.getState.Call(v.player)
	return int(int32(state))
}

func (v *libvlcNative) decodedProgress() (video, audio int32, videoExpected, ok bool) {
	if v.media == 0 || v.mediaGetStats == nil {
		return
	}
	var stats vlcMediaStats
	result, _, _ := v.mediaGetStats.Call(v.media, uintptr(unsafe.Pointer(&stats)))
	if result == 0 {
		return
	}
	tracks, _, _ := v.videoTrackCount.Call(v.player)
	// A selectable video track is accompanied by VLC's "Disable" entry.
	expected := int32(tracks) > 1
	if v.videoKnown {
		expected = v.videoExpected
	}
	return stats.DecodedVideo, stats.DecodedAudio, expected, true
}
func (v *libvlcNative) canSeek() bool {
	if v.player == 0 || v.isSeekable == nil {
		return false
	}
	ok, _, _ := v.isSeekable.Call(v.player)
	return int32(ok) != 0
}
func (v *libvlcNative) seekTimeline() (position, duration int64) {
	p, _, _ := v.getTime.Call(v.player)
	d, _, _ := v.getLength.Call(v.player)
	return int64(p), int64(d)
}
func (v *libvlcNative) seekTo(position int64) { v.setTime.Call(v.player, uintptr(position)) }

func (v *libvlcNative) close() {
	if v.player != 0 {
		v.stop.Call(v.player)
		if v.media != 0 {
			v.mediaRelease.Call(v.media)
			v.media = 0
		}
		v.releasePlayer.Call(v.player)
		v.player = 0
	}
	if v.instance != 0 {
		v.releaseInstance.Call(v.instance)
		v.instance = 0
	}
	if v.dll != nil {
		v.dll.Release()
		v.dll = nil
	}
}

// Capture before stopping the previous decoder. VLC writes asynchronously;
// a short ceiling prevents snapshot failure delaying a channel indefinitely.
func (v *libvlcNative) transitionSnapshot() image.Image {
	if v.player == 0 || v.media == 0 || v.takeSnapshot == nil {
		return nil
	}
	var stats vlcMediaStats
	ok, _, _ := v.mediaGetStats.Call(v.media, uintptr(unsafe.Pointer(&stats)))
	if ok == 0 || stats.DisplayedPictures == 0 {
		return nil
	}
	dir, err := os.MkdirTemp("", "active-code-frame-*")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "frame.png")
	name := cBytes(path)
	status, _, _ := v.takeSnapshot.Call(v.player, 0, uintptr(unsafe.Pointer(&name[0])), 960, 540)
	runtime.KeepAlive(name)
	if int32(status) != 0 {
		return nil
	}
	until := time.Now().Add(180 * time.Millisecond)
	for time.Now().Before(until) {
		f, err := os.Open(path)
		if err == nil {
			cfg, e := png.DecodeConfig(f)
			f.Close()
			if e == nil && cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= 1920 && cfg.Height <= 1080 {
				f, e = os.Open(path)
				if e == nil {
					picture, e := png.Decode(f)
					f.Close()
					if e == nil {
						return picture
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}
func (v *libvlcNative) presentedFirstFrame() bool {
	if v.media == 0 {
		return false
	}
	var stats vlcMediaStats
	ok, _, _ := v.mediaGetStats.Call(v.media, uintptr(unsafe.Pointer(&stats)))
	return ok != 0 && transitionCanReveal(stats.DisplayedPictures, stats.PlayedAudioBuffers, v.videoKnown, v.videoExpected)
}
