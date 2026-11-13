//go:build windows

package main

import "testing"

func TestTypesCanBeTraversedWithoutChangingLiveCategories(t *testing.T) {
	g := &nativeUI{width: 1280, height: 720, focus: 0, archiveSection: "Live TV", groups: []string{"Live TV", "Movies", "Series", "PAK | ISLAMIC", "PAK | NEWS HD"}, channels: []channel{{Name: "Live", Group: "PAK | ISLAMIC", Type: "Live TV"}}, visible: []int{0}}
	g.moveSelection(1)
	g.moveSelection(1)
	if g.groupIndex != 2 || g.archiveSection != "Live TV" || len(g.visible) != 1 || g.groups[3] != "PAK | ISLAMIC" {
		t.Fatal("type focus changed selected archive")
	}
	g.moveSelection(1)
	if g.groupIndex != 3 || len(g.visible) != 1 {
		t.Fatal("Live category is unreachable")
	}
}
func TestConfirmTypeKeepsCategoryPaneReachable(t *testing.T) {
	g := &nativeUI{width: 1280, height: 720, focus: 0, groupIndex: 1, archiveSection: "Live TV", groups: []string{"Live TV", "Movies", "Series", "News"}, channels: []channel{{Name: "Live", Group: "News", Type: "Live TV"}, {Name: "Film", Group: "Cinema", Type: "Movies"}}}
	g.enterFocusedGroup()
	if g.archiveSection != "Movies" || g.focus != 0 {
		t.Fatal("confirmed type must keep focus in category pane")
	}
	if len(g.groups) != 4 || g.groups[3] != "Cinema" {
		t.Fatal("confirmed type did not update categories")
	}
	g.moveSelection(1)
	g.moveSelection(1)
	if g.groupIndex != 3 || len(g.visible) != 1 {
		t.Fatal("movie category navigation failed")
	}
	g.enterFocusedGroup()
	if g.focus != 1 {
		t.Fatal("confirmed category did not enter Channels")
	}
}
