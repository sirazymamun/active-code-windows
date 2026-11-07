package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Recording must get the full startup/decoder retry budget. The previous
// eight-second zero-byte check could abort the output before VLC had started.
const recordStartupBudget = 20 * time.Second

func recordingHasTimedOut(started, now time.Time, bytes int64) bool {
	return bytes == 0 && !started.IsZero() && now.Sub(started) >= recordStartupBudget
}
func nativeRecordingOptions(file string) []string {
	// VLC's option grammar uses quoted UTF-8 paths; normalize Windows separators
	// without relying on the host OS used by source/tests.
	file = strings.ReplaceAll(file, `\`, "/")
	file = strings.ReplaceAll(file, `"`, `\"`)
	return []string{
		fmt.Sprintf(`:sout=#duplicate{dst=display,dst=std{access=file{append,overwrite},mux=ts,dst="%s"}}`, file),
		":sout-file-overwrite", ":sout-file-append", ":sout-mux-caching=200",
	}
}

// A missing/empty file means no output yet; successful zero-byte stats are
// treated the same as a file the native output has not created yet.

func recordingFileSize(info os.FileInfo, err error) int64 {
	if err != nil || info == nil {
		return 0
	}
	return info.Size()
}
