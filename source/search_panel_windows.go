//go:build windows

package main

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func (g *nativeUI) searchPanelRect() rectF {
	width := min32(650, g.width*43/100)
	if g.width <= 680 {
		width = min32(440, g.width*85/100)
	}
	return rectF{g.width - width, 0, width, g.height}
}
func (g *nativeUI) searchInputRect() rectF {
	r := g.searchPanelRect()
	return rectF{r.x + 12, 54, r.w - 88, 46}
}
func (g *nativeUI) searchMicRect() rectF {
	r := g.searchPanelRect()
	return rectF{r.x + r.w - 64, 54, 52, 46}
}
func (g *nativeUI) searchCloseRect() rectF {
	r := g.searchPanelRect()
	return rectF{r.x + r.w - 52, 4, 48, 44}
}
func (g *nativeUI) searchResultsTop() int32 { return 138 }
func (g *nativeUI) openSearchPanel() {
	if g.seriesPage {
		g.restoreSeriesLibrary("Series")
	}
	g.audioOpen = false
	g.infoOpen = false
	g.searchVisible = true
	g.showLists = false
	g.overlayVisible = true
	g.overlayDeadline = time.Time{}
	g.searchText = ""
	g.voiceNote = ""
	g.focus = 1
	g.chanIndex = 0
	g.chanScroll = 0
	g.refreshVisible()
	g.fitChildren()
	winSetFocus.Call(g.hwnd)
	g.repaint()
}
func (g *nativeUI) cancelVoiceSearch() {
	g.searchEpoch++
	if g.voiceCancel != nil {
		g.voiceCancel()
		g.voiceCancel = nil
	}
	g.voiceBusy = false
}
func (g *nativeUI) closeSearchPanel() {
	if !g.searchVisible {
		return
	}
	g.cancelVoiceSearch()
	g.searchVisible = false
	g.searchText = ""
	g.voiceNote = ""
	g.refreshVisible()
	g.armOverlayHide()
	g.fitChildren()
	g.repaint()
}
func (g *nativeUI) drawSearchPanel(dc uintptr) {
	r := g.searchPanelRect()
	fill(dc, r, rgb(17, 37, 48))
	label(dc, "Search", rectF{r.x + 12, 4, r.w - 70, 44}, g.fontNormal, rgb(228, 240, 243))
	centerLabel(dc, "×", g.searchCloseRect(), g.fontLarge, rgb(228, 240, 243))
	input := g.searchInputRect()
	fill(dc, input, rgb(231, 241, 243))
	query := g.searchText
	if query == "" {
		query = "Type to search…"
	}
	label(dc, query, rectF{input.x + 8, input.y, input.w - 16, input.h}, g.fontNormal, rgb(29, 51, 58))
	mic := "🎙"
	if g.voiceBusy {
		mic = "…"
	}
	centerLabel(dc, mic, g.searchMicRect(), g.fontLarge, rgb(228, 240, 243))
	note := g.voiceNote
	if note == "" {
		note = "Text search / Voice search"
	}
	label(dc, note, rectF{r.x + 12, 103, r.w - 24, 30}, g.fontSmall, rgb(156, 183, 189))
	h := g.channelRowHeight()
	for i := g.chanScroll; i < len(g.visible); i++ {
		y := g.searchResultsTop() + int32(i-g.chanScroll)*h
		if y+h > g.height {
			break
		}
		ch := g.channels[g.visible[i]]
		if i == g.chanIndex {
			fill(dc, rectF{r.x + 5, y, r.w - 10, h - 2}, rgb(24, 97, 126))
		}
		label(dc, ch.Name, rectF{r.x + 12, y, r.w - 24, h * 58 / 100}, g.fontListName, rgb(228, 240, 243))
		label(dc, ch.Group, rectF{r.x + 12, y + h*55/100, r.w - 24, h * 40 / 100}, g.fontListMeta, rgb(156, 183, 189))
	}
}
func (g *nativeUI) clickSearchPanel(x, y int32) {
	if g.searchCloseRect().inside(x, y) || !g.searchPanelRect().inside(x, y) {
		g.closeSearchPanel()
		return
	}
	if g.searchMicRect().inside(x, y) {
		g.startVoiceSearch()
		return
	}
	if y >= g.searchResultsTop() {
		i := g.chanScroll + int((y-g.searchResultsTop())/g.channelRowHeight())
		if i >= 0 && i < len(g.visible) {
			actual := g.visible[i]
			g.closeSearchPanel()
			g.playChannelIndex(actual)
		}
	}
}
func (g *nativeUI) searchKey(key uintptr) {
	switch key {
	case keyEsc:
		g.closeSearchPanel()
	case keyF3:
		g.startVoiceSearch()
	case keyBackspace:
		g.cancelVoiceSearch()
		letters := []rune(g.searchText)
		if len(letters) > 0 {
			g.searchText = string(letters[:len(letters)-1])
			g.chanIndex = 0
			g.chanScroll = 0
			g.refreshVisible()
		}
	case keyUp, keyDown:
		if len(g.visible) == 0 {
			return
		}
		if key == keyUp {
			g.chanIndex--
		} else {
			g.chanIndex++
		}
		g.chanIndex = maxInt(0, int(min32(int32(len(g.visible)-1), int32(g.chanIndex))))
		rows := maxInt(1, int((g.height-g.searchResultsTop())/g.channelRowHeight()))
		if g.chanIndex < g.chanScroll {
			g.chanScroll = g.chanIndex
		}
		if g.chanIndex >= g.chanScroll+rows {
			g.chanScroll = g.chanIndex - rows + 1
		}
		g.repaint()
	case keyEnter:
		if g.chanIndex >= 0 && g.chanIndex < len(g.visible) {
			actual := g.visible[g.chanIndex]
			g.closeSearchPanel()
			g.playChannelIndex(actual)
		}
	}
}
func (g *nativeUI) startVoiceSearch() {
	if g.voiceBusy {
		return
	}
	g.cancelVoiceSearch()
	epoch := g.searchEpoch
	g.voiceBusy = true
	g.voiceNote = "Listening…"
	g.repaint()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	g.voiceCancel = cancel
	go func() {
		defer cancel()
		script := `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[System.Text.Encoding]::UTF8; Add-Type -AssemblyName System.Speech; $r=New-Object System.Speech.Recognition.SpeechRecognitionEngine; try { $r.SetInputToDefaultAudioDevice(); $r.LoadGrammar((New-Object System.Speech.Recognition.DictationGrammar)); $r.InitialSilenceTimeout=[TimeSpan]::FromSeconds(5); $r.BabbleTimeout=[TimeSpan]::FromSeconds(6); $v=$r.Recognize([TimeSpan]::FromSeconds(8)); if($v){[Console]::Write($v.Text)} } finally { $r.Dispose() }`
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		out, err := cmd.Output()
		query := strings.TrimSpace(strings.TrimPrefix(string(out), "\ufeff"))
		ok := err == nil && query != ""
		if !ok {
			query = "Voice unavailable: check microphone and installed speech language; type to search"
		}
		g.post(uiTask{kind: "voice", searchEpoch: epoch, success: ok, note: query})
	}()
}
