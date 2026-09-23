// Package shell runs the commands typed in doted inside a pseudo-terminal and
// streams what they write back to the UI goroutine.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var ErrBusy = errors.New("a command is already running")

const (
	readBufferSize = 32 * 1024

	// After the command exits, a background job it started may still hold the
	// terminal open. Wait this long for the output to drain, then let go.
	drainGrace = 200 * time.Millisecond
)

// Event is either a chunk of terminal output or, when Done is set, the end of
// the command.
type Event struct {
	Data []byte

	Done     bool
	ExitCode int
	Status   string // why the command failed ("exit status 1", "signal: interrupt"); empty on success
	Killed   bool   // ended by Kill
}

// Runner executes one command at a time through the user's shell, attached
// to a PTY so programs see a real terminal. Its methods must be called from a
// single goroutine (the game loop); only the process plumbing runs in the
// background.
type Runner struct {
	shell  string
	dir    string
	events chan Event

	pty     *os.File
	cancel  context.CancelFunc
	running bool
}

func NewRunner(dir string) *Runner {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return &Runner{shell: sh, dir: dir, events: make(chan Event, 256)}
}

func (r *Runner) Dir() string { return r.dir }

func (r *Runner) Running() bool { return r.running }

// Chdir implements the `cd` builtin: a child process can't change our
// working directory, so the runner tracks it and applies it to each command.
func (r *Runner) Chdir(path string) error {
	home, _ := os.UserHomeDir()
	switch {
	case path == "" || path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	case !filepath.IsAbs(path):
		path = filepath.Join(r.dir, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: not a directory", path)
	}
	r.dir = filepath.Clean(path)
	return nil
}

// Start launches cmdline via `$SHELL -c` on a terminal of cols×rows cells.
// Output arrives through Drain.
func (r *Runner) Start(cmdline string, cols, rows int) error {
	if r.running {
		return ErrBusy
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, r.shell, "-c", cmdline)
	cmd.Dir = r.dir
	// Colors are rendered, but screen-addressing programs are not supported
	// yet, so keep pagers out of the way.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "CLICOLOR=1", "PAGER=cat", "GIT_PAGER=cat")

	pty, err := startPTY(cmd, cols, rows)
	if err != nil {
		cancel()
		return err
	}
	r.running = true
	r.cancel = cancel
	r.pty = pty

	go func() {
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			r.pump(pty)
		}()

		err := cmd.Wait()
		select {
		case <-readDone:
		case <-time.After(drainGrace):
		}
		pty.Close() // unblocks the reader if a background job kept the terminal open
		<-readDone

		killed := ctx.Err() != nil
		cancel()
		ev := Event{Done: true, ExitCode: cmd.ProcessState.ExitCode(), Killed: killed}
		if err != nil && !killed {
			ev.Status = err.Error()
		}
		r.events <- ev
	}()
	return nil
}

// Write sends input (keystrokes) to the running command's terminal.
func (r *Runner) Write(p []byte) error {
	if !r.running {
		return nil
	}
	_, err := r.pty.Write(p)
	return err
}

// Resize tells the running command the terminal is now cols×rows (SIGWINCH).
func (r *Runner) Resize(cols, rows int) error {
	if !r.running {
		return nil
	}
	return setSize(r.pty, cols, rows)
}

// Kill terminates the running command and everything it spawned.
func (r *Runner) Kill() {
	if r.cancel != nil {
		r.cancel()
	}
}

// Drain hands pending events to fn without blocking. Call it once per frame.
// It stops after a bounded number of events so a flood of output can't stall
// the frame.
func (r *Runner) Drain(fn func(Event)) {
	for range cap(r.events) {
		select {
		case ev := <-r.events:
			if ev.Done {
				r.running = false
				r.cancel = nil
				r.pty = nil
			}
			fn(ev)
		default:
			return
		}
	}
}

func (r *Runner) pump(f *os.File) {
	buf := make([]byte, readBufferSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			r.events <- Event{Data: bytes.Clone(buf[:n])}
		}
		if err != nil {
			return // EOF/EIO once the terminal is closed on the other side
		}
	}
}
