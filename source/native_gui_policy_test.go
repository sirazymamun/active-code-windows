package main

import (
	"os"
	"strings"
	"testing"
)

// These tests cover packaging regressions and cannot replace a Windows UI run.
// A user-clicked Telegram support URL may use Windows ShellExecute; the player UI may not.
func TestNoBrowserLauncher(t *testing.T) {
	for _, f := range []string{"launch_windows.go", "launch_args.go", "web/index.html"} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("Legacy browser launcher/UI still shipped: %s", f)
		}
	}
	main, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), "launchNativeUI(app)") {
		t.Fatal("Native UI not used at startup")
	}
	if strings.Contains(string(main), "launchWindow(url)") {
		t.Fatal("Legacy browser launch used")
	}
}
func TestMenuAndEmbeddedVideoNative(t *testing.T) {
	code, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(code)
	for _, token := range []string{"Search", "Playlist", "Record", "Reload", "Manage", "Info", "videoHwnd", "vlc.render(g.videoHwnd)", "wndPopup|wndVisible", "g.width", "g.height"} {
		if !strings.Contains(s, token) {
			t.Errorf("Missing Windows native UI feature %q", token)
		}
	}
	for _, token := range []string{"msedge", "--kiosk", "WebView2"} {
		if strings.Contains(s, token) {
			t.Errorf("Browser dependency %q", token)
		}
	}
}

// Regression guard for the startup failure reported on Windows 10:
// "Cannot create native edit controls".
func TestNativeEditControlStartupRegression(t *testing.T) {
	body, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	for _, token := range []string{
		"winExClientEdge", "0x00000200", // extended style not WS_BORDER
		"editPassword", "0x0020", // ES_PASSWORD
		"editAutoHScroll", "0x0080", // ES_AUTOHSCROLL
		"g.codeEdit = 0", "g.searchEdit = 0", "g.codeBuffer", "msgChar:",
		"g.handleCharacter(wParam)",
	} {
		if !strings.Contains(src, token) {
			t.Errorf("Missing EDIT control startup fix: %s", token)
		}
	}
	if strings.Contains(src, `return errors.New("Cannot create native edit controls")`) {
		t.Fatal("EDIT creation failure must use keyboard fallback, not abort the player")
	}
	if strings.Contains(src, `winCreate.Call(uintptr(wndBorder)`) {
		t.Fatal("WS_BORDER must not be an extended window style")
	}
}

func TestAndroidStyleActivationNavigation(t *testing.T) {
	b, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, token := range []string{"welcomeButton()", "popupCloseRect()", "activationOpen", "openActivation()", "closeActivation()", "10 Digit Activation Code", "Enter activation code", "t.me/tv2uplayer", "keyEsc", "keyEnter"} {
		if !strings.Contains(s, token) {
			t.Errorf("Missing Android-style startup behavior %q", token)
		}
	}
}

func TestSavedSessionReconnectsAutomatically(t *testing.T) {
	b, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, token := range []string{"hasSavedActivation", "if hasSavedActivation", `g.beginLoad("")`, `g.playChannelIndex(0)`} {
		if !strings.Contains(src, token) {
			t.Errorf("Missing Android-style saved-session startup behavior %q", token)
		}
	}
}

func TestHiddenOverlayRemainsMouseReachable(t *testing.T) {
	b, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, token := range []string{
		"overlayHiddenAlpha  = 1",
		"rgb(1, 1, 1)",
		"Never physically hide the layered window while playback is active",
		"case msgMouseMove:",
		"g.revealOverlay()",
	} {
		if !strings.Contains(src, token) {
			t.Errorf("Missing hidden-overlay input recovery guard %q", token)
		}
	}
}

func TestFullscreenOverlayHasNoPermanentChrome(t *testing.T) {
	b, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, forbidden := range []string{
		`label(hdc, "WINDOWS PLAYER · v5.1.8"`,
		`label(hdc, note, rectF{8, h - 27`,
		`fill(hdc, rectF{0, h - 29, w, 29}`,
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("permanent player chrome returned: %s", forbidden)
		}
	}
	for _, required := range []string{
		`winMove.Call(g.videoHwnd, 0, 0`,
		`drawCornerLogo(hdc, rectF{`,
		`titles := []string{"⌕", "☷", "◎", "↻", "⚿", "ⓘ"}`,
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("missing fullscreen overlay behavior: %s", required)
		}
	}
}

func TestSearchMatchesEntirePlaylistAndPlaysActualResult(t *testing.T) {
	b, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, required := range []string{
		`if g.searchVisible {`,
		`haystack := strings.ToLower(ch.Name + " " + ch.Group + " " + ch.Type)`,
		`actual := g.visible[g.chanIndex]`,
		`g.playChannelIndex(actual)`,
	} {
		if !strings.Contains(s, required) {
			t.Errorf("missing Android-style global search behavior: %s", required)
		}
	}
}

func TestEscOverlayThenExitAndNoMouseResize(t *testing.T) {
	b, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, required := range []string{
		`if g.dismissPlayerOverlay() {`,
		`if !g.showLists && !g.overlayVisible {`,
		`g.hideOverlay()`,
		`DestroyWindow`,
		`Do not resize/reposition either the VLC surface or the layered window here`,
	} {
		if !strings.Contains(s, required) {
			t.Errorf("missing Esc/flicker regression guard: %s", required)
		}
	}
}

func TestListPanelsHaveNoTitleHeaders(t *testing.T) {
	b, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, forbidden := range []string{
		`label(hdc, "Group / Category"`,
		`channelTitle := "Channel List"`,
		`label(hdc, "Channel List"`,
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("list title header returned: %s", forbidden)
		}
	}
	// Search is a separate panel: list geometry cannot reserve an inline row.
	if strings.Contains(s, "return top + 48") {
		t.Fatal("inline channel search row returned")
	}
	for _, method := range []string{"groupListStart", "channelListStart"} {
		marker := "func (g *nativeUI) " + method + "(top int32) int32"
		start := strings.Index(s, marker)
		if start < 0 || !strings.Contains(s[start:start+len(marker)+28], "return top + 4") {
			t.Fatalf("unexpected list start for %s", method)
		}
	}

}

func TestInfoMenuRegressionUsesNamedIndex(t *testing.T) {
	testBody, err := os.ReadFile("overlay_behavior_windows_test.go")
	if err != nil {
		t.Fatal(err)
	}
	testSrc := string(testBody)
	if !strings.Contains(testSrc, "g.action(menuInfo)") {
		t.Fatal("Windows Info regression test must use menuInfo, not a numeric index")
	}
	if strings.Contains(testSrc, "g.action(4)") {
		t.Fatal("stale numeric Info menu index 4 remains after Manage insertion")
	}
	uiBody, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	uiSrc := string(uiBody)
	for _, token := range []string{"case menuManage:", "case menuInfo:"} {
		if !strings.Contains(uiSrc, token) {
			t.Fatalf("native menu action switch missing %s", token)
		}
	}
}
