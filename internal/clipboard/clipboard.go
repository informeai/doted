// Package clipboard reads and writes the system clipboard without cgo: with
// the system's own tools on macOS and Linux, and its API on Windows.
package clipboard

import "errors"

// ErrUnavailable means no way to reach the system clipboard was found, such
// as on Linux without wl-clipboard, xclip or xsel.
var ErrUnavailable = errors.New("no system clipboard tool found (install wl-clipboard, xclip or xsel)")
