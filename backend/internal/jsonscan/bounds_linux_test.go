package jsonscan

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func TestSafeAtGuardPage(t *testing.T) {
	page := os.Getpagesize()
	memory, err := syscall.Mmap(-1, 0, 2*page, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(memory)
	for i := 0; i < page; i++ {
		memory[i] = 'a'
	}
	if err := syscall.Mprotect(memory[page:], syscall.PROT_NONE); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 129; n++ {
		s := unsafe.String(&memory[page-n], n)
		if !Safe(s) {
			t.Fatalf("valid string ending at guard, length %d", n)
		}
		memory[page-1] = '"'
		if Safe(s) {
			t.Fatalf("missed last-byte escape, length %d", n)
		}
		memory[page-1] = 'a'
	}
}
