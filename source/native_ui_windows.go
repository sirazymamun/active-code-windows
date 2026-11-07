//go:build windows

// A genuine Win32 user interface. No browser, WebView, Chromium or Edge process
// is launched. The LibVLC video output is a CHILD of this application window.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	wndPopup            = 0x80000000
	wndVisible          = 0x10000000
	wndChild            = 0x40000000
	wndTabStop          = 0x00010000
	wndBorder           = 0x00800000 // WS_BORDER is a WINDOW style, not an extended style
	winExClientEdge     = 0x00000200 // WS_EX_CLIENTEDGE
	editAutoHScroll     = 0x0080     // ES_AUTOHSCROLL (not a password flag)
	editPassword        = 0x0020     // ES_PASSWORD
	editNumber          = 0x2000     // ES_NUMBER
	msgGetDlgCode       = 0x0087
	wndClipChildren     = 0x02000000
	showWindow          = 5
	hideWindow          = 0
	msgCreate           = 0x0001
	msgDestroy          = 0x0002
	msgSize             = 0x0005
	msgClose            = 0x0010
	msgPaint            = 0x000F
	msgCommand          = 0x0111
	msgKeyDown          = 0x0100
	msgKeyUp            = 0x0101
	msgChar             = 0x0102
	msgLDown            = 0x0201
	msgLUp              = 0x0202
	msgMouseMove        = 0x0200
	msgMouseWheel       = 0x020A
	msgSetCursor        = 0x0020
	msgAppResult        = 0x8001
	msgOverlayHide      = 0x8002
	msgAudioHide        = 0x8003
	msgCtlColorEdit     = 0x0133
	msgTouch            = 0x0240
	keyEsc              = 0x1B
	keyEnter            = 0x0D
	keyUp               = 0x26
	keyDown             = 0x28
	keyLeft             = 0x25
	keyRight            = 0x27
	keyF11              = 0x7A
	keyBackspace        = 0x08
	keyF3               = 0x72
	keyPageUp           = 0x21
	keyPageDown         = 0x22
	editChanged         = 0x0300
	defaultCharset      = 1
	fontNormal          = 400
	fontSemiBold        = 600
	drawSingleLine      = 0x20
	drawCenter          = 0x01
	drawVCenter         = 0x04
	drawEndEllipsis     = 0x8000
	drawNoPrefix        = 0x800
	transparentText     = 1
	videoNotify         = 0x100
	videoBlackRect      = 0x04
	wndExLayered        = 0x00080000
	wndExToolWindow     = 0x00000080
	layerColorKey       = 0x00000001
	layerAlpha          = 0x00000002
	overlayVisibleAlpha = 200
	overlayHiddenAlpha  = 1 // non-zero so the invisible catcher still receives mouse input
)

var (
	user32win       = syscall.NewLazyDLL("user32.dll")
	gdi32win        = syscall.NewLazyDLL("gdi32.dll")
	winRegister     = user32win.NewProc("RegisterClassExW")
	winCreate       = user32win.NewProc("CreateWindowExW")
	winDefaultProc  = user32win.NewProc("DefWindowProcW")
	winShow         = user32win.NewProc("ShowWindow")
	winUpdate       = user32win.NewProc("UpdateWindow")
	winMessage      = user32win.NewProc("GetMessageW")
	winTranslate    = user32win.NewProc("TranslateMessage")
	winDispatch     = user32win.NewProc("DispatchMessageW")
	winPostMessage  = user32win.NewProc("PostMessageW")
	winPostQuit     = user32win.NewProc("PostQuitMessage")
	winInvalidate   = user32win.NewProc("InvalidateRect")
	winGetRect      = user32win.NewProc("GetClientRect")
	winMove         = user32win.NewProc("MoveWindow")
	winSetFocus     = user32win.NewProc("SetFocus")
	winGetText      = user32win.NewProc("GetWindowTextW")
	winSetText      = user32win.NewProc("SetWindowTextW")
	winBeginPaint   = user32win.NewProc("BeginPaint")
	winEndPaint     = user32win.NewProc("EndPaint")
	winFillRect     = user32win.NewProc("FillRect")
	winRoundRect    = gdi32win.NewProc("RoundRect")
	winEllipse      = gdi32win.NewProc("Ellipse")
	winCreatePen    = gdi32win.NewProc("CreatePen")
	winDrawText     = user32win.NewProc("DrawTextW")
	winSend         = user32win.NewProc("SendMessageW")
	winSetDPI       = user32win.NewProc("SetProcessDPIAware")
	winScreen       = user32win.NewProc("GetSystemMetrics")
	winLoadCursor   = user32win.NewProc("LoadCursorW")
	winSetBkMode    = gdi32win.NewProc("SetBkMode")
	winSetTextColor = gdi32win.NewProc("SetTextColor")
	winSetBkColor   = gdi32win.NewProc("SetBkColor")
	winCreateBrush  = gdi32win.NewProc("CreateSolidBrush")
	winSelect       = gdi32win.NewProc("SelectObject")
	winDelete       = gdi32win.NewProc("DeleteObject")
	winCreateFont   = gdi32win.NewProc("CreateFontW")
	winGetModule    = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	winSetWndProc   = user32win.NewProc("SetWindowLongPtrW")
	winLayerAttrs   = user32win.NewProc("SetLayeredWindowAttributes")
	winCallWndProc  = user32win.NewProc("CallWindowProcW")
)

type winRect struct{ Left, Top, Right, Bottom int32 }
type winPoint struct{ X, Y int32 }
type winMessageStruct struct {
	Hwnd           uintptr
	Message        uint32
	pad            uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             winPoint
	Private        uint32
}
type winPaint struct {
	HDC       uintptr
	Erase     int32
	Paint     winRect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}
type wndClass struct {
	Size, Style                   uint32
	WndProc                       uintptr
	ClassExtra, WindowExtra       int32
	Instance, Icon, Cursor, Brush uintptr
	Menu, Name                    *uint16
	IconSmall                     uintptr
}
type uiTask struct {
	seekToken       uint64
	seekPreview     bool
	searchEpoch     uint64
	frame           *heldVideoFrame
	frameAck        chan bool
	frameExpiry     time.Time
	loadEpoch       uint64
	audioTracks     []nativeAudioTrack
	infoLines       []string
	kind, note      string
	channels        []channel
	success         bool
	preserveActive  bool
	requestID       uint64
	replacePlaylist bool
	recording       bool
}
type vlcRequest struct {
	seek, seekPreview, seekCommit bool
	seekSeconds                   int64
	seekToken                     uint64
	setAudio                      bool
	audioID                       int32
	item                          channel
	stop                          bool
	quit                          bool
	requestID                     uint64
	toggleRecord                  bool
}
type rectF struct{ x, y, w, h int32 }

func (r rectF) inside(x, y int32) bool { return x >= r.x && y >= r.y && x < r.x+r.w && y < r.y+r.h }
func (r rectF) native() winRect        { return winRect{r.x, r.y, r.x + r.w, r.y + r.h} }
func min32(x, y int32) int32 {
	if x < y {
		return x
	}
	return y
}
func max32(x, y int32) int32 {
	if x > y {
		return x
	}
	return y
}
func rgb(r, g, b byte) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

var mainUI *nativeUI
var callbackProc uintptr
var overlayCallbackProc uintptr
var videoCallbackProc uintptr
var editCallbackProc uintptr
var codePreviousProc uintptr
var searchPreviousProc uintptr
var videoPreviousProc uintptr

type nativeUI struct {
	seekHold                                                                    vodSeekHold
	seekToken                                                                   uint64
	recordingPending                                                            bool
	firstContentPending, everPresented                                          bool
	loadingPhase                                                                int
	bottomStatus                                                                string
	bottomStatusUntil                                                           time.Time
	bottomStatusEpoch                                                           uint64
	reloadInProgress                                                            bool
	fontInfo                                                                    uintptr
	voiceBusy                                                                   bool
	voiceNote                                                                   string
	searchEpoch                                                                 uint64
	voiceCancel                                                                 func()
	frameHwnd                                                                   uintptr
	heldFrame                                                                   *heldVideoFrame
	videoClickHwnd                                                              uintptr
	fontListName, fontListMeta                                                  uintptr
	loadEpoch                                                                   uint64
	audioPromptURL                                                              string
	menuHits                                                                    map[uintptr]int
	vlcDone                                                                     chan struct{}
	libraryChannels                                                             []channel
	seriesPage, seriesLoading                                                   bool
	playingItem                                                                 channel
	archiveSection                                                              string
	groupCounts                                                                 map[string]int
	audioOpen, audioPrompted                                                    bool
	audioTracks                                                                 []nativeAudioTrack
	audioCursor, audioScroll                                                    int
	audioHideAt                                                                 time.Time
	audioBox                                                                    rectF
	audioRowHeight, audioHeaderHeight                                           int32
	fontAudio                                                                   uintptr
	recordingNote                                                               string
	infoOpen                                                                    bool
	infoLines                                                                   []string
	app                                                                         *application
	hwnd, videoHwnd, overlayHwnd, codeEdit, searchEdit                          uintptr
	width, height                                                               int32
	fontNormal, fontSmall, fontLarge, fontLogo, fontBold, fontSplash, fontClose uintptr
	inputBrush                                                                  uintptr
	active, checking, closed, showLists, searchVisible, activationOpen          bool
	overlayVisible                                                              bool
	overlayDeadline                                                             time.Time
	overlayTimerArmed                                                           bool
	pointerRevealOnly                                                           bool
	busy                                                                        bool
	status, activationError, searchText, selection                              string
	codeBuffer                                                                  string // fallback if Windows EDIT creation is unavailable
	groups                                                                      []string
	channels                                                                    []channel
	visible                                                                     []int
	groupIndex, groupScroll, chanIndex, chanScroll, selectedID                  int
	focus                                                                       int // 0 group, 1 channel, 2 menu
	pointerX, pointerY                                                          int32
	dragStart                                                                   time.Time
	tasks                                                                       []uiTask
	taskMu                                                                      sync.Mutex
	vlcCh                                                                       chan vlcRequest
	playRequest                                                                 uint64
	lastMouseX, lastMouseY                                                      int32
	mouseKnown                                                                  bool
	ignoreMouseUntil                                                            time.Time
	recording                                                                   bool
}

func winUTF16(value string) *uint16 { p, _ := syscall.UTF16PtrFromString(value); return p }
func makeFont(size int32, weight int32) uintptr {
	face := winUTF16("Segoe UI")
	out, _, _ := winCreateFont.Call(uintptr(-size), 0, 0, 0, uintptr(weight), 0, 0, 0, defaultCharset, 0, 0, 0, 0, uintptr(unsafe.Pointer(face)))
	return out
}
func (g *nativeUI) bounds() (int32, int32, int32, int32) {
	base := min32(30, max32(14, g.height*3/100))
	if g.height > 1080 {
		base = base * g.height / 1080
	}
	groupW := min32(g.width*24/100, base*13)
	chW := min32(g.width*32/100, base*19)
	if g.width <= 680 {
		groupW = g.width * 34 / 100
		chW = g.width - groupW - 6
	}
	if !g.showLists {
		groupW = 0
		chW = 0
	}
	return groupW, chW, 4, max32(32, g.height-4)
}
func (g *nativeUI) focusColumnAt(x int32) {
	gw, cw, _, _ := g.bounds()
	if x < gw {
		g.focus = 0
	} else if x < gw+cw {
		g.focus = 1
	}
}

func (g *nativeUI) groupRowHeight() int32 {
	if g.width <= 680 {
		return 26
	}
	return max32(34, g.height*42/1080)
}
func (g *nativeUI) channelRowHeight() int32 {
	if g.width <= 680 {
		return 33
	}
	return max32(44, g.height*54/1080)
}

func (g *nativeUI) groupListStart(top int32) int32   { return top + 4 }
func (g *nativeUI) channelListStart(top int32) int32 { return top + 4 }

func (g *nativeUI) menuRect(index int) rectF {
	cell := max32(50, min32(110, g.height/15))
	menuH := max32(42, min32(72, g.height/20))
	margin := max32(7, min32(18, g.height/120))
	return rectF{g.width - int32(5-index)*cell - margin, margin, cell - 3, menuH}
}
func (g *nativeUI) codeRect() rectF {
	// Android activation popup: centered, unboxed heading and 10-digit entry.
	w := min32(620, max32(270, g.width-36))
	h := min32(292, max32(215, g.height-28))
	return rectF{(g.width - w) / 2, (g.height - h) / 2, w, h}
}
func (g *nativeUI) welcomeButton() rectF {
	w := min32(188, max32(132, g.width/5))
	h := int32(42)
	return rectF{(g.width - w) / 2, g.height/2 + 12, w, h}
}
func (g *nativeUI) popupCloseRect() rectF {
	r := g.codeRect()
	return rectF{r.x + r.w - 54, r.y - 7, 54, 51}
}
func (g *nativeUI) welcomeTelegramRect() rectF {
	return rectF{max32(0, (g.width-228)/2), g.height - 90, 228, 48}
}
func (g *nativeUI) openActivation() {
	g.closeSearchPanel()
	if g.checking || g.busy {
		return
	}
	g.codeBuffer = ""
	g.activationError = ""
	g.activationOpen = true
	g.overlayDeadline = time.Time{}
	g.fitChildren()
	if g.codeEdit != 0 {
		winSetFocus.Call(g.codeEdit)
	} else {
		winSetFocus.Call(g.hwnd)
	}
	g.repaint()
}
func (g *nativeUI) closeActivation() {
	if g.checking {
		return
	}
	g.activationOpen = false
	g.activationError = ""
	g.codeBuffer = ""
	if g.active {
		g.overlayVisible = true
	}
	g.fitChildren()
	winSetFocus.Call(g.hwnd)
	g.armOverlayHide()
	g.repaint()
}
func (g *nativeUI) codeInputRect() rectF {
	r := g.codeRect()
	return rectF{r.x + 8, r.y + 62, r.w - 16, 65}
}
func (g *nativeUI) activateButton() rectF {
	r := g.codeRect()
	return rectF{r.x + 8, r.y + r.h - 44, r.w - 16, 37}
}
func (g *nativeUI) fitChildren() {
	if g.hwnd == 0 {
		return
	}
	groupW, chW, top, _ := g.bounds()
	if g.videoHwnd != 0 {
		if g.active && !g.activationOpen && !(g.busy && len(g.channels) == 0) {
			// Android parity: playback always owns the entire monitor. Menus never
			// resize the VLC surface; they are drawn by the layered overlay above it.
			winMove.Call(g.videoHwnd, 0, 0, uintptr(max32(1, g.width)), uintptr(max32(1, g.height)), 1)
			winShow.Call(g.videoHwnd, showWindow)
		} else {
			winShow.Call(g.videoHwnd, hideWindow)
		}
	}
	if g.overlayHwnd != 0 {
		winMove.Call(g.overlayHwnd, 0, 0, uintptr(max32(1, g.width)), uintptr(max32(1, g.height)), 1)
		if g.active && !g.activationOpen {
			// Never physically hide the layered window while playback is active.
			// LibVLC/Direct3D may own the real video child and consume mouse input, so
			// a 1/255-alpha full-screen catcher remains above the video. It is visually
			// imperceptible but reliably receives mouse movement/clicks and can reveal
			// the Android-style controls again.
			alpha := uintptr(overlayHiddenAlpha)
			if g.overlayVisible || g.audioOpen || g.bottomStatusVisible() || g.firstContentPending {
				alpha = uintptr(overlayVisibleAlpha)
			}
			winLayerAttrs.Call(g.overlayHwnd, uintptr(rgb(0, 0, 0)), alpha, uintptr(layerColorKey|layerAlpha))
			winShow.Call(g.overlayHwnd, showWindow)
		} else {
			winShow.Call(g.overlayHwnd, hideWindow)
		}
	}
	g.syncHeldFrame()
	g.syncMenuHitWindows()
	if g.codeEdit != 0 {
		if !g.checking && g.activationOpen {
			input := g.codeInputRect()
			winMove.Call(g.codeEdit, uintptr(input.x), uintptr(input.y), uintptr(input.w), uintptr(input.h), 1)
			winShow.Call(g.codeEdit, showWindow)
		} else {
			winShow.Call(g.codeEdit, hideWindow)
		}
	}
	if g.searchEdit != 0 {
		if g.active && g.showLists && g.searchVisible && g.overlayHwnd == 0 {
			x := groupW + 10
			winMove.Call(g.searchEdit, uintptr(x), uintptr(top+39), uintptr(max32(95, chW-18)), 36, 1)
			winShow.Call(g.searchEdit, showWindow)
		} else {
			winShow.Call(g.searchEdit, hideWindow)
		}
	}
}

func (g *nativeUI) repaintOverlay() {
	if g.overlayHwnd != 0 {
		winInvalidate.Call(g.overlayHwnd, 0, 0)
	}
}

func (g *nativeUI) armOverlayHide() {
	if !g.active || g.activationOpen || g.infoOpen || g.audioOpen || g.searchVisible || g.overlayHwnd == 0 || !g.overlayVisible {
		return
	}
	// Android HIDE_AFTER_MS. Mouse movement only extends this deadline; it does
	// not create a new Windows resize/repaint cycle or an unbounded timer storm.
	g.overlayDeadline = time.Now().Add(15 * time.Second)
	if g.overlayTimerArmed {
		return
	}
	g.overlayTimerArmed = true
	time.AfterFunc(15*time.Second, func() {
		winPostMessage.Call(g.hwnd, msgOverlayHide, 0, 0)
	})
}

func (g *nativeUI) revealOverlay() bool {
	g.finishVodSeek(true)
	if !g.active || g.activationOpen {
		return false
	}
	wasHidden := !g.overlayVisible
	if wasHidden {
		g.overlayVisible = true
		g.showLists = true
		g.focus = 1
		// Do not resize/reposition either the VLC surface or the layered window here.
		// Re-running MoveWindow on every mouse move caused visible fullscreen shaking.
		if g.overlayHwnd != 0 {
			winLayerAttrs.Call(g.overlayHwnd, uintptr(rgb(0, 0, 0)), uintptr(overlayVisibleAlpha), uintptr(layerColorKey|layerAlpha))
		}
		g.syncHeldFrame()
		g.syncMenuHitWindows()
		g.repaintOverlay()
	}
	g.armOverlayHide()
	return wasHidden
}

func (g *nativeUI) hideOverlay() {
	if !g.overlayVisible {
		return
	}
	g.overlayVisible = false
	g.overlayDeadline = time.Time{}
	g.overlayTimerArmed = false
	// Change only the layered alpha. Keeping both HWND geometries untouched avoids
	// a VLC/Direct3D surface resize and therefore prevents fullscreen flicker/shake.
	if g.overlayHwnd != 0 {
		alpha := uintptr(overlayHiddenAlpha)
		if g.bottomStatusVisible() || g.firstContentPending {
			alpha = uintptr(overlayVisibleAlpha)
		}
		winLayerAttrs.Call(g.overlayHwnd, uintptr(rgb(0, 0, 0)), alpha, uintptr(layerColorKey|layerAlpha))
	}
	g.syncHeldFrame()
	g.syncMenuHitWindows()
	g.repaintOverlay()
}

func (g *nativeUI) hideAfterChannelSelection() {
	g.audioOpen = false
	if g.playingItem.URL == "" || g.audioPromptURL != g.playingItem.URL {
		g.audioPrompted = false
		g.audioTracks = nil
		g.audioPromptURL = g.playingItem.URL
	}
	g.infoOpen = false
	g.showLists = false
	g.hideOverlay()
	g.overlayVisible = false
	g.overlayDeadline = time.Time{}
	g.ignoreMouseUntil = time.Now().Add(500 * time.Millisecond)
	g.lastMouseX, g.lastMouseY = g.pointerX, g.pointerY
	g.mouseKnown = true
	g.fitChildren()
}

func (g *nativeUI) dismissPlayerControls() {
	g.showLists = false
	g.hideOverlay()
	g.overlayVisible = false
	g.ignoreMouseUntil = time.Now().Add(500 * time.Millisecond)
	g.fitChildren()
	g.repaint()
}
func (g *nativeUI) togglePlayerControls() {
	g.finishVodSeek(true)
	if g.overlayVisible {
		g.dismissPlayerControls()
		return
	}
	g.showLists = true
	g.overlayVisible = true
	g.focus = 1
	g.fitChildren()
	g.armOverlayHide()
	g.repaint()
}

// Windows can repeat WM_MOUSEMOVE without physical movement after repainting.
// Those messages must not reopen the lists or keep extending the hide timer.
func (g *nativeUI) meaningfulMouseMove(x, y int32) bool {
	if time.Now().Before(g.ignoreMouseUntil) {
		g.lastMouseX, g.lastMouseY = x, y
		g.mouseKnown = true
		return false
	}
	dx, dy := x-g.lastMouseX, y-g.lastMouseY
	if g.mouseKnown && dx > -4 && dx < 4 && dy > -4 && dy < 4 {
		return false
	}
	g.lastMouseX, g.lastMouseY, g.mouseKnown = x, y, true
	return true
}

func (g *nativeUI) repaint() {
	if g.hwnd != 0 {
		winInvalidate.Call(g.hwnd, 0, 0)
	}
	g.repaintOverlay()
}

func fill(hdc uintptr, r rectF, color uint32) {
	if r.w <= 0 || r.h <= 0 {
		return
	}
	nr := r.native()
	brush, _, _ := winCreateBrush.Call(uintptr(color))
	winFillRect.Call(hdc, uintptr(unsafe.Pointer(&nr)), brush)
	winDelete.Call(brush)
}
func label(hdc uintptr, content string, r rectF, font uintptr, color uint32) {
	if r.w < 1 || r.h < 1 {
		return
	}
	old, _, _ := winSelect.Call(hdc, font)
	winSetBkMode.Call(hdc, transparentText)
	winSetTextColor.Call(hdc, uintptr(color))
	nr := r.native()
	data := winUTF16(content)
	winDrawText.Call(hdc, uintptr(unsafe.Pointer(data)), ^uintptr(0), uintptr(unsafe.Pointer(&nr)), drawSingleLine|drawVCenter|drawEndEllipsis|drawNoPrefix)
	winSelect.Call(hdc, old)
}
func centerLabel(hdc uintptr, content string, r rectF, font uintptr, color uint32) {
	if r.w < 1 || r.h < 1 {
		return
	}
	old, _, _ := winSelect.Call(hdc, font)
	winSetBkMode.Call(hdc, transparentText)
	winSetTextColor.Call(hdc, uintptr(color))
	nr := r.native()
	data := winUTF16(content)
	winDrawText.Call(hdc, uintptr(unsafe.Pointer(data)), ^uintptr(0), uintptr(unsafe.Pointer(&nr)), drawSingleLine|drawVCenter|drawCenter|drawEndEllipsis|drawNoPrefix)
	winSelect.Call(hdc, old)
}

func measureTextWidth(hdc uintptr, text string, font uintptr) int32 {
	old, _, _ := winSelect.Call(hdc, font)
	data, _ := syscall.UTF16FromString(text)
	var size struct{ X, Y int32 }
	gdi32win.NewProc("GetTextExtentPoint32W").Call(hdc, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)-1), uintptr(unsafe.Pointer(&size)))
	winSelect.Call(hdc, old)
	return size.X
}
func roundedFill(hdc uintptr, r rectF, radius int32, color uint32) {
	brush, _, _ := winCreateBrush.Call(uintptr(color))
	pen, _, _ := winCreatePen.Call(0, 1, uintptr(color))
	oldBrush, _, _ := winSelect.Call(hdc, brush)
	oldPen, _, _ := winSelect.Call(hdc, pen)
	winRoundRect.Call(hdc, uintptr(r.x), uintptr(r.y), uintptr(r.x+r.w), uintptr(r.y+r.h), uintptr(radius), uintptr(radius))
	winSelect.Call(hdc, oldPen)
	winSelect.Call(hdc, oldBrush)
	winDelete.Call(pen)
	winDelete.Call(brush)
}
func telegramIcon(hdc uintptr, r rectF, font uintptr) {
	brush, _, _ := winCreateBrush.Call(uintptr(rgb(38, 166, 226)))
	pen, _, _ := winCreatePen.Call(0, 1, uintptr(rgb(38, 166, 226)))
	oldBrush, _, _ := winSelect.Call(hdc, brush)
	oldPen, _, _ := winSelect.Call(hdc, pen)
	winEllipse.Call(hdc, uintptr(r.x), uintptr(r.y), uintptr(r.x+r.w), uintptr(r.y+r.h))
	winSelect.Call(hdc, oldPen)
	winSelect.Call(hdc, oldBrush)
	winDelete.Call(pen)
	winDelete.Call(brush)
	centerLabel(hdc, "➤", rectF{r.x + 2, r.y + 1, r.w - 4, r.h - 2}, font, rgb(255, 255, 255))
}
func (g *nativeUI) draw(hdc uintptr) {
	w, h := g.width, g.height
	fill(hdc, rectF{0, 0, w, h}, rgb(8, 27, 33))
	pink := rgb(232, 31, 110)
	cyan := rgb(82, 203, 229)
	white := rgb(228, 240, 243)
	sub := rgb(156, 183, 189)
	if !g.active || g.activationOpen {
		// Welcome and activation are distinct views, matching the Android TV player.
		fill(hdc, rectF{0, 0, w, h}, rgb(10, 16, 27))
		if !g.activationOpen && !g.checking {
			logoW := min32(290, max32(130, w*22/100))
			logoX := (w - logoW) / 2
			logoY := h/2 - 137
			if !drawWelcomeLogo(hdc, rectF{logoX, logoY, logoW, logoW * 240 / 640}) {
				label(hdc, "tv2u", rectF{logoX, logoY, logoW, 122}, g.fontSplash, pink)
			}
			b := g.welcomeButton()
			roundedFill(hdc, rectF{b.x - 4, b.y - 4, b.w + 8, b.h + 8}, 32, cyan)
			roundedFill(hdc, rectF{b.x - 1, b.y - 1, b.w + 2, b.h + 2}, 29, rgb(10, 16, 27))
			roundedFill(hdc, rectF{b.x + 2, b.y + 2, b.w - 4, b.h - 4}, 27, rgb(226, 35, 118))
			centerLabel(hdc, "Enter activation code", rectF{b.x + 9, b.y, b.w - 18, b.h}, g.fontSmall, white)
			t := g.welcomeTelegramRect()
			telegramIcon(hdc, rectF{t.x + 8, t.y + 6, 35, 35}, g.fontBold)
			label(hdc, "t.me/tv2uplayer", rectF{t.x + 52, t.y + 4, t.w - 55, t.h - 8}, g.fontNormal, rgb(196, 209, 214))
			return
		}
		box := g.codeRect()
		inputTitle := g.codeInputRect()
		centerLabel(hdc, "10 Digit Activation Code", rectF{inputTitle.x, box.y, inputTitle.w, 44}, g.fontLarge, white)
		centerLabel(hdc, "×", g.popupCloseRect(), g.fontClose, white)
		if g.checking {
			centerLabel(hdc, "Checking your subscription…", rectF{box.x + 8, box.y + 115, box.w - 16, 44}, g.fontNormal, white)
		} else {
			if g.codeEdit == 0 {
				input := g.codeInputRect()
				fill(hdc, input, rgb(10, 16, 27))
				fill(hdc, rectF{input.x, input.y, input.w, 1}, rgb(104, 115, 132))
				fill(hdc, rectF{input.x, input.y + input.h - 1, input.w, 1}, rgb(104, 115, 132))
				fill(hdc, rectF{input.x, input.y, 1, input.h}, rgb(104, 115, 132))
				fill(hdc, rectF{input.x + input.w - 1, input.y, 1, input.h}, rgb(104, 115, 132))
				value := g.codeBuffer
				if value == "" {
					value = "0 1 2 3 4 5 6 7 8 9"
				}
				centerLabel(hdc, value, rectF{input.x + 12, input.y, input.w - 24, input.h}, g.fontLarge, rgb(164, 173, 188))
			}
			centerLabel(hdc, "Get a code from your subscription vendor.", rectF{box.x + 12, box.y + 132, box.w - 24, 30}, g.fontNormal, sub)
			centerLabel(hdc, "Use it within 10 minutes.", rectF{box.x + 12, box.y + 165, box.w - 24, 28}, g.fontNormal, sub)
			notice := g.app.subscriptionNotice()
			if notice == "" {
				notice = "Enter your activation code."
			}
			centerLabel(hdc, notice, rectF{box.x + 12, box.y + 204, box.w - 24, 28}, g.fontNormal, sub)
			if g.activationError != "" {
				label(hdc, g.activationError, rectF{box.x + 12, box.y + 243, box.w - 24, 32}, g.fontSmall, rgb(252, 134, 125))
			}
		}
		return
	}
	if g.busy && len(g.channels) == 0 {
		g.drawStartupLoading(hdc)
		return
	}
	if g.overlayHwnd != 0 {
		// The full-screen VLC child paints above this owner; active controls live
		// in the separate layered overlay window.
		fill(hdc, rectF{0, 0, w, h}, rgb(0, 0, 0))
		return
	}
	g.drawPlayerOverlay(hdc, false)
}

func (g *nativeUI) drawPlayerOverlay(hdc uintptr, layered bool) {
	w, h := g.width, g.height
	cyan := rgb(82, 203, 229)
	white := rgb(228, 240, 243)
	sub := rgb(156, 183, 189)
	if g.firstContentPending {
		g.drawStartupLoading(hdc)
		g.drawBottomStatus(hdc)
		return
	}
	if layered {
		if !g.overlayVisible {
			if g.bottomStatusVisible() {
				fill(hdc, rectF{0, 0, w, h}, rgb(0, 0, 0))
				if g.audioOpen {
					g.drawAudioWizard(hdc)
				}
				g.drawBottomStatus(hdc)
				return
			}
			if g.audioOpen {
				fill(hdc, rectF{0, 0, w, h}, rgb(0, 0, 0))
				g.drawAudioWizard(hdc)
				return
			}
			// Black is the colour key and would pass mouse input through. Paint the
			// hidden catcher with a near-black NON-key colour; combined with uniform
			// alpha=1 it is effectively invisible while remaining hit-testable.
			fill(hdc, rectF{0, 0, w, h}, rgb(1, 1, 1))
			return
		}
		// Black is the layered-window colour key: only painted controls remain.
		fill(hdc, rectF{0, 0, w, h}, rgb(0, 0, 0))
	}
	if g.searchVisible {
		g.drawSearchPanel(hdc)
		return
	}
	// Android-style compact icon-only menu floats inside the video. There is no
	// permanent top bar or Windows-player title consuming video pixels.
	// Segoe UI contains these glyphs on
	// supported Windows 10/11 systems, avoiding bulky text buttons over video.
	// Android menu order: Search, Playlist, Record, Reload, Info.
	titles := []string{"⌕", "☷", "◎", "↻", "ⓘ"}
	for i, t := range titles {
		r := g.menuRect(i)
		color := white
		if i == 2 && g.recording {
			t = "■"
			color = rgb(255, 50, 80)
		}
		centerLabel(hdc, t, r, g.fontLarge, color)
	}
	gw, cw, top, bottom := g.bounds()
	if g.showLists {
		fill(hdc, rectF{3, top, max32(1, gw-3), bottom - top}, rgb(21, 38, 44))
		fill(hdc, rectF{gw, top, cw, bottom - top}, rgb(18, 36, 42))
		// Android-style panels intentionally have no Group/Category or Channel List
		// title row. Content begins immediately; Search gets only its input field.

		groupRowH := g.groupRowHeight()
		for i := g.groupScroll; i < len(g.groups); i++ {
			y := g.groupListStart(top) + int32(i-g.groupScroll)*groupRowH
			if y+groupRowH > bottom {
				break
			}
			if i == g.groupIndex {
				color := rgb(28, 60, 71)
				if g.focus == 0 {
					color = rgb(24, 97, 126)
				}
				fill(hdc, rectF{4, y, gw - 9, groupRowH - 2}, color)
				if g.focus == 0 {
					fill(hdc, rectF{4, y, 3, groupRowH - 2}, cyan)
				}
			}
			count := fmt.Sprintf("%d", g.groupCounts[g.groups[i]])
			if g.groupCounts[g.groups[i]] < 0 {
				count = ""
			}
			countWidth := max32(28, measureTextWidth(hdc, count, g.fontListMeta)+12)
			label(hdc, g.groups[i], rectF{8, y, gw - countWidth - 20, groupRowH - 1}, g.fontListName, white)
			centerLabel(hdc, count, rectF{gw - countWidth - 6, y, countWidth, groupRowH - 1}, g.fontListMeta, sub)
		}
		startY := g.channelListStart(top)
		channelRowH := g.channelRowHeight()
		for row, i := 0, g.chanScroll; i < len(g.visible); row, i = row+1, i+1 {
			y := startY + int32(row)*channelRowH
			if y+channelRowH > bottom {
				break
			}
			ch := g.channels[g.visible[i]]
			if g.chanIndex == i {
				color := rgb(28, 60, 71)
				if g.focus == 1 {
					color = rgb(23, 97, 123)
				}
				fill(hdc, rectF{gw + 5, y, cw - 10, channelRowH - 1}, color)
				if g.focus == 1 {
					fill(hdc, rectF{gw + 5, y, 3, channelRowH - 1}, cyan)
				}
			}
			label(hdc, ch.Name, rectF{gw + 12, y, cw - 22, channelRowH * 58 / 100}, g.fontListName, white)
			label(hdc, ch.Group, rectF{gw + 12, y + channelRowH*55/100, cw - 22, channelRowH * 40 / 100}, g.fontListMeta, sub)
		}
	}
	// No permanent status/footer bar: video always owns the full monitor. The
	// Android-style tv2u mark itself floats inside the video overlay.
	message := ""
	if strings.HasPrefix(g.status, "Playback unavailable:") || strings.HasPrefix(g.status, "Playlist unavailable:") {
		message = "No signal!"
	} else if strings.HasPrefix(g.status, "Loading ") || strings.HasPrefix(g.status, "Trying ") || strings.HasPrefix(g.status, "Reconnecting ") {
		message = "Loading! Please wait."
	}
	if message != "" {
		notice := rectF{(w - min32(w-24, 480)) / 2, h/2 - 32, min32(w-24, 480), 64}
		roundedFill(hdc, notice, 12, rgb(21, 38, 44))
		centerLabel(hdc, message, notice, g.fontNormal, white)
	}
	if g.audioOpen && !g.infoOpen {
		g.drawAudioWizard(hdc)
	}
	if g.infoOpen {
		g.drawStreamInfo(hdc)
	}
	logoW := min32(120, max32(72, w/16))
	logoH := logoW * 192 / 512
	drawCornerLogo(hdc, rectF{w - logoW - 10, h - logoH - 10, logoW, logoH})
	g.drawBottomStatus(hdc)
}

func (g *nativeUI) post(t uiTask) {
	g.taskMu.Lock()
	if g.closed {
		g.taskMu.Unlock()
		return
	}
	g.tasks = append(g.tasks, t)
	g.taskMu.Unlock()
	winPostMessage.Call(g.hwnd, msgAppResult, 0, 0)
}
func (g *nativeUI) takeTasks() []uiTask {
	g.taskMu.Lock()
	out := g.tasks
	g.tasks = nil
	g.taskMu.Unlock()
	return out
}
func (g *nativeUI) beginLoad(code string) {
	if g.busy {
		return
	}
	g.loadEpoch++
	epoch := g.loadEpoch
	preserveActive := code != "" && g.active
	g.busy = true
	g.checking = code != "" || !g.active
	g.activationError = ""
	g.status = "Verifying subscription…"
	g.fitChildren()
	g.repaint()
	go func() {
		var err error
		if code != "" {
			_, err = g.app.activate(code)
		} else {
			_, err = g.app.ensure(true)
		}
		if err != nil {
			g.post(uiTask{loadEpoch: epoch, kind: "auth", note: userError(err), preserveActive: preserveActive})
			return
		}
		g.post(uiTask{loadEpoch: epoch, kind: "authorized", replacePlaylist: code != ""})
		channels, err := g.app.loadPlaylist()
		if err != nil {
			g.post(uiTask{loadEpoch: epoch, kind: "load", success: false, note: playlistFailureMessage})
			return
		}
		g.post(uiTask{loadEpoch: epoch, kind: "load", success: true, channels: channels})
	}()
}
func (g *nativeUI) updateGroups() {
	g.archiveSection = "Live TV"
	if len(g.channels) > 0 && topLevelGroup(g.channels[0].Type) {
		g.archiveSection = g.channels[0].Type
	}
	g.groups, g.groupCounts = archiveGroups(g.channels, g.archiveSection)
	if g.seriesPage {
		g.groups = append(g.groups[:3], append([]string{backToSeries}, g.groups[3:]...)...)
		g.groupCounts[backToSeries] = -1
		_, totals := archiveGroups(g.libraryChannels, "Series")
		for _, kind := range []string{"Live TV", "Movies", "Series"} {
			g.groupCounts[kind] = totals[kind]
		}
	}
	g.groupIndex = 0
	if g.archiveSection == "Movies" {
		g.groupIndex = 1
	} else if g.archiveSection == "Series" {
		g.groupIndex = 2
	}
	if g.seriesPage && len(g.groups) > 4 {
		g.groupIndex = 4
	}
	g.groupScroll = 0
	g.chanScroll = 0
	g.chanIndex = 0
	g.refreshVisible()
}

func (g *nativeUI) commitArchiveGroup() {
	if g.groupIndex < 0 || g.groupIndex >= len(g.groups) {
		return
	}
	selected := g.groups[g.groupIndex]
	if g.seriesLoading {
		atomic.AddUint64(&g.playRequest, 1)
		g.seriesLoading = false
		g.busy = false
	}
	if g.seriesPage && (topLevelGroup(selected) || selected == backToSeries) {
		if selected == backToSeries {
			selected = "Series"
		}
		g.restoreSeriesLibrary(selected)
		return
	}
	if topLevelGroup(selected) && selected != g.archiveSection {
		g.archiveSection = selected
		g.groups, g.groupCounts = archiveGroups(g.channels, selected)
		g.groupScroll = 0
	}
}
func (g *nativeUI) refreshVisible() {
	g.visible = g.visible[:0]
	query := strings.ToLower(strings.TrimSpace(g.searchText))
	if g.searchVisible {
		// Android search is global: it searches the entire playlist, not only the
		// currently selected Group/Category. Name, category and content type all
		// participate so a search result can be played directly from any group.
		if query != "" {
			for i, ch := range g.channels {
				haystack := strings.ToLower(ch.Name + " " + ch.Group + " " + ch.Type)
				if strings.Contains(haystack, query) {
					g.visible = append(g.visible, i)
				}
			}
		}
	} else {
		selected := "Live TV"
		if g.groupIndex >= 0 && g.groupIndex < len(g.groups) {
			selected = g.groups[g.groupIndex]
		}
		for i, ch := range g.channels {
			if selected == "Live TV" || selected == "Movies" || selected == "Series" {
				if ch.Type != selected {
					continue
				}
			} else if ch.Group != selected || ch.Type != g.archiveSection {
				continue
			}
			g.visible = append(g.visible, i)
		}
	}
	if g.chanIndex >= len(g.visible) {
		g.chanIndex = maxInt(0, len(g.visible)-1)
	}
	g.chanScroll = 0
	g.repaint()
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func (g *nativeUI) processTasks() {
	needsLayout, infoChanged := false, false
	for _, task := range g.takeTasks() {
		if task.loadEpoch != 0 && task.loadEpoch != g.loadEpoch {
			continue
		}
		if (task.kind == "seek" || task.kind == "record" || task.kind == "first-frame" || task.kind == "frame" || task.kind == "frame-clear" || task.kind == "play" || task.kind == "info" || task.kind == "audio" || task.kind == "audio-error" || task.kind == "series") && task.requestID != 0 && task.requestID != atomic.LoadUint64(&g.playRequest) {
			continue
		}
		if task.kind == "frame" {
			accepted := time.Now().Before(task.frameExpiry) && g.active && !g.activationOpen
			if accepted {
				accepted = g.showHeldFrame(task.frame)
			}
			if task.frameAck != nil {
				task.frameAck <- accepted
			}
			continue
		}
		if task.kind == "first-frame" {
			g.stopStartupLoading(true)
			g.clearHeldFrame()
			g.fitChildren()
			g.repaint()
			winSetFocus.Call(g.hwnd)
			continue
		}
		if task.kind == "frame-clear" {
			g.stopStartupLoading(false)
			g.clearHeldFrame()
			continue
		}
		if task.kind == "voice" {
			if task.searchEpoch == g.searchEpoch && g.searchVisible {
				g.voiceBusy = false
				g.voiceCancel = nil
				g.voiceNote = task.note
				if task.success {
					g.searchText = task.note
					g.voiceNote = ""
					g.chanIndex = 0
					g.chanScroll = 0
					g.refreshVisible()
				}
				g.repaint()
			}
			continue
		}
		if task.kind == "seek" {
			if task.seekToken == g.seekToken {
				duration := 2 * time.Second
				if task.seekPreview && task.success && g.seekHold.active {
					duration = 0
				}
				g.showVodSeekNotice(task.note, duration)
			}
			continue
		}
		if task.kind == "info" {
			g.infoLines = task.infoLines
			infoChanged = true
			continue
		}
		if task.kind == "audio" {
			g.audioTracks = task.audioTracks
			if len(g.audioTracks) < 2 {
				if g.audioOpen {
					g.closeAudioWizard()
				}
			} else if g.active && !g.busy && !g.audioPrompted && !g.infoOpen && !g.activationOpen {
				g.openAudioWizard()
			} else if g.audioOpen {
				g.repaint()
			}
			continue
		}
		needsLayout = true
		switch task.kind {
		case "series":
			g.busy = false
			g.seriesLoading = false
			if task.success {
				g.libraryChannels = g.channels
				g.channels = task.channels
				g.seriesPage = true
				g.updateGroups()
				g.showLists = true
				g.overlayVisible = true
				g.focus = 1
				g.status = "Choose a season or episode"
			} else {
				g.stopStartupLoading(false)
				g.clearHeldFrame()
				g.status = "Playback unavailable: " + task.note
				g.revealOverlay()
			}
		case "audio-error":
			g.status = task.note
			g.openAudioWizard()
		case "authorized":
			if task.replacePlaylist {
				g.everPresented = false
			}
			if g.reloadInProgress {
				g.showBottomStatus("Reloading: downloading playlist…", 0)
			}
			if task.replacePlaylist {
				if g.vlcCh != nil {
					g.stopVLC()
				}
				g.libraryChannels = nil
				g.seriesPage = false
				g.seriesLoading = false
				g.channels = nil
				g.updateGroups()
			}
			g.active = true
			g.activationOpen = false
			g.checking = false
			g.status = "Loading! Please wait."
			g.startStartupLoading()
		case "auth":
			g.stopStartupLoading(false)
			if g.reloadInProgress {
				g.reloadInProgress = false
				g.showBottomStatus("Reload failed: "+task.note, 8*time.Second)
			}
			g.busy = false
			g.checking = false
			g.active = task.preserveActive
			g.activationOpen = true
			g.activationError = task.note
			g.status = "Activation required"
		case "load":
			if g.reloadInProgress {
				g.reloadInProgress = false
				if task.note != "" {
					g.showBottomStatus("Reload failed: "+task.note, 8*time.Second)
				} else {
					g.showBottomStatus(fmt.Sprintf("Playlist reloaded: %d items", len(task.channels)), 6*time.Second)
				}
			}
			g.libraryChannels = nil
			g.seriesPage = false
			g.seriesLoading = false
			g.busy = false
			g.checking = false
			g.active = true // authorization is valid even if the provider playlist is temporarily unavailable
			g.activationOpen = false
			g.showLists = true
			g.focus = 1
			g.overlayVisible = true
			g.channels = task.channels
			g.updateGroups()
			winSetFocus.Call(g.hwnd)
			if task.note != "" {
				g.stopStartupLoading(false)
				g.status = playlistFailureMessage
			} else {
				g.status = fmt.Sprintf("Playlist loaded: %d channels", len(g.channels))
				if len(g.channels) == 0 {
					g.stopStartupLoading(false)
					g.showBottomStatus(playlistFailureMessage, 8*time.Second)
				}
				if len(g.channels) > 0 {
					g.playChannelIndex(0) // Android startup rule: always begin with playlist item one
				}
			}
			g.armOverlayHide()
		case "play":
			if task.success || strings.HasPrefix(task.note, "Trying ") || strings.HasPrefix(task.note, "Reconnecting ") {
				g.status = task.note
			} else {
				g.stopStartupLoading(false)
				g.clearHeldFrame()
				g.status = "Playback unavailable: " + task.note
				g.revealOverlay()
			}
		case "record":
			g.recordingPending = false
			g.status = task.note
			g.recordingNote = task.note
			g.recording = task.recording
			g.showBottomStatus(task.note, 8*time.Second)
		case "revoked":
			g.loadEpoch++
			g.busy = false
			g.seriesLoading = false
			g.audioOpen = false
			g.infoOpen = false
			g.active = false
			g.checking = false
			g.status = "Subscription check failed: " + task.note
			g.activationOpen = true
			g.activationError = task.note
			g.stopVLC()
		}
	}
	if needsLayout {
		g.fitChildren()
		g.repaint()
	} else if infoChanged && g.infoOpen {
		g.repaint()
	}
}
func (g *nativeUI) playChannelIndex(idx int) {
	if idx < 0 || idx >= len(g.channels) {
		return
	}
	if item := g.channels[idx]; item.URL != "" && item.URL == g.playingItem.URL && (strings.HasPrefix(g.status, "Playing") || g.recording) {
		g.selectedID = idx
		g.playingItem = item
		g.hideAfterChannelSelection()
		g.repaint()
		return
	}
	if g.channels[idx].SeriesID != "" {
		g.openSeriesArchive(g.channels[idx])
		return
	}
	if g.seriesLoading {
		g.seriesLoading = false
		g.busy = false
	}
	g.finishVodSeek(false)
	requestID := atomic.AddUint64(&g.playRequest, 1)
	g.recordingPending = false
	g.recording = false
	if g.bottomStatus == "Starting recording…" || g.bottomStatus == "Stopping recording…" {
		g.bottomStatus = ""
		g.bottomStatusEpoch++
	}
	previousID := g.selectedID
	g.selectedID = idx
	item := g.channels[idx]
	g.playingItem = item
	g.infoLines = nil
	g.startStartupLoading()
	g.status = "Loading " + item.Name + "…"
	g.hideAfterChannelSelection()
	g.repaint()
	go func() {
		if previousID != idx {
			g.app.stopRecording()
		}
		if _, err := g.app.ensure(false); err != nil {
			g.post(uiTask{kind: "play", note: userError(err), requestID: requestID})
			return
		}
		if requestID != atomic.LoadUint64(&g.playRequest) {
			return
		}
		select {
		case g.vlcCh <- vlcRequest{item: item, requestID: requestID}:
		case <-time.After(8 * time.Second):
			g.post(uiTask{kind: "play", note: "VLC player is busy", requestID: requestID})
		}
	}()
}

// Revocation must not block the UI behind queued channel changes.
func (g *nativeUI) stopVLC() {
	g.finishVodSeek(false)
	g.stopStartupLoading(false)
	g.clearHeldFrame()
	atomic.AddUint64(&g.playRequest, 1)
	for {
		select {
		case g.vlcCh <- vlcRequest{stop: true}:
			return
		default:
			select {
			case <-g.vlcCh:
			default:
			}
		}
	}
}

func (g *nativeUI) playSelected() {
	if len(g.visible) == 0 || g.chanIndex < 0 || g.chanIndex >= len(g.visible) {
		return
	}
	idx := g.visible[g.chanIndex]
	g.playChannelIndex(idx)
}

func (g *nativeUI) vlcLoop() {
	if g.vlcDone != nil {
		defer close(g.vlcDone)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var vlc libvlcNative
	initialized := false
	defer vlc.close()

	// Windows uses one short request-wide startup budget. Changing decoder or
	// User-Agent cannot keep extending the spinner. Live reconnects remain
	// limited to three in a rolling 30-second window.
	const primaryTimeout = 8 * time.Second         // fast initial VLC attempt; all retries share the total budget
	const fallbackTimeout = 12 * time.Second       // bounded compatibility retry; never renew the overall request deadline
	const fallbackQueuedTimeout = 20 * time.Second // overall Windows startup ceiling, including all UA/decoder retries
	const pollEvery = 50 * time.Millisecond
	var workerRequest uint64
	postPlay := func(task uiTask) { task.requestID = workerRequest; g.post(task) }
	var current channel
	var uaIndex int
	var deadline, requestDeadline time.Time
	var reportedPlaying bool
	var holdActive bool
	var awaitFrame bool
	var reconnects []time.Time
	var fallbackMode bool
	var softwareDecode bool
	var preferSoftware, decoderFallback bool
	preferredUA := map[string]string{}
	var attemptStarted, lastProgress time.Time
	var lastDecoded int32

	userAgents := playlistUserAgents
	fixedUA := func(item channel) bool { return strings.TrimSpace(item.Headers["User-Agent"]) != "" }
	currentUA := func() string {
		if fixedUA(current) {
			return ""
		}
		if uaIndex < 0 || uaIndex >= len(userAgents) {
			return userAgents[0]
		}
		return userAgents[uaIndex]
	}
	preferredAudioDone := false
	manualAudioID := int32(-2)
	var infoSampler streamInfoSampler
	var nextInfo time.Time
	var recordStarted time.Time
	var recordConfirmed bool
	finishRecording := func() {
		if vlc.recordFile == "" {
			return
		}
		file := vlc.recordFile
		if initialized {
			vlc.stop.Call(vlc.player)
		}
		vlc.recordFile = ""
		stat, err := os.Stat(file)
		if err != nil || stat.Size() == 0 {
			os.Remove(file)
			postPlay(uiTask{kind: "record", note: "Recording failed: no stream packets were received before the recording stopped"})
		} else {
			postPlay(uiTask{kind: "record", note: fmt.Sprintf("Recording saved: %s (%d bytes)", file, stat.Size())})
		}
	}
	defer finishRecording()
	startCurrent := func() error {
		if !initialized {
			if err := vlc.load(); err != nil {
				return err
			}
			vlc.render(g.videoHwnd)
			initialized = true
		}
		preferredAudioDone = false
		infoSampler = streamInfoSampler{}
		nextInfo = time.Time{}
		err := vlc.change(current, currentUA(), softwareDecode)
		attemptStarted = time.Now()
		lastProgress = attemptStarted
		lastDecoded = 0
		return err
	}
	reset := func(item channel) error {
		resetStarted := time.Now()
		awaitFrame = true
		// Paint the old picture before VLC resets its native output surface. A
		// rapid zap preserves the existing hold until the latest request presents.
		if initialized && !holdActive {
			if picture := vlc.transitionSnapshot(); picture != nil {
				ack := make(chan bool, 1)
				expiry := time.Now().Add(200 * time.Millisecond)
				g.post(uiTask{kind: "frame", frame: makeHeldVideoFrame(picture), frameAck: ack, frameExpiry: expiry, requestID: workerRequest})
				select {
				case holdActive = <-ack:
				case <-time.After(time.Until(expiry)):
				}
			}
		}
		finishRecording()
		if current.URL != item.URL {
			manualAudioID = -2
		}
		current = item
		preferredAudioDone = false
		userAgents = playbackUserAgents(preferredUA[playbackProvider(item.URL)])
		uaIndex = 0
		fallbackMode = preferSoftware
		softwareDecode = preferSoftware
		decoderFallback = false
		requestDeadline = resetStarted.Add(fallbackQueuedTimeout)
		deadline = resetStarted.Add(primaryTimeout)
		reportedPlaying = false
		reconnects = nil
		return startCurrent()
	}
	stopCurrent := func() {
		postPlay(uiTask{kind: "frame-clear"})
		awaitFrame = false
		holdActive = false
		finishRecording()
		if initialized && vlc.player != 0 {
			vlc.stop.Call(vlc.player)
		}
		current = channel{}
		reportedPlaying = false
		reconnects = nil
		fallbackMode = false
		if initialized && vlc.media != 0 {
			g.post(uiTask{kind: "info", infoLines: infoSampler.snapshot(&vlc, softwareDecode), requestID: workerRequest})
		}
	}

	toggleRecording := func() {
		if current.URL == "" || current.Type != "Live TV" {
			postPlay(uiTask{kind: "record", note: "Choose a playing Live TV channel before recording"})
			return
		}
		if vlc.recordFile != "" {
			finishRecording()
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				postPlay(uiTask{kind: "record", note: userError(err)})
				return
			}
			dir := filepath.Join(home, "Videos", "active-code")
			if err := os.MkdirAll(dir, 0700); err != nil {
				postPlay(uiTask{kind: "record", note: userError(err)})
				return
			}
			name := regexp.MustCompile(`[^\pL\pN._ -]+`).ReplaceAllString(current.Name, "_")
			name = strings.Trim(name, ". ")
			if name == "" {
				name = "channel"
			}
			if len([]rune(name)) > 55 {
				name = string([]rune(name)[:55])
			}
			file, err := os.CreateTemp(dir, name+"_"+time.Now().Format("20060102_150405")+"_*.ts")
			if err != nil {
				postPlay(uiTask{kind: "record", note: userError(err)})
				return
			}
			vlc.recordFile = file.Name()
			file.Close()
			recordStarted = time.Now()
			recordConfirmed = false
			postPlay(uiTask{kind: "record", note: "Starting recording…", recording: true})
		}
		reportedPlaying = false
		requestDeadline = time.Now().Add(fallbackQueuedTimeout)
		deadline = time.Now().Add(primaryTimeout)
		if err := startCurrent(); err != nil {
			finishRecording()
			postPlay(uiTask{kind: "play", note: userError(err)})
		}
	}

	var seekPlan vodSeekPlan
	handleSeek := func(cmd vlcRequest) {
		note := "This content is not seekable yet"
		postSeek := func(text string, ok bool) {
			g.post(uiTask{kind: "seek", note: text, success: ok, requestID: cmd.requestID, seekToken: cmd.seekToken, seekPreview: cmd.seekPreview})
		}
		if !initialized || cmd.requestID != workerRequest || current.URL == "" {
			postSeek("Wait for the selected movie or episode to start", false)
			return
		}
		if !canUseVodArrow(current, false, 0x27) {
			return
		}
		if !vlc.canSeek() {
			postSeek(note, false)
			return
		}
		position, duration := vlc.seekTimeline()
		if position < 0 {
			postSeek(note, false)
			return
		}
		target := seekPlan.target(cmd.requestID, cmd.seekToken, position, cmd.seekSeconds, duration, time.Now())
		if !cmd.seekPreview {
			vlc.seekTo(target)
			seekPlan.applied(target, time.Now())
			lastProgress = time.Now()
		}
		direction := "+"
		seconds := cmd.seekSeconds
		if seconds < 0 {
			direction = "−"
			seconds = -seconds
		}
		text := fmt.Sprintf("Seek %s%ds · %s", direction, seconds, seekTimeLabel(target))
		if duration > 0 {
			text += " / " + seekTimeLabel(duration)
		}
		postSeek(text, true)
	}

	for {
		if current.URL == "" {
			cmd, ok := <-g.vlcCh
			if !ok || cmd.quit {
				return
			}
			if cmd.stop {
				stopCurrent()
				continue
			}
			if cmd.requestID != atomic.LoadUint64(&g.playRequest) {
				continue
			}
			if cmd.seek {
				handleSeek(cmd)
				continue
			}
			workerRequest = cmd.requestID
			if cmd.setAudio {
				if initialized && vlc.selectAudio(cmd.audioID) {
					manualAudioID = cmd.audioID
					preferredAudioDone = true
				} else {
					g.post(uiTask{kind: "audio-error", note: "Audio track is not available yet; retry", requestID: workerRequest})
				}
				continue
			}
			if cmd.toggleRecord {
				toggleRecording()
				continue
			}
			if cmd.item.URL == "" {
				continue
			}
			if err := reset(cmd.item); err != nil {
				postPlay(uiTask{kind: "play", note: userError(err)})
				current = channel{}
			}
			continue
		}

		timer := time.NewTimer(pollEvery)
		select {
		case cmd, ok := <-g.vlcCh:
			if !timer.Stop() {
				<-timer.C
			}
			if !ok || cmd.quit {
				return
			}
			if cmd.stop {
				stopCurrent()
				continue
			}
			if cmd.requestID != atomic.LoadUint64(&g.playRequest) {
				continue
			}
			if cmd.seek {
				handleSeek(cmd)
				continue
			}
			workerRequest = cmd.requestID
			if cmd.setAudio {
				if initialized && vlc.selectAudio(cmd.audioID) {
					manualAudioID = cmd.audioID
					preferredAudioDone = true
				} else {
					g.post(uiTask{kind: "audio-error", note: "Audio track is not available yet; retry", requestID: workerRequest})
				}
				continue
			}
			if cmd.toggleRecord {
				toggleRecording()
				continue
			}
			if cmd.item.URL != "" {
				if err := reset(cmd.item); err != nil {
					postPlay(uiTask{kind: "play", note: userError(err)})
					current = channel{}
				}
			}
		case <-timer.C:
			if !initialized || current.URL == "" {
				continue
			}
			if awaitFrame && vlc.presentedFirstFrame() {
				awaitFrame = false
				postPlay(uiTask{kind: "first-frame"})
				holdActive = false
			}
			state := vlc.state()
			now := time.Now()
			if now.After(nextInfo) {
				tracks := vlc.audioTracks()
				currentAudio := int32(-1)
				for _, t := range tracks {
					if t.Selected {
						currentAudio = t.ID
					}
				}
				wanted := currentAudio
				if manualAudioID >= 0 {
					wanted = manualAudioID
				} else if !preferredAudioDone {
					wanted = preferredAudioTrack(tracks, currentAudio)
				}
				if wanted >= 0 && wanted != currentAudio {
					if vlc.selectAudio(wanted) {
						preferredAudioDone = true
						tracks = vlc.audioTracks()
					}
				} else if currentAudio >= 0 {
					preferredAudioDone = true
				}
				g.post(uiTask{kind: "audio", audioTracks: tracks, requestID: workerRequest})
				g.post(uiTask{kind: "info", infoLines: infoSampler.snapshot(&vlc, softwareDecode), requestID: workerRequest})
				nextInfo = now.Add(time.Second)
			}
			if vlc.recordFile != "" {
				stat, err := os.Stat(vlc.recordFile)
				if !recordConfirmed && err == nil && stat.Size() > 0 {
					recordConfirmed = true
					postPlay(uiTask{kind: "record", note: "Recording: " + vlc.recordFile, recording: true})
				}
				if (err == nil && stat.Size() >= maxRecordBytes) || now.Sub(recordStarted) >= maxRecordDuration || recordingHasTimedOut(recordStarted, now, recordingFileSize(stat, err)) {
					finishRecording()
					if err := startCurrent(); err != nil {
						postPlay(uiTask{kind: "play", note: userError(err)})
					}
				}
			}
			if (!reportedPlaying || awaitFrame) && !now.Before(requestDeadline) {
				postPlay(uiTask{kind: "play", note: "No signal! Startup exceeded the 20-second total budget"})
				stopCurrent()
				continue
			}
			video, audio, videoExpected, haveStats := vlc.decodedProgress()
			decoded := audio
			if videoExpected {
				decoded = video
			}
			if decoded != lastDecoded {
				lastDecoded = decoded
				lastProgress = now
			}
			// VLC can remain in Playing with no decoded video, or after a dead
			// network connection. Check decoder progress as Android checks frames.
			if state == 3 && haveStats && reportedPlaying && current.Type == "Live TV" && now.Sub(lastProgress) > 7*time.Second {
				state = 7 // use the bounded live reconnect path below
			}
			switch state {
			case 3: // libvlc_Playing
				if haveStats && videoExpected && video == 0 && !softwareDecode && now.Sub(attemptStarted) > 6*time.Second {
					softwareDecode = true
					decoderFallback = true
					fallbackMode = true
					deadline = boundedPlaybackDeadline(now, fallbackTimeout, requestDeadline)
					if err := startCurrent(); err == nil {
						postPlay(uiTask{kind: "play", note: "Trying software video decoding…"})
						continue
					}
				}
				if !reportedPlaying && (!haveStats || video > 0 || (!videoExpected && audio > 0)) {
					reportedPlaying = true
					if !fixedUA(current) {
						preferredUA[playbackProvider(current.URL)] = currentUA()
					}
					if decoderFallback && softwareDecode && video > 0 {
						preferSoftware = true
					}
					postPlay(uiTask{kind: "play", success: true, note: "Playing · VLC 3.0.24 Stable"})
				}
			case 6, 7: // Ended / Error
				if !reportedPlaying {
					if !fallbackMode {
						fallbackMode = true
						uaIndex = 0
						softwareDecode = true // Retry the same provider identity with software decoding first.
						deadline = boundedPlaybackDeadline(now, fallbackTimeout, requestDeadline)
						if err := startCurrent(); err == nil {
							postPlay(uiTask{kind: "play", note: "Trying compatibility fallback…"})
							continue
						}
					}
					if !fixedUA(current) && fallbackMode && uaIndex+1 < len(userAgents) && now.Before(deadline) {
						uaIndex++
						if err := startCurrent(); err == nil {
							continue
						}
					}
					postPlay(uiTask{kind: "play", note: "No signal! Stream failed before playback started"})
					stopCurrent()
					continue
				}
				if current.Type == "Live TV" {
					kept := reconnects[:0]
					for _, t := range reconnects {
						if now.Sub(t) < 30*time.Second {
							kept = append(kept, t)
						}
					}
					reconnects = kept
					if len(reconnects) < 3 {
						reconnects = append(reconnects, now)
						reportedPlaying = false
						fallbackMode = softwareDecode
						uaIndex = 0
						requestDeadline = now.Add(fallbackQueuedTimeout)
						deadline = now.Add(primaryTimeout)
						if err := startCurrent(); err == nil {
							postPlay(uiTask{kind: "play", note: "Reconnecting live stream…"})
							continue
						}
					}
					postPlay(uiTask{kind: "play", note: "No signal! Live stream ended after 3 reconnect attempts"})
				}
				stopCurrent()
			}
			if current.URL != "" && !reportedPlaying && now.After(deadline) {
				if !fallbackMode {
					fallbackMode = true
					uaIndex = 0
					softwareDecode = true
					deadline = boundedPlaybackDeadline(now, fallbackTimeout, requestDeadline)
					if err := startCurrent(); err == nil {
						postPlay(uiTask{kind: "play", note: "Trying compatibility fallback…"})
						continue
					}
				}
				postPlay(uiTask{kind: "play", note: "No signal! Compatibility retry timed out"})
				stopCurrent()
			}
		}
	}
}

func (g *nativeUI) recordAction() {
	if g.recordingPending {
		return
	}
	if g.playingItem.URL == "" || g.playingItem.Type != "Live TV" {
		g.status = "Choose a Live TV channel before recording"
		g.showBottomStatus(g.status, 6*time.Second)
		return
	}
	g.recordingPending = true
	g.dismissPlayerControls()
	if g.recording {
		g.showBottomStatus("Stopping recording…", 0)
	} else {
		g.showBottomStatus("Starting recording…", 0)
	}
	requestID := atomic.LoadUint64(&g.playRequest)
	wasRecording := g.recording
	go func() {
		if _, err := g.app.ensure(false); err != nil {
			g.post(uiTask{kind: "record", requestID: requestID, note: "Recording: " + userError(err), recording: wasRecording})
			return
		}
		select {
		case g.vlcCh <- vlcRequest{toggleRecord: true, requestID: requestID}:
		case <-time.After(2 * time.Second):
			g.post(uiTask{kind: "record", requestID: requestID, note: "Player busy; retry recording", recording: wasRecording})
		}
	}()
}

func (g *nativeUI) action(index int) {
	if !g.active {
		return
	}
	switch index {
	case 0:
		if g.searchVisible {
			g.closeSearchPanel()
		} else {
			g.openSearchPanel()
		}
	case 1:
		g.openActivation()
	case 2:
		g.recordAction()
	case 3:
		if g.busy {
			g.showBottomStatus("A playlist operation is already in progress", 4*time.Second)
			break
		}
		g.reloadInProgress = true
		g.showBottomStatus("Reloading playlist…", 0)
		g.status = "Refreshing playlist…"
		g.beginLoad("")
	case 4:
		g.audioOpen = false
		g.showLists = false
		g.infoOpen = !g.infoOpen
		g.overlayVisible = true
		g.overlayDeadline = time.Time{}
		if !g.infoOpen {
			g.armOverlayHide()
		}

	}
	g.fitChildren()
	g.repaint()
}
func (g *nativeUI) moveSelection(delta int) {
	if g.focus == 0 {
		if len(g.groups) == 0 {
			return
		}
		g.groupIndex = maxInt(0, g.groupIndex+delta)
		if g.groupIndex >= len(g.groups) {
			g.groupIndex = len(g.groups) - 1
		}
		_, _, top, bottom := g.bounds()
		display := maxInt(1, int((bottom-g.groupListStart(top))/g.groupRowHeight()))
		if g.groupIndex < g.groupScroll {
			g.groupScroll = g.groupIndex
		}
		if g.groupIndex >= g.groupScroll+display {
			g.groupScroll = g.groupIndex - display + 1
		}
		g.chanIndex = 0
		g.refreshVisible()
	} else {
		if len(g.visible) == 0 {
			return
		}
		g.chanIndex = maxInt(0, g.chanIndex+delta)
		if g.chanIndex >= len(g.visible) {
			g.chanIndex = len(g.visible) - 1
		}
		_, _, top, bottom := g.bounds()
		display := maxInt(1, int((bottom-g.channelListStart(top))/g.channelRowHeight()))
		if g.chanIndex < g.chanScroll {
			g.chanScroll = g.chanIndex
		}
		if g.chanIndex >= g.chanScroll+display {
			g.chanScroll = g.chanIndex - display + 1
		}
	}
	g.repaint()
}
func (g *nativeUI) click(x, y int32) {
	if g.searchVisible {
		g.clickSearchPanel(x, y)
		return
	}
	if g.audioOpen && !g.infoOpen {
		if g.audioBox.inside(x, y) {
			if y < g.audioBox.y+g.audioHeaderHeight {
				g.closeAudioWizard()
				return
			}
			if g.audioRowHeight > 0 {
				g.chooseAudio(g.audioScroll + int((y-g.audioBox.y-g.audioHeaderHeight)/g.audioRowHeight))
			}
			return
		}
		g.closeAudioWizard()
	}
	if g.infoOpen && g.active && !g.activationOpen {
		if g.infoCloseRect().inside(x, y) || g.menuRect(4).inside(x, y) {
			g.infoOpen = false
			g.syncHeldFrame()
			g.syncMenuHitWindows()
			g.armOverlayHide()
			g.repaint()
		}
		return
	}
	if !g.active || g.activationOpen {
		if !g.activationOpen && !g.checking {
			if g.welcomeButton().inside(x, y) {
				g.openActivation()
			}
			if g.welcomeTelegramRect().inside(x, y) {
				openTelegramSupport()
			}
		} else if g.popupCloseRect().inside(x, y) {
			g.closeActivation()
		} else if g.activationOpen && g.codeInputRect().inside(x, y) {
			winSetFocus.Call(g.hwnd)
		}
		return
	}
	if g.overlayHwnd != 0 {
		g.revealOverlay()
	}
	for i := 0; i < 5; i++ {
		if g.menuRect(i).inside(x, y) {
			g.action(i)
			return
		}
	}
	if !g.showLists {
		g.dismissPlayerControls()
		return
	}
	gw, cw, top, _ := g.bounds()
	if x < gw && y >= g.groupListStart(top) {
		idx := int((y-g.groupListStart(top))/g.groupRowHeight()) + g.groupScroll
		if idx >= 0 && idx < len(g.groups) {
			g.groupIndex = idx
			g.commitArchiveGroup()
			g.focus = 0
			g.chanIndex = 0
			g.refreshVisible()
		}
	} else if x >= gw && x < gw+cw && y >= g.channelListStart(top) {
		idx := int((y-g.channelListStart(top))/g.channelRowHeight()) + g.chanScroll
		if idx >= 0 && idx < len(g.visible) {
			g.chanIndex = idx
			g.focus = 1
			if g.searchVisible {
				actual := g.visible[idx]
				g.searchVisible = false
				g.searchText = ""
				g.refreshVisible()
				g.playChannelIndex(actual)
			} else {
				g.playSelected()
			}
		}
	} else {
		g.dismissPlayerControls()
	}
	winSetFocus.Call(g.hwnd)
	g.repaint()
}
func (g *nativeUI) submitActivation() {
	if g.checking || g.busy || !g.activationOpen {
		return
	}
	code := g.codeBuffer
	if g.codeEdit != 0 {
		data := make([]uint16, 32)
		winGetText.Call(g.codeEdit, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
		code = syscall.UTF16ToString(data)
	}
	if len(code) != 10 || strings.Trim(code, "0123456789") != "" {
		g.activationError = "Enter exactly 10 digits"
		g.repaint()
		return
	}
	g.beginLoad(code)
}
func (g *nativeUI) handleKeys(key uintptr) {
	if key == keyEsc && (g.seekHold.active || (g.bottomStatusVisible() && strings.HasPrefix(g.bottomStatus, "Seek "))) {
		if g.seekHold.active {
			g.finishVodSeek(false)
		} else {
			g.seekToken++
		}
		g.showVodSeekNotice("", 0)
		return
	}

	if g.active && !g.activationOpen && !g.checking && !g.searchVisible && !g.infoOpen && !g.audioOpen && !g.firstContentPending && canUseVodArrow(g.playingItem, g.showLists && g.overlayVisible, key) {
		g.beginVodSeek(key)
		return
	}
	if g.seekHold.active {
		g.finishVodSeek(true)
	}
	if g.searchVisible {
		g.searchKey(key)
		return
	}
	if g.audioOpen && !g.infoOpen {
		switch key {
		case keyUp:
			g.moveAudio(-1)
			return
		case keyDown:
			g.moveAudio(1)
			return
		case keyEnter, 0x20:
			g.chooseAudio(g.audioCursor)
			return
		case keyEsc:
			g.closeAudioWizard()
			return
		case keyLeft, keyRight:
			g.closeAudioWizard()
		}
	}
	if key == 0x41 && g.active && !g.activationOpen && !g.searchVisible && !g.infoOpen {
		g.openAudioWizard()
		return
	}
	if g.infoOpen && g.active && !g.activationOpen {
		if key == keyEsc {
			g.infoOpen = false
			g.syncHeldFrame()
			g.syncMenuHitWindows()
			g.armOverlayHide()
			g.repaint()
		}
		return
	}
	if key == keyEsc && g.seriesLoading {
		atomic.AddUint64(&g.playRequest, 1)
		g.seriesLoading = false
		g.busy = false
		g.status = "Series loading cancelled"
		g.repaint()
		return
	}
	if key == keyEsc && g.seriesPage {
		g.restoreSeriesLibrary("Series")
		return
	}
	if (key == 0x20 || key == 0x5D) && g.active && !g.activationOpen && !g.searchVisible {
		g.togglePlayerControls()
		return
	}
	if !g.active || g.activationOpen {
		if !g.activationOpen && !g.checking {
			if key == keyEnter {
				g.openActivation()
			} else if key == keyEsc {
				user32win.NewProc("DestroyWindow").Call(g.hwnd)
			}
			return
		}
		if key == keyEsc {
			g.closeActivation()
			return
		}
		if key == keyEnter && !g.checking {
			g.submitActivation()
		}
		if key == keyBackspace && g.codeEdit == 0 && !g.checking && len(g.codeBuffer) > 0 {
			g.codeBuffer = g.codeBuffer[:len(g.codeBuffer)-1]
			g.repaint()
		}
		return
	}
	// Esc is intentionally handled before generic overlay reveal. First Esc hides
	// the Android-style overlay; a second Esc while it is already hidden exits.
	if key == keyEsc && g.overlayHwnd != 0 {
		if g.searchVisible {
			g.searchVisible = false
			g.searchText = ""
			g.refreshVisible()
			return
		}
		if g.overlayVisible {
			g.showLists = false
			g.hideOverlay()
			return
		}
		user32win.NewProc("DestroyWindow").Call(g.hwnd)
		return
	}
	if g.overlayHwnd != 0 {
		wasHidden := g.revealOverlay()
		if wasHidden && (key == keyUp || key == keyDown || key == keyEnter) {
			// Match Android's anti-zap rule: the first key only reveals the browser.
			return
		}
	}
	if g.searchVisible && g.searchEdit == 0 {
		switch key {
		case keyBackspace:
			if len(g.searchText) > 0 {
				letters := []rune(g.searchText)
				g.searchText = string(letters[:len(letters)-1])
				g.chanIndex = 0
				g.refreshVisible()
			}
		case keyUp:
			g.focus = 1
			g.moveSelection(-1)
		case keyDown:
			g.focus = 1
			g.moveSelection(1)
		case keyEnter:
			if len(g.visible) > 0 && g.chanIndex >= 0 && g.chanIndex < len(g.visible) {
				actual := g.visible[g.chanIndex]
				g.searchVisible = false
				g.searchText = ""
				g.refreshVisible()
				g.playChannelIndex(actual)
			}
		case keyEsc:
			g.searchVisible = false
			g.searchText = ""
			g.refreshVisible()
		}
		return
	}
	switch key {
	case keyEsc:
		if g.searchVisible {
			g.action(0)
		} else {
			if g.showLists {
				g.showLists = false
				g.fitChildren()
				g.repaint()
			} else {
				user32win.NewProc("DestroyWindow").Call(g.hwnd)
			}
		}
	case keyUp:
		g.moveSelection(-1)
	case keyDown:
		g.moveSelection(1)
	case keyLeft, keyRight:
		focus, commit := navigateListFocus(g.focus, key)
		if commit {
			g.commitArchiveGroup()
			g.refreshVisible()
		}
		g.focus = focus
		g.showLists = true
		g.fitChildren()
		g.repaint()

	case keyEnter:
		if g.focus == 1 {
			g.playSelected()
		} else {
			g.commitArchiveGroup()
			g.refreshVisible()
			g.focus = 1
			g.repaint()
		}
	case keyPageUp:
		g.moveSelection(-8)
	case keyPageDown:
		g.moveSelection(8)
	case keyF3:
		g.action(0)
	case keyF11:
		g.showLists = !g.showLists
		g.fitChildren()
		g.repaint()
	}
}
func (g *nativeUI) handleCharacter(code uintptr) {
	if g.infoOpen {
		return
	}
	if !g.active || g.activationOpen {
		if g.activationOpen && g.codeEdit == 0 && !g.checking && code >= '0' && code <= '9' && len(g.codeBuffer) < 10 {
			g.codeBuffer += string(rune(code))
			g.activationError = ""
			if len(g.codeBuffer) == 10 {
				g.submitActivation()
			}
			g.repaint()
		}
		return
	}
	if g.searchVisible && g.searchEdit == 0 && code >= 32 && code != 127 && code <= 0xffff && len([]rune(g.searchText)) < 100 {
		g.cancelVoiceSearch()
		g.searchText += string(rune(code))
		g.chanIndex = 0
		g.refreshVisible()
	}
}
func lowSigned(v uintptr) int32  { return int32(int16(uint16(v & 0xffff))) }
func highSigned(v uintptr) int32 { return int32(int16(uint16((v >> 16) & 0xffff))) }

// Native EDIT controls handle their own keystrokes, so intercept Enter/Esc
// explicitly rather than relying on an HTTP page or browser shortcuts.
func editWindowProcedure(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	g := mainUI
	if g != nil && message == msgKeyDown {
		if hwnd == g.codeEdit && wParam == keyEnter {
			g.submitActivation()
			return 0
		}
		if hwnd == g.codeEdit && wParam == keyEsc {
			g.closeActivation()
			return 0
		}
		if hwnd == g.searchEdit && (wParam == keyEnter || wParam == keyEsc) {
			g.searchVisible = false
			g.fitChildren()
			winSetFocus.Call(g.hwnd)
			g.repaint()
			return 0
		}
	}
	previous := codePreviousProc
	if g != nil && hwnd == g.searchEdit {
		previous = searchPreviousProc
	}
	if previous == 0 {
		return defaultWnd(hwnd, message, wParam, lParam)
	}
	out, _, _ := winCallWndProc.Call(previous, hwnd, uintptr(message), wParam, lParam)
	return out
}
func windowProcedure(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	g := mainUI
	if g == nil {
		return defaultWnd(hwnd, message, wParam, lParam)
	}
	switch message {
	case msgClose:
		user32win.NewProc("DestroyWindow").Call(hwnd)
		return 0
	case msgDestroy:
		g.finishVodSeek(false)
		g.clearHeldFrame()
		g.stopStartupLoading(false)
		g.cancelVoiceSearch()
		g.taskMu.Lock()
		g.closed = true
		g.taskMu.Unlock()
		g.app.stopRecording()
		select {
		case g.vlcCh <- vlcRequest{quit: true}:
		default:
		}
		winPostQuit.Call(0)
		return 0
	case msgSize:
		var r winRect
		winGetRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		g.width = r.Right - r.Left
		g.height = r.Bottom - r.Top
		g.fitChildren()
		g.repaint()
		return 0
	case msgPaint:
		var ps winPaint
		hdc, _, _ := winBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			g.draw(hdc)
		}
		winEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case msgCtlColorEdit:
		if lParam == g.codeEdit && g.inputBrush != 0 {
			winSetTextColor.Call(wParam, uintptr(rgb(187, 195, 211)))
			winSetBkColor.Call(wParam, uintptr(rgb(10, 16, 27)))
			return g.inputBrush
		}
		return defaultWnd(hwnd, message, wParam, lParam)
	case 0x001c: // WM_ACTIVATEAPP: do not cancel when our own overlay gains focus
		if wParam == 0 {
			g.finishVodSeek(true)
		}
		return 0
	case msgKeyUp:
		if g.releaseVodSeek(wParam) {
			return 0
		}
	case 0x0113: // WM_TIMER
		if wParam == vodSeekTimer {
			g.tickVodSeek()
			return 0
		}
		if wParam == loadingAnimationTimer {
			g.animateLoading()
		}
		return 0
	case msgAppResult:
		g.processTasks()
		return 0
	case msgBottomStatusHide:
		if uint64(wParam) == g.bottomStatusEpoch && !g.bottomStatusUntil.IsZero() && !time.Now().Before(g.bottomStatusUntil) {
			if strings.HasPrefix(g.bottomStatus, "Seek ") {
				g.showVodSeekNotice("", 0)
				return 0
			}
			g.bottomStatus = ""
			g.fitChildren()
			g.repaint()
		}
		return 0
	case msgAudioHide:
		if uint64(wParam) == atomic.LoadUint64(&g.playRequest) && g.audioOpen && !time.Now().Before(g.audioHideAt) {
			g.closeAudioWizard()
		}
		return 0
	case msgOverlayHide:
		if g.overlayHwnd != 0 && g.overlayVisible && !g.overlayDeadline.IsZero() {
			remaining := time.Until(g.overlayDeadline)
			if remaining <= 0 {
				g.hideOverlay()
			} else {
				// Activity extended the deadline after the one timer was armed. Reuse
				// the same logical timer instead of allocating one timer per mouse move.
				time.AfterFunc(remaining, func() {
					winPostMessage.Call(g.hwnd, msgOverlayHide, 0, 0)
				})
			}
		} else {
			g.overlayTimerArmed = false
		}
		return 0
	case msgMouseMove:
		if g.active && g.meaningfulMouseMove(lowSigned(lParam), highSigned(lParam)) {
			if g.overlayVisible {
				g.armOverlayHide()
			}
		}
		return 0
	case msgKeyDown:
		g.handleKeys(wParam)
		return 0
	case msgChar:
		g.handleCharacter(wParam)
		return 0
	case msgLDown:
		g.pointerX, g.pointerY = lowSigned(lParam), highSigned(lParam)
		g.dragStart = time.Now()
		return 0
	case msgLUp:
		x, y := lowSigned(lParam), highSigned(lParam)
		dy := y - g.pointerY
		dx := x - g.pointerX
		if time.Since(g.dragStart) < 1500*time.Millisecond && (dy > 50 || dy < -50) {
			if dy > 0 {
				g.moveSelection(-1)
			} else {
				g.moveSelection(1)
			}
		} else if dx > 95 || dx < -95 {
			g.showLists = dx > 0
			g.fitChildren()
			g.repaint()
		} else {
			g.click(x, y)
		}
		return 0
	case msgMouseWheel:
		if g.searchVisible {
			if highSigned(wParam) > 0 {
				g.searchKey(keyUp)
			} else {
				g.searchKey(keyDown)
			}
			return 0
		}
		if g.audioOpen && !g.infoOpen {
			if highSigned(wParam) > 0 {
				g.moveAudio(-1)
			} else {
				g.moveAudio(1)
			}
			return 0
		}
		g.focusColumnAt(lowSigned(lParam))
		delta := highSigned(wParam)
		if delta > 0 {
			g.moveSelection(-1)
		} else {
			g.moveSelection(1)
		}
		return 0
	case msgCommand:
		id := uint16(wParam & 0xffff)
		event := uint16(wParam >> 16)
		if id == 1001 && event == editChanged && g.codeEdit != 0 && g.activationOpen && !g.checking && !g.busy {
			code := make([]uint16, 32)
			winGetText.Call(g.codeEdit, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)))
			if len(syscall.UTF16ToString(code)) == 10 {
				g.submitActivation()
			}
		} else if id == 1002 && event == editChanged && g.searchEdit != 0 {
			buffer := make([]uint16, 256)
			winGetText.Call(g.searchEdit, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
			g.searchText = syscall.UTF16ToString(buffer)
			g.refreshVisible()
		} else if id == 1003 {
			g.showLists = !g.showLists
			g.fitChildren()
			g.repaint()
		}
		return 0
	}
	return defaultWnd(hwnd, message, wParam, lParam)
}
func overlayWindowProcedure(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	g := mainUI
	if g == nil {
		return defaultWnd(hwnd, message, wParam, lParam)
	}
	switch message {
	case msgPaint:
		var ps winPaint
		hdc, _, _ := winBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			g.drawPlayerOverlay(hdc, true)
		}
		winEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case msgMouseMove:
		if !g.meaningfulMouseMove(lowSigned(lParam), highSigned(lParam)) {
			return 0
		}
		if g.audioOpen {
			g.armAudioHide()
			return 0
		}
		if g.overlayVisible {
			g.armOverlayHide()
		}
		return 0
	case msgLDown:
		g.pointerX, g.pointerY = lowSigned(lParam), highSigned(lParam)
		g.dragStart = time.Now()
		g.pointerRevealOnly = !g.overlayVisible && !g.audioOpen && !g.infoOpen
		if g.pointerRevealOnly {
			g.revealOverlay()
		} else {
			g.armOverlayHide()
		}
		return 0
	case msgLUp:
		x, y := lowSigned(lParam), highSigned(lParam)
		if g.pointerRevealOnly {
			g.pointerRevealOnly = false
			g.armOverlayHide()
			winSetFocus.Call(g.hwnd)
			return 0
		}
		g.click(x, y)
		g.armOverlayHide()
		winSetFocus.Call(g.hwnd)
		return 0
	case msgMouseWheel:
		if g.searchVisible {
			if highSigned(wParam) > 0 {
				g.searchKey(keyUp)
			} else {
				g.searchKey(keyDown)
			}
			return 0
		}
		if g.audioOpen && !g.infoOpen {
			if highSigned(wParam) > 0 {
				g.moveAudio(-1)
			} else {
				g.moveAudio(1)
			}
			return 0
		}
		g.focusColumnAt(lowSigned(lParam))
		delta := highSigned(wParam)
		if delta > 0 {
			g.moveSelection(-1)
		} else {
			g.moveSelection(1)
		}
		g.armOverlayHide()
		return 0
	case msgKeyDown:
		g.handleKeys(wParam)
		return 0
	case msgChar:
		g.handleCharacter(wParam)
		return 0
	}
	return defaultWnd(hwnd, message, wParam, lParam)
}

func videoWindowProcedure(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	g := mainUI
	if g != nil {
		switch message {
		case msgMouseMove:
			if g.active && g.overlayVisible && g.meaningfulMouseMove(lowSigned(lParam), highSigned(lParam)) {
				g.armOverlayHide()
			}
		case msgLUp:
			if g.active {
				g.togglePlayerControls()
				winSetFocus.Call(g.hwnd)
			}
		case msgMouseWheel:
			if g.searchVisible {
				if highSigned(wParam) > 0 {
					g.searchKey(keyUp)
				} else {
					g.searchKey(keyDown)
				}
				return 0
			}
			if g.active && !g.overlayVisible {
				g.togglePlayerControls()
			}

		}
	}
	if videoPreviousProc != 0 {
		out, _, _ := winCallWndProc.Call(videoPreviousProc, hwnd, uintptr(message), wParam, lParam)
		return out
	}
	return defaultWnd(hwnd, message, wParam, lParam)
}

func defaultWnd(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	out, _, _ := winDefaultProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return out
}

// The support link is user-initiated. The player UI itself never opens a browser.
func openTelegramSupport() {
	shell := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	verb := winUTF16("open")
	url := winUTF16("https://t.me/tv2uplayer")
	shell.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(url)), 0, 0, 1)
}

func (g *nativeUI) monitorSession() {
	timer := time.NewTicker(45 * time.Second)
	defer timer.Stop()
	for range timer.C {
		g.taskMu.Lock()
		closed := g.closed
		g.taskMu.Unlock()
		if closed {
			return
		}
		g.app.mu.RLock()
		active := g.app.token != ""
		g.app.mu.RUnlock()
		if active {
			if _, err := g.app.ensure(true); err != nil {
				g.post(uiTask{kind: "revoked", note: userError(err)})
			}
		}
	}
}
func launchNativeUI(a *application) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	winSetDPI.Call()
	g := &nativeUI{app: a, showLists: true, focus: 1, selectedID: -1, vlcCh: make(chan vlcRequest, 8), vlcDone: make(chan struct{}), status: "Activate your subscription"}
	mainUI = g
	defer func() { mainUI = nil }()
	initializeLogo()
	// Match Android's density-scaled TV typography. Native GDI sizes are physical
	// pixels, so fixed 15px text looked much smaller than the Android/WebView UI.
	screenHeightRaw, _, _ := winScreen.Call(1)
	screenHeight := int32(screenHeightRaw)
	mainFontSize := max32(18, min32(48, screenHeight/45))
	smallFontSize := max32(13, min32(30, screenHeight/72))
	boldFontSize := max32(mainFontSize+1, 19)
	menuFontSize := max32(24, min32(58, screenHeight/36))
	screenWidthRaw, _, _ := winScreen.Call(0)
	listSize, metaSize := max32(22, screenHeight/40), max32(15, screenHeight/60)
	if screenWidthRaw <= 680 {
		listSize = 14
		metaSize = 11
	}
	g.fontInfo = makeFont(max32(20, min32(42, screenHeight/38)), fontNormal)
	g.fontListName = makeFont(listSize, fontNormal)
	g.fontListMeta = makeFont(metaSize, fontNormal)
	g.fontAudio = makeFont(min32(30, max32(14, screenHeight*3/100)), fontNormal)
	g.fontNormal = makeFont(mainFontSize, fontNormal)
	g.fontSmall = makeFont(smallFontSize, fontNormal)
	g.fontLarge = makeFont(menuFontSize, fontSemiBold)
	g.fontLogo = makeFont(max32(27, min32(52, screenHeight/34)), fontSemiBold)
	g.fontSplash = makeFont(max32(88, min32(128, screenHeight/10)), 800)
	g.fontClose = makeFont(max32(40, min32(52, screenHeight/22)), fontNormal)
	g.inputBrush, _, _ = winCreateBrush.Call(uintptr(rgb(10, 16, 27)))
	g.fontBold = makeFont(boldFontSize, fontSemiBold)
	defer func() {
		if g.inputBrush != 0 {
			winDelete.Call(g.inputBrush)
		}
		for _, f := range []uintptr{g.fontInfo, g.fontListName, g.fontListMeta, g.fontAudio, g.fontNormal, g.fontSmall, g.fontLarge, g.fontLogo, g.fontBold, g.fontSplash, g.fontClose} {
			if f != 0 {
				winDelete.Call(f)
			}
		}
	}()
	if callbackProc == 0 {
		callbackProc = syscall.NewCallback(windowProcedure)
	}
	module, _, _ := winGetModule.Call(0)
	classname := winUTF16("ActiveCodeNativeWindowsV518")
	cursor, _, _ := winLoadCursor.Call(0, 32512)
	iconLarge, iconSmall := loadApplicationIcons(module)
	class := wndClass{Icon: iconLarge, IconSmall: iconSmall, Size: uint32(unsafe.Sizeof(wndClass{})), WndProc: callbackProc, Instance: module, Cursor: cursor, Name: classname}
	atom, _, err := winRegister.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return fmt.Errorf("Cannot register native window: %v", err)
	}
	width, _, _ := winScreen.Call(0)
	height, _, _ := winScreen.Call(1)
	if width < 400 || height < 320 {
		return errors.New("Screen resolution too small for Windows player")
	}
	g.width = int32(width)
	g.height = int32(height)
	title := winUTF16("active-code · Windows Player v5.1.8")
	hwnd, _, err := winCreate.Call(0, uintptr(unsafe.Pointer(classname)), uintptr(unsafe.Pointer(title)), uintptr(wndPopup|wndVisible|wndClipChildren), 0, 0, width, height, 0, 0, module, 0)
	if hwnd == 0 {
		return fmt.Errorf("Cannot create native app window: %v", err)
	}
	g.hwnd = hwnd
	videoClass := winUTF16("STATIC")
	videoTitle := winUTF16("")
	vHwnd, _, _ := winCreate.Call(0, uintptr(unsafe.Pointer(videoClass)), uintptr(unsafe.Pointer(videoTitle)), uintptr(wndChild|videoBlackRect|videoNotify), 0, 0, 100, 100, hwnd, 1003, module, 0)
	if vHwnd == 0 {
		return errors.New("Cannot create embedded VLC video surface")
	}
	g.videoHwnd = vHwnd
	// Observe activity on the VLC surface while the overlay is hidden.
	if videoCallbackProc == 0 {
		videoCallbackProc = syscall.NewCallback(videoWindowProcedure)
	}
	videoPreviousProc, _, _ = winSetWndProc.Call(g.videoHwnd, ^uintptr(3), videoCallbackProc) // GWLP_WNDPROC (-4)

	// Use a separate owned layered top-level window. If Windows rejects it, the
	// player stays usable through fitChildren's non-layered fallback instead of
	// aborting startup or leaving a blank screen.
	if overlayCallbackProc == 0 {
		overlayCallbackProc = syscall.NewCallback(overlayWindowProcedure)
	}
	overlayClass := winUTF16("ActiveCodeOverlayV518")
	overlayDef := wndClass{Size: uint32(unsafe.Sizeof(wndClass{})), WndProc: overlayCallbackProc, Instance: module, Cursor: cursor, Name: overlayClass}
	overlayAtom, _, overlayRegErr := winRegister.Call(uintptr(unsafe.Pointer(&overlayDef)))
	if overlayAtom != 0 || (overlayRegErr != nil && strings.Contains(strings.ToLower(overlayRegErr.Error()), "already exists")) {
		overlayTitle := winUTF16("")
		overlay, _, _ := winCreate.Call(uintptr(wndExLayered|wndExToolWindow), uintptr(unsafe.Pointer(overlayClass)), uintptr(unsafe.Pointer(overlayTitle)), uintptr(wndPopup), 0, 0, width, height, hwnd, 0, module, 0)
		if overlay != 0 {
			g.overlayHwnd = overlay
			g.overlayVisible = false
			// Black pixels are fully transparent; the remaining UI is uniformly
			// translucent so video remains visible through the Android-style panels.
			ok, _, _ := winLayerAttrs.Call(overlay, uintptr(rgb(0, 0, 0)), uintptr(overlayHiddenAlpha), uintptr(layerColorKey|layerAlpha))
			if ok == 0 {
				user32win.NewProc("DestroyWindow").Call(overlay)
				g.overlayHwnd = 0
			}
		}
	}
	g.createMenuHitWindows(module, cursor)
	// Activation and search text are drawn by active-code itself. This avoids
	// native EDIT controls appearing above/below the VLC/layered overlay on
	// different Windows 10/11 builds, while keyboard input remains supported.
	g.codeEdit = 0
	g.searchEdit = 0
	g.fitChildren()
	winShow.Call(hwnd, showWindow)
	winUpdate.Call(hwnd)
	go g.vlcLoop()
	go g.monitorSession()
	// Android parity: a saved activation reconnects automatically. A new install
	// still shows the welcome/activation screen.
	g.app.mu.RLock()
	hasSavedActivation := g.app.token != ""
	g.app.mu.RUnlock()
	if hasSavedActivation {
		g.beginLoad("")
	}
	winSetFocus.Call(g.hwnd)
	var msg winMessageStruct
	for {
		result, _, _ := winMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		if msg.Message == msgKeyUp && g.releaseVodSeek(msg.WParam) {
			continue
		}
		if msg.Message == msgKeyDown && g.active && !g.activationOpen && isPlayerNavigationKey(msg.WParam) {
			g.handleKeys(msg.WParam)
			continue
		}
		winTranslate.Call(uintptr(unsafe.Pointer(&msg)))
		winDispatch.Call(uintptr(unsafe.Pointer(&msg)))
	}
	for {
		select {
		case g.vlcCh <- vlcRequest{quit: true}:
			goto waitForVLC
		default:
			select {
			case <-g.vlcCh:
			default:
			}
		}
	}
waitForVLC:
	select {
	case <-g.vlcDone:
	case <-time.After(5 * time.Second):
	}
	return nil
}
