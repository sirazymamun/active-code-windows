//go:build windows

package main

// Confirm the highlighted row. Up/Down never commits a content type.
func (g *nativeUI) enterFocusedGroup() {
	if g.groupIndex < 0 || g.groupIndex >= len(g.groups) {
		return
	}
	isType := topLevelGroup(g.groups[g.groupIndex]) || g.groups[g.groupIndex] == backToSeries
	g.commitArchiveGroup()
	if isType {
		g.focus = 0
	} else {
		g.focus = 1
	}
	g.refreshVisible()
	g.fitChildren()
	g.repaint()
}
