//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// parentAlive 通过 OpenProcess + GetExitCodeProcess 探测进程是否存活（Windows，无新依赖）。
func parentAlive(pid int) bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	openProcess := kernel32.NewProc("OpenProcess")
	getExitCode := kernel32.NewProc("GetExitCodeProcess")
	closeHandle := kernel32.NewProc("CloseHandle")
	const processQueryLimitedInformation = 0x1000
	const stillActive = 259
	h, _, _ := openProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer closeHandle.Call(h)
	var code uint32
	r, _, _ := getExitCode.Call(h, uintptr(unsafe.Pointer(&code)))
	if r == 0 {
		return false
	}
	return code == stillActive
}
