package main

import "testing"

func TestManagePINLayoutIsCompactLikeActivation(t *testing.T) {
	w, h := managePopupSize(1280, 720, false)
	if w != 620 {
		t.Fatalf("PIN width=%d, want 620", w)
	}
	if h > 224 {
		t.Fatalf("PIN height=%d, should remain compact", h)
	}
}

func TestManageGroupLayoutCanGrowWithoutChangingPINLayout(t *testing.T) {
	_, pinH := managePopupSize(1920, 1080, false)
	_, groupH := managePopupSize(1920, 1080, true)
	if groupH <= pinH {
		t.Fatalf("group height=%d should exceed PIN height=%d", groupH, pinH)
	}
	if groupH > 520 {
		t.Fatalf("group height=%d exceeds compact cap", groupH)
	}
}
