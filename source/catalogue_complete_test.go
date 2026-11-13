package main

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAccountAlwaysLoadsAllArchives(t *testing.T) {
	var direct atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/get.php" {
			direct.Add(1)
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:-1,Live\nhttp://example.test/live/u/p/1.ts\n")
			return
		}
		switch r.URL.Query().Get("action") {
		case "get_live_categories", "get_vod_categories", "get_series_categories":
			fmt.Fprint(w, `[{"category_id":1,"category_name":"Shared"}]`)
		case "get_live_streams", "get_vod_streams":
			fmt.Fprint(w, `[{"stream_id":1,"name":"Entry","category_id":1}]`)
		case "get_series":
			fmt.Fprint(w, `[{"series_id":1,"name":"Series","category_id":1}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer server.Close()
	a := &application{token: "saved", validUntil: time.Now().Add(time.Minute), lease: grant{OK: true, Allowed: true, RemainingSeconds: 60, PlaylistURL: server.URL + "/get.php?username=u&password=p&type=m3u_plus&output=mpegts"}}
	items, err := a.loadPlaylist()
	if err != nil {
		t.Fatal(err)
	}
	c := catalogueCounts(items)
	if len(items) != 3 || direct.Load() != 0 || c["Movies"] != 1 || c["Series"] != 1 || c["Live TV"] != 1 {
		t.Fatal(c, len(items), direct.Load())
	}
	if !strings.HasSuffix(items[0].URL, ".ts") {
		t.Fatal("mpegts output changed")
	}
}
func TestLargeCatalogueCompleteCounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("action")
		if strings.HasSuffix(action, "categories") {
			fmt.Fprint(w, `[{"category_id":1,"category_name":"Shared"}]`)
			return
		}
		count, key := 15397, "stream_id"
		if action == "get_vod_streams" {
			count = 247443
		}
		if action == "get_series" {
			count, key = 55641, "series_id"
		}
		out := bufio.NewWriterSize(w, 65536)
		fmt.Fprint(out, "\ufeff[")
		for i := 1; i <= count; i++ {
			if i > 1 {
				fmt.Fprint(out, ",")
			}
			fmt.Fprintf(out, `{"%s":%d,"name":"Item %d","category_id":1,"container_extension":"mp4","unused":{"metadata":[1,2,3]}}`, key, i, i)
		}
		fmt.Fprint(out, "]")
		out.Flush()
	}))
	defer server.Close()
	items, err := xtreamImport(xtreamAccount{Base: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	c := catalogueCounts(items)
	if len(items) != 318481 || c["Live TV"] != 15397 || c["Movies"] != 247443 || c["Series"] != 55641 {
		t.Fatal(c, len(items))
	}
	_, counts := archiveGroups(items, "Movies")
	if counts["Shared"] != 247443 {
		t.Fatal("mixed category types", counts)
	}
}
func TestFailedMoviesCannotCommitPartialCatalogue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("action") {
		case "get_vod_streams":
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":"unavailable"}`)
		case "get_live_streams":
			fmt.Fprint(w, `[{"stream_id":1}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer server.Close()
	if rows, err := xtreamImport(xtreamAccount{Base: server.URL, Username: "u", Password: "p"}); err == nil || len(rows) > 0 {
		t.Fatal("partial import accepted")
	}
}
func TestDirectTSFormatsBothSchemes(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		for _, ext := range []string{"ts", "mpegts", "m2ts", "mts"} {
			raw := scheme + "://example.test:8080/channel." + ext + "?token=opaque"
			a := &application{token: "saved", validUntil: time.Now().Add(time.Minute), lease: grant{OK: true, Allowed: true, RemainingSeconds: 60, PlaylistURL: raw}}
			rows, err := a.loadPlaylist()
			if err != nil || len(rows) != 1 || rows[0].URL != raw {
				t.Fatalf("%s: %v", raw, err)
			}
		}
	}
}
