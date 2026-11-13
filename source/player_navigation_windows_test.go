//go:build windows

package main

import "testing"

func TestChannelRightEntersMenuAndEnterExecutesHighlightedItem(t *testing.T) {
	g := &nativeUI{active: true, showLists: true, overlayVisible: true, focus: 1, chanIndex: 3}
	g.handleKeys(keyRight)
	if g.focus != 2 || g.menuIndex != 0 || g.chanIndex != 3 {
		t.Fatal("channel Right failed to enter menu")
	}
	for i := 1; i < 6; i++ {
		g.handleKeys(keyRight)
		if g.menuIndex != i {
			t.Fatal("menu cursor skipped icon")
		}
	}
	g.handleKeys(keyEnter)
	if !g.infoOpen {
		t.Fatal("Enter did not open focused Info")
	}
	g.handleKeys(keyEsc)
	for i := 5; i > 0; i-- {
		g.handleKeys(keyLeft)
		if g.menuIndex != i-1 {
			t.Fatal("menu Left skipped icon")
		}
	}
	g.handleKeys(keyLeft)
	if g.focus != 1 || g.chanIndex != 3 {
		t.Fatal("menu Left lost channel cursor")
	}
	g.handleKeys(keyLeft)
	if g.focus != 0 {
		t.Fatal("channel Left did not enter groups")
	}
}

func TestRepeatedCategoryMovementVisitsEachRowAndKeepsCursorVisible(t *testing.T) {
	g := &nativeUI{active: true, showLists: true, focus: 0, width: 1280, height: 720,
		groups: []string{"Live TV", "Movies", "Series", "News", "Sports", "Drama", "Music"}, archiveSection: "Live TV"}
	for step := 1; step <= 100; step++ {
		g.handleKeys(keyDown)
		if g.focus != 0 || g.groupIndex != step%len(g.groups) {
			t.Fatalf("focus jumped at %d to %d", step, g.groupIndex)
		}
	}
	if g.archiveSection != "Live TV" {
		t.Fatal("navigation selected a content type")
	}
}
