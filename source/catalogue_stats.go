package main

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

var seriesDelivery = regexp.MustCompile(`/(?:series|tv[ _-]?series|episodes?)/`)
var movieDelivery = regexp.MustCompile(`/(?:movie|movies|vod|film|films)/`)
var episodeTitle = regexp.MustCompile(`(?i)(s[0-9]{1,2}[ ]*e[0-9]{1,3}|[0-9]{1,2}x[0-9]{1,3}|(?:episode|season)[ ]*[0-9]+|الحلقة[ ]*[0-9]+|পর্ব[ ]*[0-9]+)`)
var typeAttribute = regexp.MustCompile(`(?i)\b(?:stream-type|media-type|tvg-type|type)="([^"]*)"`)

func contentTypeHint(info string) string {
	m := typeAttribute.FindStringSubmatch(info)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// Match Android BigStore.mediaKind: category names are genres, not evidence of VOD.
func classifyMedia(raw, name, hint string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return "Live TV"
	}
	p := strings.ToLower(u.Path)
	h := strings.ToLower(strings.TrimSpace(hint))
	q := ""
	for key, values := range u.Query() {
		switch strings.ToLower(key) {
		case "type", "stream_type", "media_type":
			if len(values) > 0 {
				q = strings.ToLower(values[0])
			}
		}
	}
	if strings.Contains(p, "/live/") || q == "live" || h == "live" {
		return "Live TV"
	}
	if seriesDelivery.MatchString(p) || q == "series" || q == "episode" {
		return "Series"
	}
	if movieDelivery.MatchString(p) || q == "movie" || q == "movies" || q == "vod" || q == "film" {
		return "Movies"
	}
	if h == "series" || h == "episode" || h == "tv_series" {
		return "Series"
	}
	if h == "movie" || h == "movies" || h == "vod" || h == "film" {
		return "Movies"
	}
	switch path.Ext(p) {
	case ".mp4", ".mkv", ".avi", ".mov", ".m4v":
		if episodeTitle.MatchString(name) {
			return "Series"
		}
		return "Movies"
	}
	return "Live TV"
}
func topLevelGroup(s string) bool {
	return strings.EqualFold(s, "Live TV") || strings.EqualFold(s, "Movies") || strings.EqualFold(s, "Series")
}
func catalogueCounts(channels []channel) map[string]int {
	counts := map[string]int{"Live TV": 0, "Movies": 0, "Series": 0}
	for _, ch := range channels {
		counts[ch.Type]++
		if !topLevelGroup(ch.Group) {
			counts[ch.Group]++
		}
	}
	return counts
}

func archiveGroups(channels []channel, section string) ([]string, map[string]int) {
	groups := []string{"Live TV", "Movies", "Series"}
	counts := map[string]int{"Live TV": 0, "Movies": 0, "Series": 0}
	seen := map[string]bool{}
	for _, ch := range channels {
		counts[ch.Type]++
		if ch.Type != section || topLevelGroup(ch.Group) {
			continue
		}
		counts[ch.Group]++
		if !seen[ch.Group] {
			seen[ch.Group] = true
			groups = append(groups, ch.Group)
		}
	}
	return groups, counts
}

// Keep category locks independent when providers reuse a name across archives.
func managedCategoryKey(ch channel) string {
	name := ch.Group
	if ch.LockGroup != "" {
		name = ch.LockGroup
	}
	return "g:" + ch.Type + ":" + name
}
