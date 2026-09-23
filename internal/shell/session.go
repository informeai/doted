// Package shell runs the commands typed in doted inside pseudo-terminals and
// streams what they write back to the UI goroutine.
package shell

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

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

// Session holds what every command shares: the shell, extra environment and
// the working directory. It must be used from a single goroutine.
type Session struct {
	shell string
	env   []string // extra variables from the config, applied last
	dir   string
}

func NewSession(dir string) *Session {
	s := &Session{dir: dir}
	s.Configure("", nil)
	return s
}

// Configure sets the shell used for the next commands (empty means $SHELL,
// then /bin/sh) and extra environment variables, which take precedence over
// doted's own.
func (s *Session) Configure(shell string, env map[string]string) {
	s.shell = cmp.Or(shell, os.Getenv("SHELL"), "/bin/sh")
	s.env = s.env[:0]
	for _, k := range slices.Sorted(maps.Keys(env)) {
		s.env = append(s.env, k+"="+env[k])
	}
}

func (s *Session) Dir() string { return s.dir }

// Chdir implements the `cd` builtin: a child process can't change our
// working directory, so the session tracks it and applies it to each command.
func (s *Session) Chdir(path string) error {
	home, _ := os.UserHomeDir()
	switch {
	case path == "" || path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	case !filepath.IsAbs(path):
		path = filepath.Join(s.dir, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: not a directory", path)
	}
	s.dir = filepath.Clean(path)
	return nil
}

// Start launches cmdline via `$SHELL -c` on its own terminal of cols×rows
// cells. Any number of processes can run at once.
func (s *Session) Start(cmdline string, cols, rows int) (*Process, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.shell, "-c", cmdline)
	cmd.Dir = s.dir
	// Colors are rendered, but screen-addressing programs are not supported
	// yet, so keep pagers out of the way.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "CLICOLOR=1", "PAGER=cat", "GIT_PAGER=cat")
	cmd.Env = append(cmd.Env, s.env...) // later entries win

	pty, err := startPTY(cmd, cols, rows)
	if err != nil {
		cancel()
		return nil, err
	}
	p := &Process{events: make(chan Event, 256), pty: pty, cancel: cancel, running: true}

	go func() {
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			p.pump()
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
		p.events <- ev
	}()
	return p, nil
}

// Process is one running command attached to a PTY. Its methods must be
// called from a single goroutine (the game loop); only the process plumbing
// runs in the background.
type Process struct {
	events  chan Event
	pty     *os.File
	cancel  context.CancelFunc
	running bool
}

// Running reports whether the command has not finished yet (as far as the
// events drained so far tell).
func (p *Process) Running() bool { return p.running }

// Write sends input (keystrokes) to the command's terminal.
func (p *Process) Write(b []byte) error {
	if !p.running {
		return nil
	}
	_, err := p.pty.Write(b)
	return err
}

// Resize tells the command its terminal is now cols×rows (SIGWINCH).
func (p *Process) Resize(cols, rows int) error {
	if !p.running {
		return nil
	}
	return setSize(p.pty, cols, rows)
}

// Kill terminates the command and everything it spawned.
func (p *Process) Kill() { p.cancel() }

// Drain hands pending events to fn without blocking. Call it once per frame.
// It stops after a bounded number of events so a flood of output can't stall
// the frame.
func (p *Process) Drain(fn func(Event)) {
	for range cap(p.events) {
		select {
		case ev := <-p.events:
			if ev.Done {
				p.running = false
			}
			fn(ev)
		default:
			return
		}
	}
}

func (p *Process) pump() {
	buf := make([]byte, readBufferSize)
	for {
		n, err := p.pty.Read(buf)
		if n > 0 {
			p.events <- Event{Data: bytes.Clone(buf[:n])}
		}
		if err != nil {
			return // EOF/EIO once the terminal is closed on the other side
		}
	}
}
