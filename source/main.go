// active-code Windows 5.1.8: single-file Windows player with per-user setup.
// Copyright (c) active-code. Standard-library-only Go build.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const appVersion = "5.1.8"
const apiURL = "https://active-code.netcolink.com/api.php"
const maxPlaylistBytes = 256 << 20
const maxChannels = 1000000

type channel struct {
	SeriesID      string            `json:"-"`
	SeriesAccount *xtreamAccount    `json:"-"`
	Name          string            `json:"name"`
	Group         string            `json:"group"`
	URL           string            `json:"-"`
	Type          string            `json:"type"`
	Logo          string            `json:"logo,omitempty"`
	Headers       map[string]string `json:"-"`
}

type grant struct {
	OK               bool   `json:"ok"`
	Allowed          bool   `json:"allowed"`
	SubscriptionID   int64  `json:"subscription_id"`
	Label            string `json:"label"`
	PlaylistURL      string `json:"playlist_url"`
	PlaylistVersion  int    `json:"playlist_version"`
	RemainingSeconds int64  `json:"remaining_seconds"`
	DeviceToken      string `json:"device_token,omitempty"`
}

type savedSession struct {
	DeviceID string `json:"device_id"`
	Secret   string `json:"secret,omitempty"`
}

type application struct {
	mu         sync.RWMutex
	recMu      sync.Mutex
	recorder   *recordJob
	lastRecord recordingStatus
	deviceID   string
	token      string
	lease      grant
	validUntil time.Time
	channels   []channel
	revision   string
	authPath   string
	client     *http.Client
	stateFile  string
}

func randomHex(n int) (string, error) {
	p := make([]byte, n)
	if _, err := rand.Read(p); err != nil {
		return "", err
	}
	return hex.EncodeToString(p), nil
}

func newApplication(stateFile string) (*application, error) {
	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	secret, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	a := &application{deviceID: id, authPath: "/app/" + secret, client: &http.Client{Timeout: 25 * time.Second}, stateFile: stateFile}
	a.loadSaved()
	return a, nil
}

func (a *application) loadSaved() {
	payload, err := os.ReadFile(a.stateFile)
	if err != nil {
		return
	}
	clear, err := unprotectBytes(payload)
	if err != nil {
		return
	}
	var s savedSession
	if json.Unmarshal(clear, &s) == nil && regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(s.DeviceID) {
		a.deviceID = s.DeviceID
		if regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(s.Secret) {
			a.token = s.Secret
		}
	}
}

func (a *application) saveLocked() error {
	blob, err := json.Marshal(savedSession{DeviceID: a.deviceID, Secret: a.token})
	if err != nil {
		return err
	}
	sealed, err := protectBytes(blob)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(a.stateFile), 0700); err != nil {
		return err
	}
	temp := a.stateFile + ".tmp"
	if err = os.WriteFile(temp, sealed, 0600); err != nil {
		return err
	}
	return os.Rename(temp, a.stateFile)
}

func (a *application) remote(action string, body any) (grant, error) {
	if action != "activate" && action != "device-check" {
		return grant{}, errors.New("Unsupported device request")
	}
	data, err := json.Marshal(body)
	if err != nil {
		return grant{}, err
	}
	req, err := http.NewRequest(http.MethodPost, apiURL+"?action="+action, strings.NewReader(string(data)))
	if err != nil {
		return grant{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) active-code/"+appVersion)
	res, err := a.client.Do(req)
	if err != nil {
		return grant{}, errors.New("Unable to connect. Please check your internet connection and try again.")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		switch res.StatusCode {
		case 400, 401, 403, 404, 410:
			return grant{}, errors.New("Activation unavailable. Please check your code or contact your subscription vendor.")
		case 409, 429:
			return grant{}, errors.New("Service is busy. Please try again shortly.")
		default:
			return grant{}, errors.New("Service temporarily unavailable. Please try again later.")
		}
	}
	var result grant
	if err = json.NewDecoder(io.LimitReader(res.Body, 32769)).Decode(&result); err != nil {
		return grant{}, errors.New("Invalid activation server response")
	}
	if !result.OK || !result.Allowed || result.RemainingSeconds <= 0 || result.RemainingSeconds > 3650*24*3600 || !validURL(result.PlaylistURL) {
		return grant{}, errors.New("Subscription inactive or invalid server response")
	}
	return result, nil
}

func (a *application) ensure(checkFresh bool) (grant, error) {
	a.mu.RLock()
	token, id, lease, until := a.token, a.deviceID, a.lease, a.validUntil
	a.mu.RUnlock()
	if !checkFresh && lease.Allowed && time.Now().Before(until) {
		return lease, nil
	}
	if token == "" {
		return grant{}, errors.New("Activation required")
	}
	result, err := a.remote("device-check", map[string]string{"device_id": id, "device_token": token})
	a.mu.Lock()
	defer a.mu.Unlock()
	// Android discards access checks from an earlier activation generation.
	// A slow old-token response must not revoke a newly activated subscription.
	if token != a.token {
		if a.lease.Allowed && time.Now().Before(a.validUntil) {
			return a.lease, nil
		}
		return grant{}, errors.New("Activation changed during verification; retry")
	}
	if err != nil {
		a.lease = grant{}
		a.validUntil = time.Time{}
		return grant{}, err
	}
	if result.PlaylistVersion != a.lease.PlaylistVersion || result.PlaylistURL != a.lease.PlaylistURL || result.SubscriptionID != a.lease.SubscriptionID {
		a.channels = nil
		a.revision = ""
	}
	a.lease = result
	a.validUntil = time.Now().Add(time.Duration(min64(result.RemainingSeconds, 55)) * time.Second)
	return result, nil
}

func (a *application) activate(code string) (grant, error) {
	if len(code) != 10 || strings.Trim(code, "0123456789") != "" {
		return grant{}, errors.New("Enter exactly 10 digits")
	}
	a.mu.RLock()
	id := a.deviceID
	a.mu.RUnlock()
	result, err := a.remote("activate", map[string]string{"device_id": id, "code": code})
	if err != nil {
		return grant{}, err
	}
	if len(result.DeviceToken) != 64 || strings.Trim(result.DeviceToken, "0123456789abcdef") != "" {
		return grant{}, errors.New("Invalid device token response")
	}
	a.mu.Lock()
	a.token = result.DeviceToken
	a.lease = result
	a.channels = nil
	a.revision = ""
	a.validUntil = time.Now().Add(time.Duration(min64(result.RemainingSeconds, 55)) * time.Second)
	saveErr := a.saveLocked()
	a.mu.Unlock()
	if saveErr != nil {
		return grant{}, fmt.Errorf("Access approved, but cannot save device session: %w", saveErr)
	}
	return result, nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func validURL(raw string) bool {
	if len(raw) > 8192 {
		return false
	}
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

func parseAttribute(line, key string) string {
	query := key + `="`
	p := strings.Index(strings.ToLower(line), strings.ToLower(query))
	if p < 0 {
		return ""
	}
	s := line[p+len(query):]
	n := strings.IndexByte(s, '"')
	if n < 0 {
		return ""
	}
	return strings.TrimSpace(s[:n])
}

func classify(_ string, raw string) string { return classifyMedia(raw, "", "") }

func parseHeaderString(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		i := strings.IndexByte(pair, '=')
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(pair[:i])
		value := pair[i+1:]
		if decoded, err := url.QueryUnescape(value); err == nil {
			value = decoded
		}
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func canonicalHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range in {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		switch strings.ToLower(k) {
		case "user-agent", "useragent":
			out["User-Agent"] = v
		case "referer", "referrer":
			out["Referer"] = v
		default:
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolveStreamURL(raw, base string) (string, map[string]string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return "", nil, false
	}
	var pipeHeaders map[string]string
	if i := strings.IndexByte(line, '|'); i > 0 {
		tail := line[i+1:]
		if j := strings.IndexByte(tail, '='); j > 0 {
			pipeHeaders = parseHeaderString(tail)
			line = strings.TrimSpace(line[:i])
		}
	}
	u, err := url.Parse(line)
	if err != nil {
		return "", nil, false
	}
	if !u.IsAbs() {
		if base == "" {
			return "", nil, false
		}
		b, err := url.Parse(base)
		if err != nil {
			return "", nil, false
		}
		u = b.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", nil, false
	}
	if u.Hostname() == "" {
		return "", nil, false
	}
	return u.String(), canonicalHeaders(pipeHeaders), true
}

func isHLSManifest(raw string) bool {
	// HLS control tags are in the header. Never uppercase a 200+ MB catalogue
	// merely to inspect its format; Android also inspects a bounded prefix.
	if len(raw) > 65536 {
		raw = raw[:65536]
	}
	for _, line := range strings.Split(strings.ToUpper(raw), "\n") {
		line = strings.TrimSpace(line)
		for _, marker := range []string{"#EXT-X-TARGETDURATION:", "#EXT-X-MEDIA-SEQUENCE:", "#EXT-X-STREAM-INF:", "#EXT-X-I-FRAME-STREAM-INF:", "#EXT-X-PART-INF:"} {
			if strings.HasPrefix(line, marker) {
				return true
			}
		}
	}
	return false
}

func parseM3UWithBase(raw, baseURL string) []channel {
	// A single HLS media/master manifest is not a channel catalogue. A URL that
	// happens to end in .m3u8 can still appear as an entry inside an M3U_PLUS list.
	if isHLSManifest(raw) {
		return nil
	}
	head := raw
	if len(head) > 65536 {
		head = head[:65536]
	}
	head = strings.ToUpper(head)
	hasExt := strings.Contains(head, "#EXTM3U") || strings.Contains(head, "#EXTINF")
	list := make([]channel, 0, 1024)
	info, group, title, logo := "", "", "", ""
	optHeaders := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 65536), 2<<20)
	for scanner.Scan() {
		original := scanner.Text()
		line := strings.TrimSpace(strings.TrimPrefix(original, "\ufeff"))
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "#EXTINF:") {
			info = line
			group = parseAttribute(info, "group-title")
			title = parseAttribute(info, "tvg-name")
			logo = parseAttribute(info, "tvg-logo")
			if j := strings.LastIndex(info, ","); j >= 0 {
				visible := strings.TrimSpace(info[j+1:])
				if visible != "" {
					title = visible
				}
			}
			continue
		}
		if strings.HasPrefix(upper, "#EXTGRP:") {
			group = strings.TrimSpace(line[len("#EXTGRP:"):])
			continue
		}
		if strings.HasPrefix(upper, "#EXTVLCOPT:") {
			body := strings.TrimSpace(line[len("#EXTVLCOPT:"):])
			if i := strings.IndexByte(body, '='); i > 0 {
				key := strings.ToLower(strings.TrimSpace(body[:i]))
				value := strings.TrimSpace(body[i+1:])
				switch key {
				case "http-user-agent":
					optHeaders["User-Agent"] = value
				case "http-referer", "http-referrer":
					optHeaders["Referer"] = value
				}
			}
			continue
		}
		if strings.HasPrefix(upper, "#EXTHTTP:") {
			var extras map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(line[len("#EXTHTTP:"):])), &extras) == nil {
				for k, v := range extras {
					if text, ok := v.(string); ok && strings.TrimSpace(text) != "" {
						optHeaders[k] = text
					}
				}
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Plain URL lists require absolute HTTP(S) URLs. Otherwise an HTML/JSON
		// error page can become fake channels when resolved against the provider URL.
		if !hasExt && !validURL(strings.SplitN(line, "|", 2)[0]) {
			continue
		}
		streamURL, pipeHeaders, ok := resolveStreamURL(line, baseURL)
		if !ok {
			// Android accepts a plain list of absolute URLs, but ignores unrelated
			// junk lines in non-M3U input. Keep the same rule here.
			if !hasExt {
				info, group, title, logo = "", "", "", ""
				optHeaders = map[string]string{}
			}
			continue
		}
		if info == "" && hasExt {
			// Extended M3U may contain URL-only entries. Accept them just like Android.
		}
		if title == "" {
			if u, err := url.Parse(streamURL); err == nil {
				name := strings.Trim(strings.ReplaceAll(filepath.Base(u.Path), "_", " "), " /.-")
				if name != "" {
					title = name
				}
			}
		}
		if title == "" {
			title = "Channel " + strconv.Itoa(len(list)+1)
		}
		if group == "" {
			group = "Other"
		}
		if len(title) > 300 {
			title = title[:300]
		}
		if len(group) > 160 {
			group = group[:160]
		}
		if !validURL(logo) {
			logo = ""
		}
		headers := map[string]string{}
		for k, v := range optHeaders {
			headers[k] = v
		}
		for k, v := range pipeHeaders {
			headers[k] = v
		}
		list = append(list, channel{Name: title, Group: group, URL: streamURL, Logo: logo, Type: classifyMedia(streamURL, title, contentTypeHint(info)), Headers: canonicalHeaders(headers)})
		if len(list) >= maxChannels {
			break
		}
		info, group, title, logo = "", "", "", ""
		optHeaders = map[string]string{}
	}
	if scanner.Err() != nil {
		return nil
	}
	return list
}

func parseM3U(raw string) []channel { return parseM3UWithBase(raw, "") }

var playlistUserAgents = []string{
	"IPTVSmartersPro",
	"VLC/3.0.18 LibVLC/3.0.18",
	"Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36",
	"Lavf/60.16.100",
	"okhttp/4.12.0",
}

type xtreamAccount struct {
	Base, Username, Password, Output string
}

var xuiPlaylistPath = regexp.MustCompile(`(?i)^(.*)/playlist/([^/]+)/([^/]+)/(?:m3u_plus|m3u|m3u_plus\.php|m3u\.php)/?$`)

func parseXtreamURL(raw string) (xtreamAccount, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return xtreamAccount{}, false
	}
	lowerPath := strings.ToLower(u.Path)
	for _, suffix := range []string{"/get.php", "/panel_api.php", "/player_api.php"} {
		if strings.HasSuffix(lowerPath, suffix) {
			username, password := u.Query().Get("username"), u.Query().Get("password")
			if username == "" || password == "" {
				return xtreamAccount{}, false
			}
			dir := strings.TrimSuffix(u.Path, u.Path[strings.LastIndex(u.Path, "/"):])
			return xtreamAccount{Base: strings.TrimRight(u.Scheme+"://"+u.Host+dir, "/"), Username: username, Password: password, Output: strings.ToLower(u.Query().Get("output"))}, true
		}
	}
	if m := xuiPlaylistPath.FindStringSubmatch(u.Path); len(m) == 4 {
		username, e1 := url.PathUnescape(m[2])
		password, e2 := url.PathUnescape(m[3])
		if e1 != nil {
			username = m[2]
		}
		if e2 != nil {
			password = m[3]
		}
		if username == "" || password == "" {
			return xtreamAccount{}, false
		}
		return xtreamAccount{Base: strings.TrimRight(u.Scheme+"://"+u.Host+m[1], "/"), Username: username, Password: password, Output: "ts"}, true
	}
	return xtreamAccount{}, false
}

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return ""
	}
}

func xtreamJSON(account xtreamAccount, action string) ([]map[string]any, error) {
	return xtreamJSONContext(context.Background(), account, action)
}
func xtreamJSONContext(ctx context.Context, account xtreamAccount, action string) ([]map[string]any, error) {
	values := url.Values{}
	values.Set("username", account.Username)
	values.Set("password", account.Password)
	values.Set("action", action)
	endpoint := strings.TrimRight(account.Base, "/") + "/player_api.php?" + values.Encode()
	var last error
	for _, ua := range playlistUserAgents[:3] {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "application/json, text/plain, */*")
		client := &http.Client{Timeout: 12 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		dec := json.NewDecoder(io.LimitReader(res.Body, maxPlaylistBytes+1))
		dec.UseNumber()
		var rows []map[string]any
		err = dec.Decode(&rows)
		res.Body.Close()
		if err == nil && res.StatusCode >= 200 && res.StatusCode < 300 {
			return rows, nil
		}
		if err != nil {
			last = err
		} else {
			last = fmt.Errorf("Xtream %s HTTP %d", action, res.StatusCode)
		}
	}
	if last == nil {
		last = fmt.Errorf("Xtream endpoint failed: %s", action)
	}
	return nil, last
}

func xtreamCategories(account xtreamAccount, action string) map[string]string {
	rows, err := xtreamJSON(account, action)
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		id, name := anyString(row["category_id"]), anyString(row["category_name"])
		if id != "" && name != "" {
			out[id] = name
		}
	}
	return out
}

func xtreamImport(account xtreamAccount) ([]channel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	type result struct {
		items []channel
		err   error
	}
	ch := make(chan result, 3)
	worker := func(kind, streamAction, catAction string) {
		rows, err := xtreamJSONContext(ctx, account, streamAction)
		if err != nil {
			ch <- result{err: err}
			return
		}
		groups := map[string]string{}
		catCtx, catCancel := context.WithTimeout(ctx, 8*time.Second)
		if cats, e := xtreamJSONContext(catCtx, account, catAction); e == nil {
			for _, row := range cats {
				groups[anyString(row["category_id"])] = anyString(row["category_name"])
			}
		}
		catCancel()
		items := make([]channel, 0, len(rows))
		for _, row := range rows {
			idKey := "stream_id"
			if kind == "series" {
				idKey = "series_id"
			}
			id := anyString(row[idKey])
			if id == "" {
				continue
			}
			name := strings.TrimSpace(anyString(row["name"]))
			if name == "" {
				name = "Item " + id
			}
			group := strings.TrimSpace(groups[anyString(row["category_id"])])
			if group == "" {
				switch kind {
				case "live":
					group = "Live TV"
				case "movie":
					group = "Movies"
				default:
					group = "Series"
				}
			}
			escapedUser := url.PathEscape(account.Username)
			escapedPass := url.PathEscape(account.Password)
			var streamURL, typ string
			switch kind {
			case "live":
				ext := "ts"
				if account.Output == "m3u8" || account.Output == "hls" {
					ext = "m3u8"
				}
				streamURL = fmt.Sprintf("%s/live/%s/%s/%s.%s", strings.TrimRight(account.Base, "/"), escapedUser, escapedPass, url.PathEscape(id), ext)
				typ = "Live TV"
			case "movie":
				ext := strings.TrimSpace(anyString(row["container_extension"]))
				if ext == "" || len(ext) > 8 || strings.ContainsAny(ext, "/\\?&#") {
					ext = "mp4"
				}
				streamURL = fmt.Sprintf("%s/movie/%s/%s/%s.%s", strings.TrimRight(account.Base, "/"), escapedUser, escapedPass, url.PathEscape(id), ext)
				typ = "Movies"
			default:
				streamURL = fmt.Sprintf("%s/series/%s/%s/%s", strings.TrimRight(account.Base, "/"), escapedUser, escapedPass, url.PathEscape(id))
				typ = "Series"
			}
			entry := channel{Name: name, Group: group, URL: streamURL, Type: typ}
			if kind == "series" {
				entry.SeriesID = id
				copyAccount := account
				entry.SeriesAccount = &copyAccount
			}
			items = append(items, entry)
			if len(items) >= maxChannels {
				break
			}
		}
		ch <- result{items: items}
	}
	go worker("live", "get_live_streams", "get_live_categories")
	go worker("movie", "get_vod_streams", "get_vod_categories")
	go worker("series", "get_series", "get_series_categories")
	all := make([]channel, 0, 4096)
	var last error
	for i := 0; i < 3; i++ {
		r := <-ch
		if r.err != nil {
			last = r.err
			continue
		}
		room := maxChannels - len(all)
		if room <= 0 {
			break
		}
		if len(r.items) > room {
			r.items = r.items[:room]
		}
		all = append(all, r.items...)
	}
	if len(all) == 0 {
		if last == nil {
			last = errors.New("Xtream API returned no playable entries")
		}
		return nil, last
	}
	// Android presents Live first, then Movies, then Series. Concurrent requests
	// can finish in any order, so restore deterministic player order here.
	sort.SliceStable(all, func(i, j int) bool {
		rank := func(t string) int {
			if t == "Live TV" {
				return 0
			}
			if t == "Movies" {
				return 1
			}
			return 2
		}
		return rank(all[i].Type) < rank(all[j].Type)
	})
	return all, nil
}

func downloadPlaylist(rawURL string) ([]byte, string, http.Header, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var last error
	for _, ua := range playlistUserAgents {
		if ctx.Err() != nil {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, "", nil, err
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "application/x-mpegURL, application/vnd.apple.mpegurl, audio/x-mpegurl, text/plain, application/octet-stream, */*")
		client := &http.Client{Timeout: 60 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			res.Body.Close()
			last = fmt.Errorf("Playlist server HTTP %d", res.StatusCode)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, maxPlaylistBytes+1))
		res.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			last = fmt.Errorf("Playlist server HTTP %d", res.StatusCode)
			continue
		}
		if len(body) > maxPlaylistBytes {
			return nil, "", nil, errors.New("Playlist exceeds 256 MB limit")
		}
		head := body
		if len(head) > 1024 {
			head = head[:1024]
		}
		prefix := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(string(head), "\ufeff")))
		if prefix == "" || strings.HasPrefix(prefix, "<html") || strings.HasPrefix(prefix, "<!doctype html") || strings.HasPrefix(prefix, "<body") || strings.HasPrefix(prefix, "<head") {
			last = errors.New("Playlist server returned an empty reply or website instead of playlist data")
			continue
		}
		return body, res.Request.URL.String(), res.Header.Clone(), nil
	}
	if last == nil {
		last = errors.New("Playlist download failed")
	}
	return nil, "", nil, last
}

func (a *application) loadPlaylist() ([]channel, error) {
	lease, err := a.ensure(false)
	if err != nil {
		return nil, err
	}
	rev := fmt.Sprintf("%d:%d:%s", lease.SubscriptionID, lease.PlaylistVersion, lease.PlaylistURL)
	a.mu.RLock()
	if a.revision == rev && a.channels != nil {
		v := append([]channel(nil), a.channels...)
		a.mu.RUnlock()
		return v, nil
	}
	a.mu.RUnlock()
	raw, finalURL, headers, directErr := downloadPlaylist(lease.PlaylistURL)
	var items []channel
	var content string
	if directErr == nil {
		content = strings.TrimSpace(strings.TrimPrefix(string(raw), "\ufeff"))
		items = parseM3UWithBase(content, finalURL)
		// Android treats a direct HLS manifest as one playable item rather than a
		// broken channel catalogue. Keep the original provider URL so LibVLC can
		// perform segment resolution itself.
		if len(items) == 0 && isHLSManifest(content) && validURL(finalURL) {
			name := "HLS Stream"
			if u, e := url.Parse(finalURL); e == nil {
				if base := strings.Trim(strings.ReplaceAll(filepath.Base(u.Path), "_", " "), " /.-"); base != "" {
					name = base
				}
			}
			items = []channel{{Name: name, Group: "Live TV", URL: finalURL, Type: "Live TV"}}
		}
	}
	if len(items) == 0 {
		if account, ok := parseXtreamURL(lease.PlaylistURL); ok {
			if imported, xerr := xtreamImport(account); xerr == nil && len(imported) > 0 {
				items = imported
			} else if directErr != nil {
				return nil, fmt.Errorf("Playlist download failed (%v); Xtream/XUI fallback failed (%v)", directErr, xerr)
			}
		}
	}
	if len(items) == 0 {
		if directErr != nil {
			return nil, directErr
		}
		lower := strings.ToLower(content)
		switch {
		case len(raw) == 0:
			return nil, errors.New("Playlist URL returned an empty response. Check provider access and URL")
		case strings.HasPrefix(lower, "<!doctype html"), strings.HasPrefix(lower, "<html"), strings.Contains(strings.ToLower(headers.Get("Content-Type")), "text/html"):
			return nil, errors.New("Playlist URL returned a website/login/error page, not M3U. Check provider credentials and URL")
		case strings.HasPrefix(lower, "{"), strings.HasPrefix(lower, "["):
			return nil, errors.New("Playlist URL returned JSON instead of an M3U channel list. Xtream/XUI fallback was unavailable or returned no channels")
		case !strings.Contains(strings.ToUpper(content), "#EXTINF:"):
			return nil, errors.New("Playlist response contains no playable M3U entries. Check the provider URL/account")
		default:
			return nil, errors.New("Playlist has entries but no supported HTTP/HTTPS stream URLs")
		}
	}
	a.mu.Lock()
	if !a.lease.Allowed || !time.Now().Before(a.validUntil) || a.lease.SubscriptionID != lease.SubscriptionID || a.lease.PlaylistVersion != lease.PlaylistVersion || a.lease.PlaylistURL != lease.PlaylistURL {
		a.mu.Unlock()
		return nil, errors.New("Subscription changed or expired while loading playlist")
	}
	if a.lease.SubscriptionID == lease.SubscriptionID && a.lease.PlaylistVersion == lease.PlaylistVersion && a.lease.PlaylistURL == lease.PlaylistURL {
		a.channels = items
		a.revision = rev
	}
	a.mu.Unlock()
	return items, nil
}

func main() {
	installedLaunch, err := prepareInstalledLaunch()
	if err != nil {
		showError(userError(err))
		return
	}
	if installedLaunch {
		return
	}
	folder, err := os.UserConfigDir()
	if err != nil {
		folder = os.TempDir()
	}
	app, err := newApplication(filepath.Join(folder, "active-code", "windows-v5.1.8.dat"))
	if err != nil {
		showError(userError(err))
		return
	}
	if err := launchNativeUI(app); err != nil {
		showError(userError(err))
	}
}
