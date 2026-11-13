package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestXtreamSeriesLoadsSeasonEpisodeIDsOnDemand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") != "get_series_info" || r.URL.Query().Get("series_id") != "50" {
			t.Error("wrong series API request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"episodes":{"2":[{"id":"202","episode_num":2,"title":"Second","container_extension":"mkv"}],"1":[{"id":101,"episode_num":1,"title":"First","container_extension":"mp4"}]}}`))
	}))
	defer server.Close()
	items, err := loadSeriesEpisodes(channel{Name: "Drama", SeriesID: "50", SeriesAccount: &xtreamAccount{Base: server.URL, Username: "u", Password: "p"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Group != "Drama · Season 1" || items[0].URL != server.URL+"/series/u/p/101.mp4" || items[1].URL != server.URL+"/series/u/p/202.mkv" {
		t.Fatalf("series collection IDs were mistaken for episodes: %+v", items)
	}
}
