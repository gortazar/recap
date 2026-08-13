//go:build unix

package render

import (
	"os"
	"syscall"
	"unsafe"
)

// TerminalWidth returns the width of the terminal behind f, and whether there is one.
//
// When there is not — a pipe, a file, a CI log — the caller uses a fixed width, so that a
// redirect produces a stable file and CI output does not depend on the runner.
func TerminalWidth(f *os.File) (int, bool) {
	var ws struct {
		Row, Col, Xpixel, Ypixel uint16
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 || ws.Col == 0 {
		return 0, false
	}
	return int(ws.Col), true
}
