package main

import "testing"

func TestPreferredAudioUsesLabelsRatherThanTrackPosition(t *testing.T) {
	tracks := []nativeAudioTrack{{ID: 7, Label: "English 5.1"}, {ID: 42, Label: "Bangla Stereo"}}
	if id := preferredAudioTrack(tracks, 7); id != 42 {
		t.Fatal("explicit stereo track was not preferred")
	}
	unknown := []nativeAudioTrack{{ID: 1, Label: "Track 2"}, {ID: 2, Label: "English"}}
	if id := preferredAudioTrack(unknown, 2); id != 2 {
		t.Fatal("unknown track position was mistaken for stereo")
	}
	if audioPriority("English 2 channels") != 0 || audioPriority("English 6ch") != 1 {
		t.Fatal("audio label parsing failed")
	}
}
