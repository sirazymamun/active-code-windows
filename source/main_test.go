package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersion(t *testing.T) {
	if appVersion != "5.1.8" {
		t.Fatal("mismatched version")
	}
}
func TestM3UParserAndGroup(t *testing.T) {
	text := `#EXTM3U
#EXTINF:-1 tvg-name="Example" tvg-logo="https://example.org/logo.png" group-title="SPORTS | UK",Sport UHD
https://cdn.example.org/live/stream.ts
#EXTINF:-1 group-title="VOD | MOVIES",My Film
https://cdn.example.org/movie.mp4
#EXTINF:-1 group-title="SERIES | EN",Episode S01E02
https://cdn.example.org/series/1.mp4`
	got := parseM3U(text)
	if len(got) != 3 {
		t.Fatalf("entries=%d", len(got))
	}
	if got[0].Group != "SPORTS | UK" || got[0].Name != "Sport UHD" || got[0].Type != "Live TV" {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].Type != "Movies" || got[2].Type != "Series" {
		t.Fatalf("classifications: %+v", got)
	}
	if got[0].URL == "" || got[0].Logo == "" {
		t.Fatal("missing stream data")
	}
	b, _ := json.Marshal(got[0])
	if strings.Contains(string(b), "cdn.example.org/live") {
		t.Fatal("private stream URL exposed in playlist API")
	}
}
func TestNoHLSMisclassification(t *testing.T) {
	if got := parseM3U("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:3.5\nsegment-1.ts\n"); len(got) != 0 {
		t.Fatal("HLS manifest treated as channels")
	}
}
func TestActivateRejectsBadCodeWithoutNetwork(t *testing.T) {
	a, err := newApplication(filepath.Join(t.TempDir(), "test.dat"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"", "12", "a123456789", "12345678901", "12345678 0"} {
		if _, e := a.activate(v); e == nil {
			t.Errorf("accepted bad code %q", v)
		}
	}
}
func TestSessionStorage(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app", "state.dat")
	a, err := newApplication(file)
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.token = strings.Repeat("a", 64)
	err = a.saveLocked()
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newApplication(file)
	if err != nil {
		t.Fatal(err)
	}
	if a.deviceID != b.deviceID || a.token != b.token {
		t.Fatal("device identity not saved")
	}
	if e := os.Remove(file); e != nil {
		t.Fatal(e)
	}
}
func TestAccessLeaseRequiresCheck(t *testing.T) {
	a, err := newApplication(filepath.Join(t.TempDir(), "state.dat"))
	if err != nil {
		t.Fatal(err)
	}
	a.lease = grant{OK: true, Allowed: true, RemainingSeconds: 60, PlaylistURL: "https://example.com/p.m3u"}
	a.validUntil = time.Now().Add(30 * time.Second)
	if _, err = a.ensure(false); err != nil {
		t.Fatal(err)
	}
	a.validUntil = time.Now().Add(-time.Second)
	if _, err = a.ensure(false); err == nil {
		t.Fatal("expired access allowed without remote check")
	}
}
func TestURLValidation(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "file:///C:/Windows/", "https://", "foo", "http://"} {
		if validURL(raw) {
			t.Errorf("accepted invalid URL %s", raw)
		}
	}
	for _, raw := range []string{"https://example.org/live.m3u8", "http://example.org/live.ts"} {
		if !validURL(raw) {
			t.Errorf("rejected valid URL %s", raw)
		}
	}
	_ = http.MethodGet
}

func TestM3UHeadersRelativeAndKodiPipe(t *testing.T) {
	text := `#EXTM3U
#EXTINF:-1 group-title="News",Header Channel
#EXTVLCOPT:http-user-agent=ProviderUA/1.0
#EXTVLCOPT:http-referrer=https://portal.example/
/live/100.ts|User-Agent=PipeUA%2F2.0&Referer=https%3A%2F%2Fref.example%2F
`
	got := parseM3UWithBase(text, "http://iptv.example:8080/get.php?username=u&password=p&type=m3u_plus")
	if len(got) != 1 {
		t.Fatalf("entries=%d", len(got))
	}
	if got[0].URL != "http://iptv.example:8080/live/100.ts" {
		t.Fatalf("relative URL=%q", got[0].URL)
	}
	if got[0].Headers["User-Agent"] != "PipeUA/2.0" || got[0].Headers["Referer"] != "https://ref.example/" {
		t.Fatalf("headers=%v", got[0].Headers)
	}
}

func TestParseXtreamAndXUIURLs(t *testing.T) {
	x, ok := parseXtreamURL("http://example.test:8080/get.php?username=user&password=pass&type=m3u_plus&output=m3u8")
	if !ok || x.Base != "http://example.test:8080" || x.Username != "user" || x.Password != "pass" || x.Output != "m3u8" {
		t.Fatalf("xtream=%+v ok=%v", x, ok)
	}
	x, ok = parseXtreamURL("https://xui.example/panel/playlist/user%20one/pass%2Bword/m3u_plus")
	if !ok || x.Base != "https://xui.example/panel" || x.Username != "user one" || x.Password != "pass+word" {
		t.Fatalf("xui=%+v ok=%v", x, ok)
	}
}

func TestXtreamImportLiveMovieSeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("action")
		w.Header().Set("Content-Type", "application/json")
		switch action {
		case "get_live_categories":
			_, _ = w.Write([]byte(`[{"category_id":"1","category_name":"Live Group"}]`))
		case "get_live_streams":
			_, _ = w.Write([]byte(`[{"stream_id":101,"name":"Live One","category_id":"1"}]`))
		case "get_vod_categories":
			_, _ = w.Write([]byte(`[{"category_id":"2","category_name":"Movies Group"}]`))
		case "get_vod_streams":
			_, _ = w.Write([]byte(`[{"stream_id":"202","name":"Movie One","category_id":"2","container_extension":"mkv"}]`))
		case "get_series_categories":
			_, _ = w.Write([]byte(`[{"category_id":"3","category_name":"Series Group"}]`))
		case "get_series":
			_, _ = w.Write([]byte(`[{"series_id":"303","name":"Series One","category_id":"3"}]`))
		default:
			http.Error(w, "unknown", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	items, err := xtreamImport(xtreamAccount{Base: srv.URL, Username: "u", Password: "p", Output: "m3u8"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items=%d %+v", len(items), items)
	}
	if items[0].Type != "Live TV" || !strings.Contains(items[0].URL, "/live/u/p/101.m3u8") {
		t.Fatalf("live=%+v", items[0])
	}
	if items[1].Type != "Movies" || !strings.Contains(items[1].URL, "/movie/u/p/202.mkv") {
		t.Fatalf("movie=%+v", items[1])
	}
	if items[2].Type != "Series" || !strings.Contains(items[2].URL, "/series/u/p/303") {
		t.Fatalf("series=%+v", items[2])
	}
}
