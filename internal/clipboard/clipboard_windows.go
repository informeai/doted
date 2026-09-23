//go:build windows

package clipboard

import (
	"context"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Win32 clipboard API, called directly: no cgo and no helper process.

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	getClipboardData = user32.NewProc("GetClipboardData")
	setClipboardData = user32.NewProc("SetClipboardData")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalFree       = kernel32.NewProc("GlobalFree")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
)

// open opens the clipboard, retrying while another program holds it. The
// clipboard belongs to the thread that opened it until it's closed, so the
// caller stays on this thread.
func open(ctx context.Context) error {
	for {
		if r, _, _ := openClipboard.Call(0); r != 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the clipboard is busy: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Write puts text on the system clipboard.
func Write(ctx context.Context, text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := open(ctx); err != nil {
		return err
	}
	defer closeClipboard.Call()

	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := uintptr(len(utf16)) * unsafe.Sizeof(utf16[0])
	h, _, err := globalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	p, _, err := globalLock.Call(h)
	if p == 0 {
		globalFree.Call(h)
		return fmt.Errorf("GlobalLock: %w", err)
	}
	copy(unsafe.Slice((*uint16)(ptr(p)), len(utf16)), utf16)
	globalUnlock.Call(h)

	emptyClipboard.Call()
	if r, _, err := setClipboardData.Call(cfUnicodeText, h); r == 0 {
		globalFree.Call(h) // still ours: the clipboard didn't take it
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	return nil
}

// Read returns the text on the system clipboard.
func Read(ctx context.Context) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := open(ctx); err != nil {
		return "", err
	}
	defer closeClipboard.Call()

	h, _, _ := getClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil // no text on the clipboard
	}
	p, _, err := globalLock.Call(h)
	if p == 0 {
		return "", fmt.Errorf("GlobalLock: %w", err)
	}
	defer globalUnlock.Call(h)
	return windows.UTF16PtrToString((*uint16)(ptr(p))), nil
}

// ptr turns the address GlobalLock returns into a pointer. The memory is
// the system's, not Go's, so the garbage collector never moves it.
func ptr(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}
