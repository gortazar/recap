//go:build !unix

package render

import "os"

// TerminalWidth has no answer on a platform recap does not support; the caller falls back to
// a fixed width.
func TerminalWidth(*os.File) (int, bool) { return 0, false }
