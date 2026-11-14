package ui

import (
	"os"
	"syscall"
	"unsafe"
)

// disableEcho turns terminal echo off on stdin and returns the function that
// puts it back. It does nothing when stdin is not a terminal.
func disableEcho() func() {
	fd := os.Stdin.Fd()
	var t syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		return func() {}
	}
	saved := t
	t.Lflag &^= syscall.ECHO
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		return func() {}
	}
	return func() {
		syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&saved)))
	}
}
