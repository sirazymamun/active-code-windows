package main

// Keep the native top-menu order in one place so Windows-only UI tests and
// the click/remote handler cannot drift when a new menu item is added.
const (
	menuSearch = iota
	menuPlaylist
	menuRecord
	menuReload
	menuManage
	menuInfo
	menuCount
)
