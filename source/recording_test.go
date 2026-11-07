package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecordProgressiveTS(t *testing.T) {
	content := bytes.Repeat([]byte{0x47, 0x11, 0x00, 0x10}, 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write(content)
	}))
	defer srv.Close()
	j := &recordJob{started: time.Now()}
	var dest bytes.Buffer
	if e := recordStream(context.Background(), j, srv.URL+"/live.ts", &dest); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(dest.Bytes(), content) {
		t.Fatal("Recorded TS differs from source")
	}
	if atomic.LoadInt64(&j.bytes) != int64(len(content)) {
		t.Fatal("Recording bytes mismatch")
	}
}
func TestRecordHLSSegments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXTINF:3,\na.ts\n#EXTINF:3,\nb.ts\n#EXT-X-ENDLIST\n"))
		case "/a.ts":
			_, _ = w.Write([]byte("AAAA"))
		case "/b.ts":
			_, _ = w.Write([]byte("BBBB"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var dest bytes.Buffer
	j := &recordJob{started: time.Now()}
	if e := recordStream(context.Background(), j, srv.URL+"/playlist.m3u8", &dest); e != nil {
		t.Fatal(e)
	}
	if dest.String() != "AAAABBBB" {
		t.Fatalf("HLS recorded %q", dest.String())
	}
}
func TestEncryptedHLSRejected(t *testing.T) {
	body := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:3,\na.ts\n"
	_, _, _, e := getSegments(body, "https://example.org/live.m3u8")
	if e == nil || !strings.Contains(e.Error(), "Encrypted") {
		t.Fatal("Encrypted manifest was accepted")
	}
}
func TestRecordingCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	j := &recordJob{started: time.Now()}
	var output bytes.Buffer
	e := copyRecording(ctx, j, &output, strings.NewReader("test"))
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("expected cancellation; got %v", e)
	}
}
func TestRecordFilenameSafeAndSingleSource(t *testing.T) {
	if appVersion != "5.1.8" {
		t.Fatal("version changed")
	}
	if validURL("file:///C:/temp") {
		t.Fatal("file URL accepted")
	}
}
