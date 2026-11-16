package main

import "testing"

func TestCategoryTotalsCountEntireCatalogueWithoutDoubleCounting(t *testing.T) {
	channels := []channel{{Type: "Live TV", Group: "News"}, {Type: "Live TV", Group: "News"}, {Type: "Live TV", Group: "Live TV"}, {Type: "Movies", Group: "Cinema"}, {Type: "Series", Group: "Episodes"}}
	c := catalogueCounts(channels)
	if c["Live TV"] != 3 || c["Movies"] != 1 || c["Series"] != 1 || c["News"] != 2 || c["Cinema"] != 1 {
		t.Fatalf("wrong category totals: %v", c)
	}
	if z := catalogueCounts(nil); len(z) != 3 || z["Movies"] != 0 {
		t.Fatal("empty top-level categories must retain zero totals")
	}
}
func TestAndroidDeliveryClassificationAndExplicitHints(t *testing.T) {
	cases := []struct{ url, name, hint, want string }{
		{"http://example.test/play/token", "Movies channel", "", "Live TV"},
		{"http://example.test/live/u/p/55.ts", "Star Movies HD", "", "Live TV"},
		{"http://example.test/live/u/p/1.ts", "Movie Premiere", "movie", "Live TV"},
		{"http://example.test/movie/u/p/1.mkv", "Film", "", "Movies"},
		{"http://example.test/series/u/p/1", "Episode", "", "Series"},
		{"http://example.test/play/token", "Film", "vod", "Movies"},
		{"http://example.test/video.mkv", "Drama S01E02", "", "Series"},
	}
	for _, x := range cases {
		if got := classifyMedia(x.url, x.name, x.hint); got != x.want {
			t.Errorf("%s: %s", x.name, got)
		}
	}
	raw := "#EXTM3U\n#EXTINF:-1 group-title=\"Library\" media-type=\"series\",Episode\nhttp://example.test/play/opaque\n"
	if got := parseM3U(raw); len(got) != 1 || got[0].Type != "Series" {
		t.Fatal("explicit M3U delivery hint was ignored")
	}
}

func TestXtreamArchivesKeepSameNamedCategoriesSeparate(t *testing.T) {
	channels := []channel{{Type: "Live TV", Group: "Shared"}, {Type: "Movies", Group: "Shared"}, {Type: "Movies", Group: "Shared"}, {Type: "Series", Group: "Drama"}}
	live, lc := archiveGroups(channels, "Live TV")
	movie, mc := archiveGroups(channels, "Movies")
	if len(live) != 4 || live[3] != "Shared" || lc["Shared"] != 1 || mc["Shared"] != 2 || len(movie) != 4 {
		t.Fatal("archive categories were mixed across media types")
	}
	if lc["Live TV"] != 1 || lc["Movies"] != 2 || lc["Series"] != 1 {
		t.Fatal("top-level archive totals were lost")
	}
}
