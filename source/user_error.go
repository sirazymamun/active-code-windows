package main

const playlistFailureMessage = "Your subscription may have expired. Please contact your subscription vendor."

// Only locally defined public messages may reach the UI. Never forward raw
// transport errors, provider responses, URLs, credentials, or filesystem details.
func userError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch message {
	case "A recording is already running. Stop it before changing channel.":
		return message
	case "Activation changed during verification; retry":
		return message
	case "Activation required":
		return message
	case "Activation unavailable. Please check your code or contact your subscription vendor.":
		return message
	case "Cannot create embedded VLC video surface":
		return message
	case "Cannot register VLC native library directory":
		return message
	case "Choose a valid live channel before recording":
		return message
	case "Encrypted HLS recording is not supported":
		return message
	case "Enter exactly 10 digits":
		return message
	case "Fragmented MP4 HLS recording is not supported":
		return message
	case "HLS manifest exceeds 1MB":
		return message
	case "LibVLC initialization failed; codec plugin may be missing":
		return message
	case "LibVLC video player could not be created":
		return message
	case "Native Windows player requires Windows 10/11 x64":
		return message
	case "No HLS variant available":
		return message
	case "Not a valid HLS playlist":
		return message
	case "Official VLC 3.0.24 archive SHA-256 mismatch: refusing unverified code":
		return message
	case "Only MPEG-TS HLS segments can be saved":
		return message
	case "Playlist download failed":
		return message
	case "Playlist exceeds 256 MB limit":
		return message
	case "Playlist server returned an empty reply or website instead of playlist data":
		return message
	case "Recording size limit reached (4 GB)":
		return message
	case "Recording supports Live TV only. Movies and Series should be played normally.":
		return message
	case "Screen resolution too small for Windows player":
		return message
	case "Series API connection failed":
		return message
	case "Series API timed out":
		return message
	case "Series account metadata unavailable":
		return message
	case "Service is busy. Please try again shortly.":
		return message
	case "Service temporarily unavailable. Please try again later.":
		return message
	case "Subscription changed or expired while loading playlist":
		return message
	case "This series contains no playable episodes":
		return message
	case "Too many HLS segments in one recording":
		return message
	case "Too many nested HLS playlists":
		return message
	case "Unable to connect. Please check your internet connection and try again.":
		return message
	case "Unrecognized HLS media manifest":
		return message
	case "Unsafe VLC archive entry":
		return message
	case "Unsupported device request":
		return message
	case "VLC 3.0.24 stable runtime was not embedded: refusing incomplete build":
		return message
	case "VLC archive expanded size exceeds limit":
		return message
	case "VLC archive missing plugins":
		return message
	case "VLC archive path traversal":
		return message
	case "VLC archive size mismatch":
		return message
	case "Xtream API returned no playable entries":
		return message
	default:
		return "Unable to complete the request. Please try again or contact your subscription vendor."
	}
}
