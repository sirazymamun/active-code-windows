package main

// Single-executable stream recorder: direct HTTP MPEG-TS and unencrypted TS-based
// HLS only. It does not bypass a provider's encryption or access restrictions.
import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const maxRecordBytes int64 = 4 << 30
const maxRecordDuration = 2 * time.Hour

type recordingStatus struct {
	Active  bool   `json:"active"`
	File    string `json:"file,omitempty"`
	Bytes   int64  `json:"bytes"`
	Seconds int64  `json:"seconds"`
	Error   string `json:"error,omitempty"`
}
type recordJob struct {
	cancel  context.CancelFunc
	started time.Time
	file    string
	bytes   int64 // atomic
	done    chan struct{}
}

func (a *application) recordStatus() recordingStatus {
	a.recMu.Lock()
	defer a.recMu.Unlock()
	if j := a.recorder; j != nil {
		return recordingStatus{Active: true, File: j.file, Bytes: atomic.LoadInt64(&j.bytes), Seconds: int64(time.Since(j.started).Seconds())}
	}
	return a.lastRecord
}
func (a *application) startRecording(id int) (recordingStatus, error) {
	if _, err := a.ensure(true); err != nil {
		return recordingStatus{}, err
	}
	a.mu.RLock()
	if id < 0 || id >= len(a.channels) {
		a.mu.RUnlock()
		return recordingStatus{}, errors.New("Choose a valid live channel before recording")
	}
	ch := a.channels[id]
	a.mu.RUnlock()
	if ch.Type != "Live TV" {
		return recordingStatus{}, errors.New("Recording supports Live TV only. Movies and Series should be played normally.")
	}
	if !validURL(ch.URL) {
		return recordingStatus{}, errors.New("Invalid stream URL")
	}
	a.recMu.Lock()
	defer a.recMu.Unlock()
	if a.recorder != nil {
		return recordingStatus{}, errors.New("A recording is already running. Stop it before changing channel.")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return recordingStatus{}, e
	}
	dir := filepath.Join(home, "Videos", "active-code")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return recordingStatus{}, e
	}
	ext := ".ts"
	u, _ := url.Parse(ch.URL)
	if strings.HasSuffix(strings.ToLower(u.Path), ".mp4") {
		ext = ".mp4"
	}
	safe := regexp.MustCompile(`[^\pL\pN._ -]+`).ReplaceAllString(ch.Name, "_")
	safe = strings.Trim(strings.TrimSpace(safe), ". ")
	if safe == "" {
		safe = "channel"
	}
	if len([]rune(safe)) > 55 {
		safe = string([]rune(safe)[:55])
	}
	name := fmt.Sprintf("%s_%s%s", safe, time.Now().Format("20060102_150405.000"), ext)
	file := filepath.Join(dir, name)
	out, e := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return recordingStatus{}, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), maxRecordDuration)
	job := &recordJob{cancel: cancel, started: time.Now(), file: file, done: make(chan struct{})}
	a.recorder = job
	a.lastRecord = recordingStatus{}
	go a.recordWorker(ctx, job, ch.URL, out)
	return recordingStatus{Active: true, File: file}, nil
}
func (a *application) stopRecording() recordingStatus {
	a.recMu.Lock()
	j := a.recorder
	a.recMu.Unlock()
	if j != nil {
		j.cancel()
		select {
		case <-j.done:
		case <-time.After(1500 * time.Millisecond):
		}
	}
	return a.recordStatus()
}
func (a *application) recordWorker(ctx context.Context, j *recordJob, src string, out *os.File) {
	defer close(j.done)
	defer j.cancel()
	// Recheck subscription while recording, including long-running progressive streams.
	stopCheck := make(chan struct{})
	go func() {
		defer close(stopCheck)
		t := time.NewTicker(45 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, e := a.ensure(true); e != nil {
					j.cancel()
					return
				}
			}
		}
	}()
	err := recordStream(ctx, j, src, out)
	if e := out.Close(); err == nil && e != nil {
		err = e
	}
	a.recMu.Lock()
	status := recordingStatus{Active: false, File: j.file, Bytes: atomic.LoadInt64(&j.bytes), Seconds: int64(time.Since(j.started).Seconds())}
	if err != nil && !errors.Is(err, context.Canceled) {
		status.Error = userError(err)
	}
	if status.Bytes == 0 {
		_ = os.Remove(j.file)
		status.File = ""
	}
	if a.recorder == j {
		a.recorder = nil
		a.lastRecord = status
	}
	a.recMu.Unlock()
}
func recordHTTP(ctx context.Context, src string) (*http.Response, error) {
	if !validURL(src) {
		return nil, errors.New("Invalid stream URL")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "active-code-Windows/"+appVersion)
	client := &http.Client{Transport: &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 20 * time.Second}}
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, fmt.Errorf("Stream server returned HTTP %d", res.StatusCode)
	}
	return res, nil
}
func copyRecording(ctx context.Context, j *recordJob, dst io.Writer, src io.Reader) error {
	buf := make([]byte, 65536)
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		n, e := src.Read(buf)
		if n > 0 {
			if atomic.LoadInt64(&j.bytes)+int64(n) > maxRecordBytes {
				return errors.New("Recording size limit reached (4 GB)")
			}
			written, writeErr := dst.Write(buf[:n])
			atomic.AddInt64(&j.bytes, int64(written))
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}
func isHLSLink(raw, ctype string) bool {
	u, e := url.Parse(raw)
	if e != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	t := strings.ToLower(ctype)
	return strings.HasSuffix(p, ".m3u8") || strings.Contains(t, "mpegurl")
}
func recordStream(ctx context.Context, j *recordJob, src string, out io.Writer) error {
	if isHLSLink(src, "") {
		return recordHLS(ctx, j, src, out, 0)
	}
	res, e := recordHTTP(ctx, src)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if isHLSLink(src, res.Header.Get("Content-Type")) {
		return recordHLS(ctx, j, src, out, 0)
	}
	return copyRecording(ctx, j, out, res.Body)
}
func fetchManifest(ctx context.Context, src string) (string, error) {
	res, e := recordHTTP(ctx, src)
	if e != nil {
		return "", e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
	if e != nil {
		return "", e
	}
	if len(b) > 1024*1024 {
		return "", errors.New("HLS manifest exceeds 1MB")
	}
	s := strings.TrimPrefix(string(b), "\ufeff")
	if !strings.HasPrefix(strings.TrimSpace(s), "#EXTM3U") {
		return "", errors.New("Not a valid HLS playlist")
	}
	return s, nil
}
func getSegments(manifest, base string) ([]string, int, bool, error) {
	if strings.Contains(manifest, "#EXT-X-KEY:") || strings.Contains(manifest, "#EXT-X-SESSION-KEY:") {
		return nil, 0, false, errors.New("Encrypted HLS recording is not supported")
	}
	if strings.Contains(manifest, "#EXT-X-MAP:") {
		return nil, 0, false, errors.New("Fragmented MP4 HLS recording is not supported")
	}
	u, e := url.Parse(base)
	if e != nil {
		return nil, 0, false, e
	}
	segments := []string{}
	seconds := 3
	ended := false
	s := bufio.NewScanner(strings.NewReader(manifest))
	s.Buffer(make([]byte, 4096), 1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"))); err == nil && n > 0 {
				seconds = clampRecordInt(n, 1, 10)
			}
		}
		if strings.HasPrefix(line, "#EXT-X-ENDLIST") {
			ended = true
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seg, e := url.Parse(line)
		if e != nil {
			return nil, 0, false, e
		}
		resolved := u.ResolveReference(seg).String()
		if !validURL(resolved) {
			return nil, 0, false, errors.New("Invalid HLS segment URL")
		}
		if strings.HasSuffix(strings.ToLower(seg.Path), ".m4s") || strings.HasSuffix(strings.ToLower(seg.Path), ".mp4") {
			return nil, 0, false, errors.New("Only MPEG-TS HLS segments can be saved")
		}
		segments = append(segments, resolved)
	}
	return segments, seconds, ended, s.Err()
}
func clampRecordInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
func recordHLS(ctx context.Context, j *recordJob, src string, out io.Writer, depth int) error {
	if depth > 2 {
		return errors.New("Too many nested HLS playlists")
	}
	seen := make(map[string]bool)
	// TS-only HLS recording, including master playlists; polling ends on Stop/timeout.
	for {
		manifest, e := fetchManifest(ctx, src)
		if e != nil {
			return e
		}
		chunks, seconds, ended, e := getSegments(manifest, src)
		if e != nil {
			return e
		}
		if strings.Contains(manifest, "#EXT-X-STREAM-INF") {
			if len(chunks) == 0 {
				return errors.New("No HLS variant available")
			}
			return recordHLS(ctx, j, chunks[0], out, depth+1)
		}
		if !strings.Contains(manifest, "#EXT-X-TARGETDURATION") && !strings.Contains(manifest, "#EXTINF:") {
			return errors.New("Unrecognized HLS media manifest")
		}
		for _, piece := range chunks {
			if seen[piece] {
				continue
			}
			seen[piece] = true
			if len(seen) > 50000 {
				return errors.New("Too many HLS segments in one recording")
			}
			res, e := recordHTTP(ctx, piece)
			if e != nil {
				return e
			}
			// A HLS segment is bounded. An unsupported provider response does not fill the hard drive.
			limited := io.LimitReader(res.Body, 64<<20+1)
			e = copyRecording(ctx, j, out, limited)
			_ = res.Body.Close()
			if e != nil {
				return e
			}
		}
		if ended {
			return nil
		}
		timer := time.NewTimer(time.Duration(clampRecordInt(seconds/2, 1, 5)) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
