//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"
	"unsafe"
)

// VLC 3.x track ABI on Windows x64. All returned allocations are released
// by libvlc_media_tracks_release after copying the fields used by the panel.
type vlcTrackInfo struct {
	Codec, Original          uint32
	ID, Type, Profile, Level int32
	Detail                   uintptr
	Bitrate                  uint32
	Padding                  uint32
	Language, Description    uintptr
}

type streamInfoSampler struct {
	when     time.Time
	received uint32
}

var copyNativeMemory = kernel32.NewProc("RtlMoveMemory")

func copyNative(dest unsafe.Pointer, source uintptr, size uintptr) {
	copyNativeMemory.Call(uintptr(dest), source, size)
}

func fourCC(code uint32) string {
	b := []byte{byte(code), byte(code >> 8), byte(code >> 16), byte(code >> 24)}
	for i, c := range b {
		if c < 32 || c > 126 {
			b[i] = '?'
		}
	}
	return strings.ToUpper(strings.TrimSpace(string(b)))
}

func (s *streamInfoSampler) snapshot(v *libvlcNative, software bool) []string {
	states := []string{"Idle", "Opening", "Buffering", "Playing", "Paused", "Stopped", "Ended", "Error"}
	state := v.state()
	stateName := "Unknown"
	if state >= 0 && state < len(states) {
		stateName = states[state]
	}
	mode := "Automatic hardware/software selection"
	if software {
		mode = "Software decoding"
	}
	lines := []string{"Player: VLC " + vlcStableVersion, "Playback state: " + stateName, "Decoder mode: " + mode}
	video, audio := "Video: Not available yet", "Audio: Not available yet"
	selectedAudio := int32(-1)
	if get, e := v.dll.FindProc("libvlc_audio_get_track"); e == nil {
		value, _, _ := get.Call(v.player)
		selectedAudio = int32(value)
	}
	audioChosen := false
	getTracks, e1 := v.dll.FindProc("libvlc_media_tracks_get")
	releaseTracks, e2 := v.dll.FindProc("libvlc_media_tracks_release")
	if e1 == nil && e2 == nil && v.media != 0 {
		var tracks uintptr
		count, _, _ := getTracks.Call(v.media, uintptr(unsafe.Pointer(&tracks)))
		if count > 0 && tracks != 0 {
			v.videoKnown = true
			v.videoExpected = false
			defer releaseTracks.Call(tracks, count)
			limit := count
			if limit > 128 {
				limit = 128
			}
			for i := uintptr(0); i < limit; i++ {
				var address uintptr
				copyNative(unsafe.Pointer(&address), tracks+i*unsafe.Sizeof(address), unsafe.Sizeof(address))
				if address == 0 {
					continue
				}
				var track vlcTrackInfo
				copyNative(unsafe.Pointer(&track), address, unsafe.Sizeof(track))
				if track.Type == 1 {
					v.videoExpected = true
				}
				if track.Type == 1 && track.Detail != 0 && strings.Contains(video, "Not available") {
					var data [6]uint32
					copyNative(unsafe.Pointer(&data[0]), track.Detail, unsafe.Sizeof(data))
					fps := "unknown fps"
					if data[5] > 0 {
						fps = fmt.Sprintf("%.2f fps", float64(data[4])/float64(data[5]))
					}
					video = fmt.Sprintf("Video: %s | %d x %d | %s", fourCC(track.Codec), data[1], data[0], fps)
				}
				if track.Type == 0 && track.Detail != 0 && !audioChosen {
					var data [2]uint32
					copyNative(unsafe.Pointer(&data[0]), track.Detail, unsafe.Sizeof(data))
					audio = fmt.Sprintf("Audio: %s | %d Hz | %d channels", fourCC(track.Codec), data[1], data[0])
					if track.ID == selectedAudio {
						audioChosen = true
					}
				}
			}
		}
	}
	lines = append(lines, video, audio)
	var stats vlcMediaStats
	if v.media != 0 {
		ok, _, _ := v.mediaGetStats.Call(v.media, uintptr(unsafe.Pointer(&stats)))
		if ok != 0 {
			now := time.Now()
			received := uint32(stats.ReadBytes)
			if !s.when.IsZero() && now.After(s.when) {
				mbps := float64(received-s.received) * 8 / now.Sub(s.when).Seconds() / 1e6
				lines = append(lines, fmt.Sprintf("Input: %.2f Mbps | received %.2f MiB", mbps, float64(received)/(1<<20)))
			} else {
				lines = append(lines, fmt.Sprintf("Received: %.2f MiB", float64(received)/(1<<20)))
			}
			s.when, s.received = now, received
			lines = append(lines, fmt.Sprintf("Decoded: %d video frames | %d audio buffers", stats.DecodedVideo, stats.DecodedAudio), fmt.Sprintf("Displayed frames: %d | lost: %d", stats.DisplayedPictures, stats.LostPictures))
		}
	}
	position, _, _ := v.getTime.Call(v.player)
	duration, _, _ := v.getLength.Call(v.player)
	if int64(position) >= 0 {
		text := fmt.Sprintf("Position: %ds", int64(position)/1000)
		if int64(duration) > 0 {
			text += fmt.Sprintf(" / %ds", int64(duration)/1000)
		} else {
			text += " | Live / unknown duration"
		}
		lines = append(lines, text)
	}
	if v.recordFile != "" {
		lines = append(lines, "Recording: ON")
	} else {
		lines = append(lines, "Recording: OFF")
	}
	return lines
}

func (g *nativeUI) streamInfoLines() []string {
	name := "No channel selected"
	if g.playingItem.Name != "" {
		name = g.playingItem.Name
	} else if g.selectedID >= 0 && g.selectedID < len(g.channels) {
		name = g.channels[g.selectedID].Name
	}
	lines := append([]string{"Channel: " + name, "Status: " + g.status}, g.infoLines...)
	if g.recordingNote != "" {
		lines = append(lines, g.recordingNote)
	}
	if len(g.infoLines) == 0 {
		lines = append(lines, "Reading current stream information…")
	}
	return lines
}
func (g *nativeUI) infoMetrics() (width, height, row, top int32) {
	return streamInfoPanelMetrics(g.width, g.height, len(g.streamInfoLines()))
}

func (g *nativeUI) infoRect() rectF {
	w, h, _, _ := g.infoMetrics()
	return rectF{(g.width - w) / 2, (g.height - h) / 2, w, h}
}
func (g *nativeUI) infoCloseRect() rectF {
	r := g.infoRect()
	return rectF{r.x + r.w - 48, r.y + 4, 44, 44}
}
func (g *nativeUI) drawStreamInfo(hdc uintptr) {
	r := g.infoRect()
	_, _, row, top := g.infoMetrics()
	roundedFill(hdc, r, 12, rgb(15, 30, 38))
	white := rgb(235, 245, 248)
	centerLabel(hdc, "Stream Info & Playback Information", rectF{r.x + 50, r.y + 8, r.w - 100, row}, g.fontInfo, white)
	centerLabel(hdc, "×", g.infoCloseRect(), g.fontClose, white)
	for i, line := range g.streamInfoLines() {
		if top+int32(i+1)*row > r.h-8 {
			break
		}
		centerLabel(hdc, line, rectF{r.x + 16, r.y + top + int32(i)*row, r.w - 32, row}, g.fontInfo, white)
	}
}
