//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

type dataBlob struct {
	Len  uint32
	Data *byte
}

var crypt32 = syscall.NewLazyDLL("crypt32.dll")
var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var encrypt = crypt32.NewProc("CryptProtectData")
var decrypt = crypt32.NewProc("CryptUnprotectData")
var freeMem = kernel32.NewProc("LocalFree")

const cryptProtectUIForbidden = 0x1

func cryptBytes(v []byte, protect bool) ([]byte, error) {
	if len(v) == 0 {
		return nil, fmt.Errorf("empty value")
	}
	input := dataBlob{Len: uint32(len(v)), Data: &v[0]}
	var output dataBlob
	var proc *syscall.LazyProc
	if protect {
		proc = encrypt
	} else {
		proc = decrypt
	}
	result, _, callErr := proc.Call(uintptr(unsafe.Pointer(&input)), 0, 0, 0, 0, cryptProtectUIForbidden, uintptr(unsafe.Pointer(&output)))
	if result == 0 {
		return nil, fmt.Errorf("Windows DPAPI failed: %w", callErr)
	}
	defer freeMem.Call(uintptr(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, int(output.Len))...), nil
}
func protectBytes(v []byte) ([]byte, error)   { return cryptBytes(v, true) }
func unprotectBytes(v []byte) ([]byte, error) { return cryptBytes(v, false) }
