//go:build !windows

package main

func prepareInstalledLaunch() (bool, error) { return false, nil }
