//go:build windows

package main

import _ "embed"

//go:embed runtime/vlc-3.0.24-win64.zip
var bundledVLC []byte
