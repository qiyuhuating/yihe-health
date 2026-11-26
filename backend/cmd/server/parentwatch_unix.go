//go:build unix

package main

import "syscall"

// parentAlive 通过 signal 0 探测进程是否存在（Unix）。
func parentAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
