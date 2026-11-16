package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaylistRetriesPlayerIdentityAfterHTMLResponse(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("User-Agent") == playlistUserAgents[0] {
			w.Write([]byte("<html>Player identity refused</html>"))
			return
		}
		w.Write([]byte("#EXTM3U\n#EXTINF:-1,Channel\nhttp://example.test/live/1.ts\n"))
	}))
	defer srv.Close()
	body, _, _, err := downloadPlaylist(srv.URL)
	if err != nil || !strings.HasPrefix(string(body), "#EXTM3U") || requests != 2 {
		t.Fatal("HTML reply was accepted instead of trying the next player identity")
	}
}

func TestMovieGenreNameDoesNotMisclassifyLiveDelivery(t *testing.T) {
	if got := classify("TAMIL MOVIE HINDI", "https://example.test/play/token"); got != "Live TV" {
		t.Fatalf("live delivery with MOVIE genre classified as %q", got)
	}
}

func TestXUISessionMetadataStillImportsChannels(t *testing.T) {
	raw := "#EXTM3U\n#EXT-X-SESSION-DATA:DATA-ID=\"com.xui.1_5_13\"\n#EXTINF:-1 group-title=\"News\",Channel\nhttp://example.test/play/token\n"
	if got := parseM3UWithBase(raw, "http://example.test/playlist/u/p/m3u_plus"); len(got) != 1 || got[0].Name != "Channel" {
		t.Fatal("XUI catalogue session metadata must not be confused with an HLS stream")
	}
}

func TestHLSManifestWithBaseIsNotAChannelCatalogue(t *testing.T) {
	for _, manifest := range []string{
		"#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nsegment1.ts\n#EXTINF:6,\nsegment2.ts\n",
		"#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1280000\nvariant.m3u8\n",
	} {
		if got := parseM3UWithBase(manifest, "https://example.test/live/index.m3u8"); len(got) != 0 {
			t.Fatalf("HLS segments/variants became %d channels", len(got))
		}
	}
}

func TestProviderErrorPagesDoNotBecomeRelativeChannels(t *testing.T) {
	for _, response := range []string{
		"<!doctype html>\n<html>\n<body>Account expired</body>\n</html>",
		`{"error":"Invalid account"}`,
	} {
		if got := parseM3UWithBase(response, "https://example.test/playlist/u/p/m3u_plus"); len(got) != 0 {
			t.Fatalf("provider error page became %d channels", len(got))
		}
	}
	if got := parseM3UWithBase("https://example.test/live.ts\n", "https://example.test/list"); len(got) != 1 {
		t.Fatal("absolute plain URL list must still work")
	}
}
