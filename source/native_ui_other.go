//go:build !windows

package main

import "errors"

func launchNativeUI(a *application) error {
	return errors.New("Native Windows player requires Windows 10/11 x64")
}
func showError(text string) { println(text) }
