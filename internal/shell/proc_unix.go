//go:build unix

package shell

import (
	"cmp"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// startPTY starts cmd as the leader of a new session whose controlling
// terminal is a fresh PTY, and returns the PTY's master side.
func startPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	// Killing the session's process group ends the shell and everything it
	// spawned, not only the shell itself.
	cmd.Cancel = func() error {
		return unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
	}
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return pollable(master)
}

// pollable swaps the blocking file creack/pty returns for a non-blocking
// duplicate managed by Go's poller, so that Close interrupts a pending Read.
// Don't call Fd on the result: it would switch the file back to blocking.
func pollable(f *os.File) (*os.File, error) {
	fd, err := unix.Dup(int(f.Fd()))
	f.Close()
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

// setSize is pty.Setsize without the call to Fd (see pollable).
func setSize(f *os.File, cols, rows int) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	err = conn.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
	})
	return cmp.Or(err, ioctlErr)
}

// lineMode reports whether the terminal on f's other side reads input a
// line at a time (canonical mode, with the terminal's own line editing)
// rather than a key at a time, as full-screen programs and watchers do.
func lineMode(f *os.File) (canonical, ok bool) {
	conn, err := f.SyscallConn()
	if err != nil {
		return false, false
	}
	var t *unix.Termios
	if conn.Control(func(fd uintptr) { t, err = unix.IoctlGetTermios(int(fd), getTermios) }) != nil || err != nil {
		return false, false
	}
	return t.Lflag&unix.ICANON != 0, true
}
