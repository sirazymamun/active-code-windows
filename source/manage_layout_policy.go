package main

func manageClamp32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// managePopupSize keeps Manage visually aligned with the activation popup.
// PIN views stay compact; the group-list view may grow to fit more rows.
func managePopupSize(width, height int32, groups bool) (int32, int32) {
	w := manageClamp32(width-36, 270, 620)
	h := manageClamp32(height-28, 190, 224)
	if groups {
		h = manageClamp32(height-34, 250, 520)
	}
	return w, h
}
