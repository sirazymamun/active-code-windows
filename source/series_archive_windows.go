//go:build windows

package main

import "sync/atomic"

const backToSeries = "← Back to Series library"

func (g *nativeUI) openSeriesArchive(parent channel) {
	g.stopStartupLoading(false)
	if g.busy {
		return
	}
	id := atomic.AddUint64(&g.playRequest, 1)
	g.busy = true
	g.seriesLoading = true
	g.status = "Loading series episodes…"
	g.repaint()
	go func() {
		episodes, err := loadSeriesEpisodes(parent)
		task := uiTask{kind: "series", requestID: id, channels: episodes, success: err == nil}
		if err != nil {
			task.note = userError(err)
		}
		g.post(task)
	}()
}
func (g *nativeUI) restoreSeriesLibrary(section string) {
	if !g.seriesPage {
		return
	}
	g.channels = g.libraryChannels
	g.libraryChannels = nil
	g.seriesPage = false
	g.archiveSection = section
	g.groups, g.groupCounts = archiveGroups(g.channels, section)
	g.groupIndex = 0
	if section == "Movies" {
		g.groupIndex = 1
	} else if section == "Series" {
		g.groupIndex = 2
	}
	g.groupScroll = 0
	g.chanScroll = 0
	g.chanIndex = 0
	g.selectedID = -1
	g.showLists = true
	g.overlayVisible = true
	g.refreshVisible()
	g.fitChildren()
	g.repaint()
}
