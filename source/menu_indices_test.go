package main

import "testing"

func TestNativeMenuIndexesStayAlignedWithManageAndInfo(t *testing.T) {
	if menuSearch != 0 || menuPlaylist != 1 || menuRecord != 2 || menuReload != 3 || menuManage != 4 || menuInfo != 5 || menuCount != 6 {
		t.Fatalf("unexpected native menu order: search=%d playlist=%d record=%d reload=%d manage=%d info=%d count=%d", menuSearch, menuPlaylist, menuRecord, menuReload, menuManage, menuInfo, menuCount)
	}
}
