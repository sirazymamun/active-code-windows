package main

func streamInfoPanelMetrics(screenWidth, screenHeight int32, lineCount int) (width, height, row, top int32) {
	font := max(int32(20), min(int32(42), screenHeight/38))
	row = font + 2
	top = row + 20
	width = min(screenWidth-24, max(int32(600), screenWidth*86/100))
	height = min(screenHeight-24, top+int32(lineCount)*row+16)
	return
}
