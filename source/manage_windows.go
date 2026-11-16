//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The 4-digit PIN is a convenience privacy control, not encryption of media.
// All group settings are local to this Windows account and playlist.
type manageSettings struct {
	PINHash   string              `json:"pin_hash"`
	Playlists map[string][]string `json:"playlists"`
}

func pinDigest(pin string) string {
	d := sha256.Sum256([]byte("active-code-manage-v1:" + pin))
	return hex.EncodeToString(d[:])
}
func manageConfigPath() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "active-code", "manage.json")
}
func readManageSettings() manageSettings {
	m := manageSettings{PINHash: pinDigest("1234"), Playlists: make(map[string][]string)}
	path := manageConfigPath()
	if path == "" {
		return m
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	var parsed manageSettings
	if json.Unmarshal(b, &parsed) != nil || parsed.PINHash == "" || parsed.Playlists == nil {
		return m
	}
	return parsed
}
func writeManageSettings(m manageSettings) error {
	path := manageConfigPath()
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err = os.WriteFile(temp, raw, 0600); err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}
func (g *nativeUI) managePlaylistKey() string {
	if g.app == nil {
		return ""
	}
	raw := strings.TrimSpace(g.app.lease.PlaylistURL)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
func (g *nativeUI) protectedGroupKeys() map[string]bool {
	key := g.managePlaylistKey()
	if g.manageCache != nil && g.manageCacheKey == key {
		return g.manageCache
	}
	m := readManageSettings()
	out := make(map[string]bool)
	for _, group := range m.Playlists[key] {
		out[group] = true
	}
	g.manageCache = out
	g.manageCacheKey = key
	return out
}
func (g *nativeUI) isManagedChannel(ch channel) bool {
	choices := g.protectedGroupKeys()
	return choices[managedCategoryKey(ch)] || choices["g:"+ch.Group] || choices["g:"+ch.LockGroup] || choices["k:"+ch.Type]
}

// Feed archive totals and category rows from actually accessible channels.
// It also hides a provider category if its entire media type was protected.
func (g *nativeUI) availableManagedChannels() []channel {
	allowed := make([]channel, 0, len(g.channels))
	protected := g.protectedGroupKeys()
	for _, ch := range g.channels {
		if !protected[managedCategoryKey(ch)] && !protected["g:"+ch.Group] && !protected["k:"+ch.Type] {
			allowed = append(allowed, ch)
		}
	}
	return allowed
}
func (g *nativeUI) filterManagedGroups() {
	choices := g.protectedGroupKeys()
	filtered := g.groups[:0]
	for _, gr := range g.groups {
		key := "g:" + g.archiveSection + ":" + gr
		if topLevelGroup(gr) {
			key = "k:" + gr
		}
		if !choices[key] && !choices["g:"+gr] {
			filtered = append(filtered, gr)
		}
	}
	g.groups = filtered
}
func (g *nativeUI) openManage() {
	g.searchVisible = false
	g.audioOpen = false
	g.infoOpen = false
	g.manageMode = 1
	g.manageDigits = ""
	g.manageNotice = ""
	g.manageScroll = 0
	g.manageFocus = 0
	g.showLists = false
	g.overlayVisible = true
	g.overlayDeadline = time.Time{}
	g.syncMenuHitWindows()
	g.fitChildren()
	if g.overlayHwnd != 0 {
		winLayerAttrs.Call(g.overlayHwnd, uintptr(rgb(0, 0, 0)), uintptr(120), uintptr(layerColorKey|layerAlpha))
	}
	g.repaint()
}
func (g *nativeUI) closeManage() {
	g.manageMode = 0
	if g.overlayHwnd != 0 {
		winLayerAttrs.Call(g.overlayHwnd, uintptr(rgb(0, 0, 0)), uintptr(overlayVisibleAlpha), uintptr(layerColorKey|layerAlpha))
	}
	g.manageDigits = ""
	g.manageOldPin = ""
	g.syncMenuHitWindows()
	g.armOverlayHide()
	g.repaint()
}
func (g *nativeUI) authorizeManage() {
	if pinDigest(g.manageDigits) != readManageSettings().PINHash {
		g.manageNotice = "Incorrect PIN"
		g.manageDigits = ""
		g.repaint()
		return
	}
	g.manageMode = 2
	g.manageFocus = 0
	g.manageDigits = ""
	g.manageNotice = ""
	selected := g.protectedGroupKeys()
	g.manageSelected = make(map[string]bool, len(selected))
	for key, value := range selected {
		g.manageSelected[key] = value
	}
	unique := make(map[string]bool)
	all := g.channels
	if len(g.libraryChannels) > len(all) {
		all = g.libraryChannels
	}
	g.manageGroups = g.manageGroups[:0]
	// Archive order: content type first, provider category encounter order within it.
	for _, kind := range []string{"Live TV", "Movies", "Series", ""} {
		for _, ch := range all {
			if (kind != "" && ch.Type != kind) || (kind == "" && topLevelGroup(ch.Type)) {
				continue
			}
			if strings.TrimSpace(ch.Group) == "" || topLevelGroup(ch.Group) {
				continue
			}
			key := managedCategoryKey(ch)
			if !unique[key] {
				unique[key] = true
				g.manageGroups = append(g.manageGroups, key)
			}
		}
	}
	// Convert legacy whole-type selections into explicit category selections.
	for _, ch := range all {
		if selected["k:"+ch.Type] || selected["g:"+ch.Group] {
			g.manageSelected[managedCategoryKey(ch)] = true
		}
	}
	g.manageScroll = 0
	g.repaint()
}
func (g *nativeUI) saveManage() {
	m := readManageSettings()
	var selected []string
	for _, k := range g.manageGroups {
		if g.manageSelected[k] {
			selected = append(selected, k)
		}
	}
	m.Playlists[g.managePlaylistKey()] = selected
	if err := writeManageSettings(m); err != nil {
		g.manageNotice = "Unable to save settings"
		g.repaint()
		return
	}
	g.manageCache = nil
	g.closeManage()
	g.updateGroups()
	if g.isManagedChannel(g.playingItem) {
		// Protected media must no longer continue playing after Save.
		g.status = "Locked group hidden"
		g.playingItem = channel{}
		g.selectedID = -1
		g.stopManagedPlayback()
	}
	g.showBottomStatus("Group Lock + Hide saved", 3*time.Second)
	g.repaint()
}
func (g *nativeUI) stopManagedPlayback() {
	// Stop via the same player lifecycle as a normal channel change.
	// The actual native stop request is handled by the VLC worker.
	g.recordingPending = false
	g.stopVLC()
}
func (g *nativeUI) changeManagePIN() {
	if g.manageMode == 3 {
		if pinDigest(g.manageDigits) != readManageSettings().PINHash {
			g.manageNotice = "Incorrect current PIN"
			g.manageDigits = ""
			g.repaint()
			return
		}
		g.manageOldPin = g.manageDigits
		g.manageDigits = ""
		g.manageMode = 4
		g.manageNotice = ""
		g.repaint()
		return
	}
	if g.manageMode == 4 {
		if len(g.manageDigits) != 4 {
			g.manageNotice = "PIN must be 4 digits"
			g.repaint()
			return
		}
		m := readManageSettings()
		m.PINHash = pinDigest(g.manageDigits)
		if writeManageSettings(m) != nil {
			g.manageNotice = "Unable to save PIN"
			g.repaint()
			return
		}
		g.manageMode = 2
		g.manageDigits = ""
		g.manageOldPin = ""
		g.manageNotice = "PIN changed successfully"
		winSetTimer.Call(g.hwnd, 91, 4000, 0)
		g.repaint()
	}
}
func (g *nativeUI) manageRect() rectF {
	w, h := managePopupSize(g.width, g.height, g.manageMode == 2)
	return rectF{(g.width - w) / 2, (g.height - h) / 2, w, h}
}
func (g *nativeUI) manageRowCount() int {
	r := g.manageRect()
	return maxInt(1, int((r.h-140)/30))
}
func drawManageBorder(dc uintptr, r rectF, colour uint32) {
	fill(dc, rectF{r.x, r.y, r.w, 1}, colour)
	fill(dc, rectF{r.x, r.y + r.h - 1, r.w, 1}, colour)
	fill(dc, rectF{r.x, r.y, 1, r.h}, colour)
	fill(dc, rectF{r.x + r.w - 1, r.y, 1, r.h}, colour)
}
func (g *nativeUI) drawManage(dc uintptr) {
	white := rgb(228, 240, 243)
	cyan := rgb(82, 203, 229)
	sub := rgb(174, 190, 198)
	w, h := g.width, g.height
	// Black is the layered-window colour key: video remains visible around the popup.
	fill(dc, rectF{0, 0, w, h}, rgb(0, 0, 0))
	box := g.manageRect()
	// Square, translucent panel like the activation popup; opacity is applied by openManage().
	fill(dc, box, rgb(8, 12, 20))
	centerLabel(dc, "Manage Groups", rectF{box.x + 48, box.y + 4, box.w - 96, 34}, g.fontLarge, white)
	closeRect := rectF{box.x + box.w - 54, box.y - 2, 54, 44}
	if g.manageFocus == -1 {
		fill(dc, closeRect, rgb(39, 71, 84))
	}
	centerLabel(dc, "×", closeRect, g.fontClose, white)

	if g.manageMode == 1 || g.manageMode == 3 || g.manageMode == 4 {
		heading := "Enter 4-digit PIN"
		if g.manageMode == 3 {
			heading = "Enter current PIN"
		}
		if g.manageMode == 4 {
			heading = "Enter new 4-digit PIN"
		}
		centerLabel(dc, heading, rectF{box.x + 10, box.y + 42, box.w - 20, 25}, g.fontNormal, white)

		input := rectF{box.x + 8, box.y + 70, box.w - 16, 50}
		fill(dc, input, rgb(10, 16, 27))
		border := rgb(104, 115, 132)
		if g.manageFocus == 0 {
			border = rgb(204, 218, 226)
		}
		drawManageBorder(dc, input, border)
		centerLabel(dc, strings.Repeat("●", len(g.manageDigits))+strings.Repeat("_", 4-len(g.manageDigits)), rectF{input.x + 12, input.y, input.w - 24, input.h}, g.fontLarge, rgb(164, 173, 188))

		buttonY := box.y + 154
		if g.manageNotice != "" {
			centerLabel(dc, g.manageNotice, rectF{box.x + 12, box.y + 123, box.w - 24, 23}, g.fontSmall, cyan)
		} else if g.manageMode == 1 && readManageSettings().PINHash == pinDigest("1234") {
			centerLabel(dc, "Default PIN: 1234", rectF{box.x + 12, box.y + 123, box.w - 24, 23}, g.fontSmall, sub)
		}

		if g.manageMode == 1 {
			confirm := rectF{box.x + box.w/2 - 145, buttonY, 135, 34}
			change := rectF{box.x + box.w/2 + 10, buttonY, 135, 34}
			if g.manageFocus == 1 {
				fill(dc, confirm, rgb(39, 71, 84))
			}
			if g.manageFocus == 2 {
				fill(dc, change, rgb(39, 71, 84))
			}
			centerLabel(dc, "Confirm", confirm, g.fontNormal, white)
			centerLabel(dc, "Change PIN", change, g.fontNormal, white)
		} else {
			confirm := rectF{box.x + box.w/2 - 70, buttonY, 140, 34}
			if g.manageFocus == 1 {
				fill(dc, confirm, rgb(39, 71, 84))
			}
			centerLabel(dc, "Confirm", confirm, g.fontNormal, white)
		}
		return
	}

	// Group list: same visual language, compact rows; scrollbar is indicator-only.
	centerLabel(dc, "✓ Lock + Hide   |   Untick to show", rectF{box.x + 8, box.y + 39, box.w - 16, 24}, g.fontSmall, sub)
	count := g.manageRowCount()
	total := len(g.manageGroups)
	rowTop := box.y + 65
	rowH := int32(30)
	if total > count {
		height := int32(count) * rowH
		track := rectF{box.x + box.w - 10, rowTop, 3, height}
		fill(dc, track, rgb(72, 94, 108))
		thumbHeight := max32(14, height*int32(count)/int32(total))
		travel := height - thumbHeight
		maxScroll := total - count
		pos := int32(g.manageScroll) * travel / int32(maxScroll)
		fill(dc, rectF{track.x, rowTop + pos, 3, thumbHeight}, rgb(180, 221, 242))
	}
	for j := 0; j < count && j+g.manageScroll < total; j++ {
		index := j + g.manageScroll
		name := g.manageGroups[index]
		display := strings.TrimPrefix(strings.TrimPrefix(name, "g:"), "k:")
		for _, kind := range []string{"Live TV:", "Movies:", "Series:"} {
			display = strings.TrimPrefix(display, kind)
		}
		y := rowTop + int32(j)*rowH
		if g.manageFocus == index {
			fill(dc, rectF{box.x + 7, y, box.w - 26, rowH - 1}, rgb(28, 73, 96))
		}
		if g.manageSelected[name] {
			label(dc, "☑", rectF{box.x + 13, y, 32, rowH}, g.fontNormal, cyan)
		} else {
			label(dc, "☐", rectF{box.x + 13, y, 32, rowH}, g.fontNormal, white)
		}
		label(dc, display, rectF{box.x + 48, y, box.w - 70, rowH}, g.fontSmall, white)
	}
	footerY := box.y + box.h - 37
	change := rectF{box.x + box.w/2 - 145, footerY, 135, 32}
	save := rectF{box.x + box.w/2 + 10, footerY, 135, 32}
	if g.manageFocus == len(g.manageGroups) {
		fill(dc, change, rgb(39, 71, 84))
	}
	if g.manageFocus == len(g.manageGroups)+1 {
		fill(dc, save, rgb(39, 71, 84))
	}
	centerLabel(dc, "Change PIN", change, g.fontNormal, white)
	centerLabel(dc, "Save", save, g.fontNormal, cyan)
	if g.manageNotice != "" {
		centerLabel(dc, g.manageNotice, rectF{box.x + 8, box.y + box.h - 64, box.w - 16, 22}, g.fontSmall, cyan)
	}
}
func (g *nativeUI) clickManage(x, y int32) {
	box := g.manageRect()
	closeRect := rectF{box.x + box.w - 54, box.y - 2, 54, 44}
	if closeRect.inside(x, y) {
		g.closeManage()
		return
	}
	if !box.inside(x, y) {
		return
	}

	if g.manageMode == 1 || g.manageMode == 3 || g.manageMode == 4 {
		input := rectF{box.x + 8, box.y + 70, box.w - 16, 50}
		if input.inside(x, y) {
			g.manageFocus = 0
			g.repaint()
			return
		}
		buttonY := box.y + 154
		if g.manageMode == 1 {
			confirm := rectF{box.x + box.w/2 - 145, buttonY, 135, 34}
			change := rectF{box.x + box.w/2 + 10, buttonY, 135, 34}
			if confirm.inside(x, y) {
				g.manageFocus = 1
				g.manageKey(keyEnter)
				return
			}
			if change.inside(x, y) {
				g.manageMode = 3
				g.manageFocus = 0
				g.manageDigits = ""
				g.manageNotice = ""
				g.repaint()
				return
			}
		} else {
			confirm := rectF{box.x + box.w/2 - 70, buttonY, 140, 34}
			if confirm.inside(x, y) {
				g.manageFocus = 1
				g.manageKey(keyEnter)
				return
			}
		}
		return
	}

	footerY := box.y + box.h - 42
	if y >= footerY {
		if x < box.x+box.w/2 {
			g.manageMode = 3
			g.manageFocus = 0
			g.manageDigits = ""
			g.manageNotice = ""
			g.repaint()
		} else {
			g.saveManage()
		}
		return
	}
	rowTop := box.y + 65
	rowH := int32(30)
	if y >= rowTop && y < rowTop+int32(g.manageRowCount())*rowH {
		index := g.manageScroll + int((y-rowTop)/rowH)
		if index >= 0 && index < len(g.manageGroups) {
			k := g.manageGroups[index]
			g.manageSelected[k] = !g.manageSelected[k]
			g.manageFocus = index
			g.manageNotice = ""
			g.repaint()
		}
	}
}
func (g *nativeUI) manageKey(key uintptr) {
	if key == keyEsc {
		if g.manageMode >= 3 {
			g.manageMode = 2
			g.manageDigits = ""
			g.manageFocus = len(g.manageGroups)
		} else {
			g.closeManage()
		}
		g.repaint()
		return
	}
	if key == keyUp || key == keyDown || key == keyLeft || key == keyRight {
		last := 1
		if g.manageMode == 1 {
			last = 2
		}
		if g.manageMode == 2 {
			last = len(g.manageGroups) + 1
		}
		if key == keyDown || key == keyRight {
			g.manageFocus++
		} else {
			g.manageFocus--
		}
		if g.manageFocus > last {
			g.manageFocus = -1
		}
		if g.manageFocus < -1 {
			g.manageFocus = last
		}
		if g.manageMode == 2 && g.manageFocus >= 0 && g.manageFocus < len(g.manageGroups) {
			if g.manageFocus < g.manageScroll {
				g.manageScroll = g.manageFocus
			}
			if g.manageFocus >= g.manageScroll+g.manageRowCount() {
				g.manageScroll = g.manageFocus - g.manageRowCount() + 1
			}
		}
		g.repaint()
		return
	}
	if key == keyBackspace {
		if g.manageMode != 2 && len(g.manageDigits) > 0 {
			g.manageDigits = g.manageDigits[:len(g.manageDigits)-1]
		}
		g.repaint()
		return
	}
	if key != keyEnter {
		return
	}
	if g.manageFocus == -1 {
		g.closeManage()
		return
	}
	if g.manageMode == 2 {
		count := len(g.manageGroups)
		if g.manageFocus >= 0 && g.manageFocus < count {
			k := g.manageGroups[g.manageFocus]
			g.manageSelected[k] = !g.manageSelected[k]
		} else if g.manageFocus == count {
			g.manageMode = 3
			g.manageFocus = 0
			g.manageDigits = ""
			g.manageNotice = ""
		} else if g.manageFocus == count+1 {
			g.saveManage()
			return
		}
		g.repaint()
		return
	}
	if g.manageMode == 1 && g.manageFocus == 2 {
		g.manageMode = 3
		g.manageFocus = 0
		g.manageDigits = ""
		g.repaint()
		return
	}
	if len(g.manageDigits) != 4 {
		g.manageNotice = "Enter exactly four digits"
		g.repaint()
		return
	}
	if g.manageMode == 1 {
		g.authorizeManage()
	} else {
		g.changeManagePIN()
	}
}
func (g *nativeUI) manageCharacter(code uintptr) {
	if g.manageMode != 1 && g.manageMode != 3 && g.manageMode != 4 {
		return
	}
	if code >= '0' && code <= '9' && len(g.manageDigits) < 4 {
		g.manageDigits += string(rune(code))
		g.manageNotice = ""
		g.repaint()
	}
}
