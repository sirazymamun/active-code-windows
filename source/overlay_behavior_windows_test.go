//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestTransparentVideoCaptureClickHidesAndReopensBothColumns(t *testing.T) {
	previous := mainUI
	defer func() { mainUI = previous }()
	g := &nativeUI{active: true, width: 1920, height: 1080, showLists: true, overlayVisible: true}
	mainUI = g
	point := uintptr(uint32(1800) | uint32(800)<<16)
	videoClickProcedure(0, msgLDown, 0, point)
	videoClickProcedure(0, msgLUp, 0, point)
	if g.overlayVisible || g.showLists {
		t.Fatal("blank playing-video click did not hide both columns")
	}
	videoClickProcedure(0, msgLDown, 0, point)
	videoClickProcedure(0, msgLUp, 0, point)
	if !g.overlayVisible || !g.showLists {
		t.Fatal("next video click did not reopen both columns")
	}
}

func TestSelectingChannelHidesListsAndIgnoresStationaryMouse(t *testing.T) {
	g := &nativeUI{active: true, showLists: true, overlayVisible: true, pointerX: 100, pointerY: 200}
	g.hideAfterChannelSelection()
	if g.showLists || g.overlayVisible {
		t.Fatal("selected channel left controls visible")
	}
	if g.meaningfulMouseMove(100, 200) {
		t.Fatal("selection mouse message reopened controls")
	}
	g.ignoreMouseUntil = time.Time{}
	if g.meaningfulMouseMove(101, 201) {
		t.Fatal("stationary jitter reopened controls")
	}
	if !g.meaningfulMouseMove(110, 210) {
		t.Fatal("real mouse movement must remain usable")
	}
}

func TestInfoMenuShowsPanelAndEscClosesIt(t *testing.T) {
	g := &nativeUI{active: true, showLists: true, overlayVisible: true}
	g.action(4)
	if !g.infoOpen || g.showLists {
		t.Fatal("Info must open its panel over video, without channel lists")
	}
	g.handleKeys(keyEsc)
	if g.infoOpen || !g.active {
		t.Fatal("Esc must dismiss Info without leaving playback")
	}
}

func TestInfoUpdatesRejectPreviousChannelMetadata(t *testing.T) {
	g := &nativeUI{active: true, infoOpen: true, playRequest: 2, tasks: []uiTask{
		{kind: "info", requestID: 2, infoLines: []string{"Current stream"}},
		{kind: "info", requestID: 1, infoLines: []string{"Old stream"}},
	}}
	g.processTasks()
	if len(g.infoLines) != 1 || g.infoLines[0] != "Current stream" {
		t.Fatal("stale stream information replaced current metadata")
	}
}

func TestMultipleAudioPopupSelectsRealTrackAndDoesNotReopen(t *testing.T) {
	tracks := []nativeAudioTrack{{ID: 5, Label: "English"}, {ID: 99, Label: "Bangla stereo", Selected: true}}
	g := &nativeUI{active: true, playRequest: 9, vlcCh: make(chan vlcRequest, 1), tasks: []uiTask{{kind: "audio", requestID: 9, audioTracks: tracks}}}
	g.processTasks()
	if !g.audioOpen || g.audioCursor != 1 {
		t.Fatal("multiple audio tracks did not automatically show the current selection")
	}
	g.handleKeys(keyEnter)
	cmd := <-g.vlcCh
	if !cmd.setAudio || cmd.audioID != 99 || cmd.requestID != 9 {
		t.Fatal("audio selection used a row index instead of VLC's actual track ID")
	}
	if g.audioOpen {
		t.Fatal("audio wizard stayed open after selection")
	}
	g.tasks = []uiTask{{kind: "audio", requestID: 9, audioTracks: tracks}}
	g.processTasks()
	if g.audioOpen {
		t.Fatal("repeated track updates reopened a completed audio choice")
	}
	g.hideAfterChannelSelection()
	g.tasks = []uiTask{{kind: "audio", requestID: 9, audioTracks: tracks}}
	g.processTasks()
	if !g.audioOpen {
		t.Fatal("a new channel did not reset automatic audio prompting")
	}
	g.closeAudioWizard()
}

func TestArchiveSwitchChangesCategoryCountsAndContentTogether(t *testing.T) {
	g := &nativeUI{active: true, channels: []channel{{Name: "Live", Type: "Live TV", Group: "Shared"}, {Name: "Film A", Type: "Movies", Group: "Shared"}, {Name: "Film B", Type: "Movies", Group: "Shared"}}}
	g.updateGroups()
	if g.groupCounts["Shared"] != 1 {
		t.Fatal("Live category total was mixed with Movies")
	}
	g.groupIndex = 1
	g.focus = 0
	g.handleKeys(keyEnter)
	if g.archiveSection != "Movies" || g.groupCounts["Shared"] != 2 || len(g.visible) != 2 {
		t.Fatal("Movies archive did not select its own categories and contents")
	}
	g.groupIndex = 3
	g.refreshVisible()
	if len(g.visible) != 2 || g.channels[g.visible[0]].Type != "Movies" {
		t.Fatal("same-named Live category leaked into Movies archive")
	}
}

func TestKeyboardAndMouseToggleDoNotReplayHiddenSelection(t *testing.T) {
	g := &nativeUI{active: true, width: 1920, height: 1080}
	g.handleKeys(0x20)
	if !g.overlayVisible || !g.showLists {
		t.Fatal("Space did not reveal controls")
	}
	g.handleKeys(0x20)
	if g.overlayVisible {
		t.Fatal("second Space did not hide controls")
	}
	if !g.revealOverlay() {
		t.Fatal("hidden controls could not be revealed")
	}
	g.click(1800, 800)
	if g.overlayVisible || g.showLists {
		t.Fatal("blank video click did not hide controls")
	}
	if !g.revealOverlay() {
		t.Fatal("controls did not remain reusable after hiding")
	}
}

func TestSeriesEpisodePageCanRestoreArchiveWithoutLosingTotals(t *testing.T) {
	base := []channel{{Type: "Live TV", Group: "News"}, {Name: "Drama", Type: "Series", Group: "Shows", SeriesID: "50"}}
	g := &nativeUI{active: true, libraryChannels: base, seriesPage: true, channels: []channel{{Name: "Episode 1", Type: "Series", Group: "Drama · Season 1"}}}
	g.updateGroups()
	if g.groupCounts["Live TV"] != 1 || g.groupCounts["Series"] != 1 || g.groups[3] != backToSeries {
		t.Fatal("episode page lost global archive navigation/totals")
	}
	g.handleKeys(keyEsc)
	if g.seriesPage || len(g.channels) != 2 || g.archiveSection != "Series" {
		t.Fatal("Back did not restore the Series library")
	}
}

func TestNativeMenuHasNoBackgroundTilesAndUsesImageLogo(t *testing.T) {
	b, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	a := strings.Index(s, "for i, t := range titles {")
	if a < 0 {
		t.Fatal("menu draw loop missing")
	}
	tail := s[a:]
	end := strings.Index(tail, "gw, cw, top, bottom :=")
	if a < 0 || end < 0 {
		t.Fatal("menu draw loop missing")
	}
	if strings.Contains(tail[:end], "fill(") || strings.Contains(tail[:end], "roundedFill(") {
		t.Fatal("menu icon backgrounds returned")
	}
	if !strings.Contains(s, "drawCornerLogo(hdc,") || strings.Contains(s, `label(hdc, "tv2u", rectF{w - 96`) {
		t.Fatal("video corner must use the image logo")
	}
	if unsafe.Sizeof(vlcTrackInfo{}) != 56 {
		t.Fatal("unexpected Win64 VLC track ABI layout")
	}
}

func TestSearchUsesIndependentRightPanelAndGlobalMatches(t *testing.T) {
	g := &nativeUI{active: true, width: 1920, height: 1080, groups: []string{"Live TV", "Movies", "Series"}, channels: []channel{{Name: "News A", Type: "Live TV", Group: "News"}, {Name: "Cinema B", Type: "Movies", Group: "Films"}}}
	g.action(0)
	if !g.searchVisible || g.showLists || g.searchPanelRect().x < 1000 {
		t.Fatal("Search must replace columns with a right panel")
	}
	g.handleCharacter('B')
	if len(g.visible) != 1 || g.visible[0] != 1 {
		t.Fatal("global text search missed a movie outside selected archive")
	}
	epoch := g.searchEpoch
	g.closeSearchPanel()
	g.tasks = []uiTask{{kind: "voice", searchEpoch: epoch, success: true, note: "stale voice"}}
	g.processTasks()
	if g.searchVisible || g.searchText != "" {
		t.Fatal("late voice result reopened search")
	}
}
func TestFrameClearIgnoresPreviousRequestAndClearsCurrent(t *testing.T) {
	frame := &heldVideoFrame{width: 1, height: 1, bgra: []byte{1, 2, 3, 255}}
	g := &nativeUI{active: true, playRequest: 2, heldFrame: frame, tasks: []uiTask{{kind: "frame-clear", requestID: 1}}}
	g.processTasks()
	if g.heldFrame == nil {
		t.Fatal("previous content uncovered new decoder")
	}
	g.tasks = []uiTask{{kind: "frame-clear", requestID: 2}}
	g.processTasks()
	if g.heldFrame != nil {
		t.Fatal("presented first frame did not release hold")
	}
}
func TestTVListGeometryFitsNamesAndMetadata(t *testing.T) {
	for _, h := range []int32{720, 1080, 2160} {
		g := &nativeUI{width: h * 16 / 9, height: h}
		if g.channelRowHeight() < max32(22, h/40)+max32(15, h/60)+4 {
			t.Fatalf("TV list row clips name/metadata at %d", h)
		}
	}
}

func TestLeftRightMovesAcrossListsEvenWhenInitiallyHidden(t *testing.T) {
	g := &nativeUI{active: true, width: 1366, height: 768, focus: 1, overlayHwnd: 1, groups: []string{"Live TV", "Movies", "Series", "News"}, channels: []channel{{Name: "A", Type: "Live TV", Group: "News"}, {Name: "B", Type: "Live TV", Group: "News"}}}
	g.updateGroups()
	g.focus = 1
	g.overlayVisible = false
	g.showLists = false
	g.handleKeys(keyLeft)
	if g.focus != 0 || !g.showLists || !g.overlayVisible {
		t.Fatal("Left only revealed columns without focusing Group/Category")
	}
	g.handleKeys(keyRight)
	if g.focus != 1 {
		t.Fatal("Right did not enter Channel column")
	}
	g.chanIndex = 1
	g.handleKeys(keyRight)
	if g.chanIndex != 1 {
		t.Fatal("Right inside Channel reset channel selection")
	}
}
func TestBottomNoticeSurvivesPlaybackAndStaleHideTimer(t *testing.T) {
	g := &nativeUI{active: true, width: 1366, height: 768, playRequest: 2}
	g.showBottomStatus("Playlist reloaded: 9 items", 6*time.Second)
	epoch := g.bottomStatusEpoch
	g.tasks = []uiTask{{kind: "play", requestID: 2, success: true, note: "Playing"}}
	g.processTasks()
	if !g.bottomStatusVisible() || g.bottomStatus != "Playlist reloaded: 9 items" {
		t.Fatal("Playing overwrote reload result")
	}
	g.showBottomStatus("Recording saved", 8*time.Second)
	previous := mainUI
	mainUI = g
	defer func() { mainUI = previous }()
	windowProcedure(0, msgBottomStatusHide, uintptr(epoch), 0)
	if g.bottomStatus != "Recording saved" {
		t.Fatal("old timer removed current result")
	}
}
func TestFirstFrameLoadingIsAnimatedAndRequestScoped(t *testing.T) {
	g := &nativeUI{active: true, playRequest: 2}
	g.startStartupLoading()
	phase := g.loadingPhase
	g.animateLoading()
	if !g.firstContentPending || phase == g.loadingPhase {
		t.Fatal("first-channel loader is not animated")
	}
	g.tasks = []uiTask{{kind: "play", requestID: 2, success: true, note: "Playing"}, {kind: "first-frame", requestID: 1}}
	g.processTasks()
	if !g.firstContentPending {
		t.Fatal("Playing/stale first frame stopped loading before presentation")
	}
	g.tasks = []uiTask{{kind: "first-frame", requestID: 2}}
	g.processTasks()
	if g.firstContentPending || !g.everPresented {
		t.Fatal("current presented frame did not remove loader")
	}
}
func TestInfoLinesAreReadableAndCompact(t *testing.T) {
	for _, height := range []int32{720, 1080, 2160} {
		g := &nativeUI{width: height * 16 / 9, height: height, infoLines: make([]string, 12)}
		w, h, row, top := g.infoMetrics()
		if row < 22 || w > g.width || h > g.height || top+14*row > h {
			t.Fatalf("information clipped at height %d", height)
		}
	}
}

func TestMovieArrowQueuesSeekWithoutOpeningColumnsOrRepeatingTap(t *testing.T) {
	g := &nativeUI{active: true, everPresented: true, playRequest: 3, playingItem: channel{Type: "Movies", URL: "file:///movie.mp4"}, vlcCh: make(chan vlcRequest, 8)}
	g.handleKeys(keyRight)
	cmd := <-g.vlcCh
	if !cmd.seek || cmd.seekPreview || cmd.seekSeconds != 10 || cmd.requestID != 3 || g.overlayVisible || g.showLists {
		t.Fatal("movie arrow revealed columns instead of seeking")
	}
	g.handleKeys(keyRight)
	if len(g.vlcCh) != 0 {
		t.Fatal("OS repeat added uncontrolled seek steps")
	}
	g.releaseVodSeek(keyRight)
	if g.seekHold.active {
		t.Fatal("release left hold timer active")
	}
	g.showLists = true
	g.overlayVisible = true
	g.focus = 1
	g.handleKeys(keyLeft)
	if g.focus != 0 || len(g.vlcCh) != 0 {
		t.Fatal("list navigation was replaced with seeking")
	}
}
func TestStaleSeekFeedbackCannotOverwriteNewMovieNotice(t *testing.T) {
	g := &nativeUI{active: true, playRequest: 4, seekToken: 9, bottomStatus: "Current status", tasks: []uiTask{{kind: "seek", requestID: 3, seekToken: 8, note: "Old seek"}}}
	g.processTasks()
	if g.bottomStatus != "Current status" {
		t.Fatal("stale seek feedback leaked")
	}
}
