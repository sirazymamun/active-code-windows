package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Xtream get_series returns collections, not playable episode IDs.
// Fetch seasons/episodes only for the collection the user opens.
func loadSeriesEpisodes(parent channel) ([]channel, error) {
	if parent.SeriesAccount == nil || parent.SeriesID == "" {
		return nil, errors.New("Series account metadata unavailable")
	}
	a := parent.SeriesAccount
	query := url.Values{"username": {a.Username}, "password": {a.Password}, "action": {"get_series_info"}, "series_id": {parent.SeriesID}}
	endpoint := strings.TrimRight(a.Base, "/") + "/player_api.php?" + query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var last error
	for _, ua := range playlistUserAgents[:3] {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "application/json")
		res, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
		if err != nil {
			last = errors.New("Series API connection failed")
			continue
		}
		var data struct {
			Episodes map[string][]map[string]any `json:"episodes"`
		}
		decoder := json.NewDecoder(io.LimitReader(res.Body, 16<<20))
		decoder.UseNumber()
		err = decoder.Decode(&data)
		res.Body.Close()
		if err != nil || res.StatusCode < 200 || res.StatusCode >= 300 {
			last = fmt.Errorf("Series API unavailable (HTTP %d)", res.StatusCode)
			continue
		}
		seasons := make([]string, 0, len(data.Episodes))
		for season := range data.Episodes {
			seasons = append(seasons, season)
		}
		sort.Slice(seasons, func(i, j int) bool {
			x, e1 := strconv.Atoi(seasons[i])
			y, e2 := strconv.Atoi(seasons[j])
			if e1 == nil && e2 == nil {
				return x < y
			}
			return seasons[i] < seasons[j]
		})
		items := []channel{}
		for _, season := range seasons {
			rows := data.Episodes[season]
			sort.SliceStable(rows, func(i, j int) bool {
				x, _ := strconv.Atoi(anyString(rows[i]["episode_num"]))
				y, _ := strconv.Atoi(anyString(rows[j]["episode_num"]))
				return x < y
			})
			for _, row := range rows {
				id := anyString(row["id"])
				if id == "" {
					continue
				}
				ext := anyString(row["container_extension"])
				if ext == "" || len(ext) > 8 || strings.ContainsAny(ext, "/\\?&#") {
					ext = "mp4"
				}
				stream := fmt.Sprintf("%s/series/%s/%s/%s.%s", strings.TrimRight(a.Base, "/"), url.PathEscape(a.Username), url.PathEscape(a.Password), url.PathEscape(id), ext)
				if direct := anyString(row["direct_source"]); validURL(direct) {
					stream = direct
				}
				name := strings.TrimSpace(anyString(row["title"]))
				if name == "" {
					name = "Episode " + anyString(row["episode_num"])
				}
				items = append(items, channel{Name: name, Group: parent.Name + " · Season " + season, URL: stream, Type: "Series", Headers: parent.Headers})
			}
		}
		if len(items) > 0 {
			return items, nil
		}
		return nil, errors.New("This series contains no playable episodes")
	}
	if last == nil {
		last = errors.New("Series API timed out")
	}
	return nil, last
}
