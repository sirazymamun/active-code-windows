//go:build windows

package main

import (
	"fmt"
	"sort"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

type vlcTrackDescription struct {
	ID         int32
	Padding    uint32
	Name, Next uintptr
}

func nativeCString(address uintptr) string {
	if address == 0 {
		return ""
	}
	data := make([]byte, 0, 80)
	for i := uintptr(0); i < 512; i++ {
		var c byte
		copyNative(unsafe.Pointer(&c), address+i, 1)
		if c == 0 {
			break
		}
		data = append(data, c)
	}
	return string(data)
}
func (v *libvlcNative) audioTracks() []nativeAudioTrack {
	get, e := v.dll.FindProc("libvlc_audio_get_track_description")
	if e != nil {
		return nil
	}
	release, e := v.dll.FindProc("libvlc_track_description_list_release")
	if e != nil {
		return nil
	}
	current, e := v.dll.FindProc("libvlc_audio_get_track")
	if e != nil {
		return nil
	}
	selected, _, _ := current.Call(v.player)
	head, _, _ := get.Call(v.player)
	if head == 0 {
		return nil
	}
	defer release.Call(head)
	tracks := []nativeAudioTrack{}
	for ptr, n := head, 0; ptr != 0 && n < 64; n++ {
		var track vlcTrackDescription
		copyNative(unsafe.Pointer(&track), ptr, unsafe.Sizeof(track))
		if track.ID >= 0 {
			name := nativeCString(track.Name)
			if name == "" {
				name = fmt.Sprintf("Track %d", track.ID)
			}
			tracks = append(tracks, nativeAudioTrack{ID: track.ID, Label: name, Selected: track.ID == int32(selected)})
		}
		ptr = track.Next
	}
	sort.SliceStable(tracks, func(i, j int) bool { return audioPriority(tracks[i].Label) < audioPriority(tracks[j].Label) })
	return tracks
}
func (v *libvlcNative) selectAudio(id int32) bool {
	if id < 0 {
		return false
	}
	set, e := v.dll.FindProc("libvlc_audio_set_track")
	if e != nil {
		return false
	}
	r, _, _ := set.Call(v.player, uintptr(id))
	return int32(r) == 0
}

func (g *nativeUI) openAudioWizard() {
	if len(g.audioTracks) < 2 || g.activationOpen || g.infoOpen {
		return
	}
	g.audioOpen = true
	g.audioPrompted = true
	g.audioScroll = 0
	g.audioCursor = 0
	for i, t := range g.audioTracks {
		if t.Selected {
			g.audioCursor = i
			break
		}
	}
	g.overlayDeadline = time.Time{}
	g.armAudioHide()
	g.fitChildren()
	g.repaint()
}
func (g *nativeUI) armAudioHide() {
	g.audioHideAt = time.Now().Add(15 * time.Second)
	id := atomic.LoadUint64(&g.playRequest)
	time.AfterFunc(15*time.Second, func() { winPostMessage.Call(g.hwnd, msgAudioHide, uintptr(id), 0) })
}
func (g *nativeUI) closeAudioWizard() {
	g.audioOpen = false
	g.fitChildren()
	g.armOverlayHide()
	g.repaint()
}
func (g *nativeUI) chooseAudio(index int) {
	if index < 0 || index >= len(g.audioTracks) {
		return
	}
	id := g.audioTracks[index].ID
	request := atomic.LoadUint64(&g.playRequest)
	g.closeAudioWizard()
	select {
	case g.vlcCh <- vlcRequest{setAudio: true, audioID: id, requestID: request}:
	default:
		g.status = "Audio selection busy; press A to retry"
	}
}
func (g *nativeUI) moveAudio(delta int) {
	g.audioCursor = maxInt(0, g.audioCursor+delta)
	if g.audioCursor >= len(g.audioTracks) {
		g.audioCursor = len(g.audioTracks) - 1
	}
	if g.audioCursor < g.audioScroll {
		g.audioScroll = g.audioCursor
	}
	if g.audioCursor >= g.audioScroll+8 {
		g.audioScroll = g.audioCursor - 7
	}
	g.armAudioHide()
	g.repaint()
}
func (g *nativeUI) drawAudioWizard(hdc uintptr) {
	if !g.audioOpen || len(g.audioTracks) < 2 {
		return
	}
	old, _, _ := winSelect.Call(hdc, g.fontAudio)
	measure := gdi32win.NewProc("GetTextExtentPoint32W")
	width := int32(150)
	for _, track := range g.audioTracks {
		text, _ := syscall.UTF16FromString(track.Label)
		var size struct{ X, Y int32 }
		measure.Call(hdc, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), uintptr(unsafe.Pointer(&size)))
		width = max32(width, size.X+44)
	}
	winSelect.Call(hdc, g.fontNormal)
	title, _ := syscall.UTF16FromString("Audio language")
	var titleSize struct{ X, Y int32 }
	measure.Call(hdc, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)-1), uintptr(unsafe.Pointer(&titleSize)))
	width = max32(width, titleSize.X+54)
	winSelect.Call(hdc, old)
	width = min32(width, g.width-6)
	base := min32(30, max32(14, g.height*3/100))
	rows := min32(8, int32(len(g.audioTracks)))
	line := max32(24, base*162/100)
	header := max32(28, base*12/10)
	height := header + rows*line
	y := max32(4, g.height-base*34/10-height)
	g.audioBox = rectF{g.width - width, y, width, height}
	g.audioRowHeight = line
	g.audioHeaderHeight = header
	r := g.audioBox
	roundedFill(hdc, r, 8, rgb(8, 12, 20))
	label(hdc, "Audio language", rectF{r.x + 12, r.y + 4, r.w - 54, header - 4}, g.fontNormal, rgb(240, 247, 250))
	centerLabel(hdc, "×", rectF{r.x + r.w - 38, r.y + 3, 34, header - 2}, g.fontBold, rgb(240, 247, 250))
	if g.audioCursor < g.audioScroll {
		g.audioScroll = g.audioCursor
	}
	if g.audioCursor >= g.audioScroll+int(rows) {
		g.audioScroll = g.audioCursor - int(rows) + 1
	}
	for row := 0; row < int(rows) && g.audioScroll+row < len(g.audioTracks); row++ {
		i := g.audioScroll + row
		track := g.audioTracks[i]
		rr := rectF{r.x + 4, r.y + g.audioHeaderHeight + int32(row)*line, r.w - 8, line}
		if i == g.audioCursor {
			fill(hdc, rr, rgb(24, 100, 124))
		}
		name := track.Label
		if track.Selected {
			name = name + "  ✓"
		}
		color := rgb(238, 243, 250)
		if track.Selected {
			color = rgb(62, 166, 255)
		}
		label(hdc, name, rectF{rr.x + 8, rr.y, rr.w - 16, rr.h}, g.fontAudio, color)
	}
}
