//go:build !windows

package main

// Non-Windows test implementation; release executable uses Windows DPAPI encryption.
func protectBytes(v []byte) ([]byte, error)   { return append([]byte(nil), v...), nil }
func unprotectBytes(v []byte) ([]byte, error) { return append([]byte(nil), v...), nil }
