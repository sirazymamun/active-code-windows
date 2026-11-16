package main

import (
	"regexp"
	"strings"
)

type nativeAudioTrack struct {
	ID       int32
	Label    string
	Selected bool
}

var stereoAudioLabel = regexp.MustCompile(`(?i)(stereo|2\.0|\b2\s*(?:ch|channels?)\b|2-channel)`)
var surroundAudioLabel = regexp.MustCompile(`(?i)(5\.1|surround|\b6\s*(?:ch|channels?)\b|6-channel)`)

func audioPriority(label string) int {
	if stereoAudioLabel.MatchString(label) {
		return 0
	}
	if surroundAudioLabel.MatchString(label) {
		return 1
	}
	return 2
}
func preferredAudioTrack(tracks []nativeAudioTrack, current int32) int32 {
	for _, track := range tracks {
		if track.ID >= 0 && audioPriority(strings.TrimSpace(track.Label)) == 0 {
			return track.ID
		}
	}
	for _, track := range tracks {
		if track.ID >= 0 && audioPriority(track.Label) == 1 {
			return track.ID
		}
	}
	return current
}
