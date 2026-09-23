// Package jobs tracks the commands doted runs. Each job keeps its complete
// output in its own scrollback, so it can be moved to the background and
// brought back later without losing anything it printed meanwhile.
package jobs

import (
	"io"
	"slices"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/informeai/doted/internal/shell"
	"github.com/informeai/doted/internal/terminal"
)

type Job struct {
	ID      int
	Command string
	Started time.Time
	Ended   time.Time

	// Status is why the job failed ("exit status 1"); empty while running or
	// on success.
	Status string
	Killed bool

	// Listed jobs appear in the jobs list: they are, or have been, in the
	// background.
	Listed bool

	Output *terminal.Scrollback

	proc   *shell.Process
	parser *terminal.Parser

	// screen emulates the whole terminal grid. Full-screen programs (vim,
	// less, htop) are drawn from it; it also encodes keys the way the
	// program asked for and answers its queries, such as where the cursor is.
	screen        *vt.Emulator
	cursorVisible bool
}

func (j *Job) Running() bool { return j.proc.Running() }

// Col is the job's cursor column on its newest output line.
func (j *Job) Col() int { return j.parser.Col() }

// Write sends raw input to the job. It goes through the screen's input like
// keys and pastes do, so everything typed reaches the program in order.
func (j *Job) Write(b []byte) error {
	if !j.Running() {
		return nil
	}
	_, err := j.screen.InputPipe().Write(b)
	return err
}

// closeScreen ends the goroutine forwarding the screen's input. It closes
// the input pipe itself rather than calling the emulator's Close, which sets
// a flag that goroutine reads without a lock.
func (j *Job) closeScreen() {
	if pw, ok := j.screen.InputPipe().(io.Closer); ok {
		pw.Close()
	}
}

// FullScreen reports whether the job shows a full-screen program: it
// switched to the alternate screen.
func (j *Job) FullScreen() bool { return j.screen.IsAltScreen() }

// Screen is the job's terminal grid, for drawing a full-screen program.
func (j *Job) Screen() *vt.Emulator { return j.screen }

// CursorVisible reports whether the program shows its cursor.
func (j *Job) CursorVisible() bool { return j.cursorVisible }

// SendKey sends a key to the job, encoded as its terminal modes ask.
func (j *Job) SendKey(k uv.KeyEvent) {
	if j.Running() {
		j.screen.SendKey(k)
	}
}

// Paste types text into the job as a paste, bracketed if the program asked
// for it (so an editor doesn't auto-indent or run what's pasted).
func (j *Job) Paste(s string) {
	if j.Running() {
		j.screen.Paste(s)
	}
}

// SendText types text into the job.
func (j *Job) SendText(s string) {
	if j.Running() {
		j.screen.SendText(s)
	}
}

// State is where the job's shell left its state; see shell.Session.Adopt.
func (j *Job) State() string { return j.proc.State() }

func (j *Job) Kill() { j.proc.Kill() }

// Interrupt sends Ctrl+C to the job's terminal, which interrupts the program
// the way pressing it would.
func (j *Job) Interrupt() { j.Write([]byte{0x03}) }

// LineMode reports whether the job reads its input a line at a time rather
// than a key at a time; ok is false when that can't be told.
func (j *Job) LineMode() (canonical, ok bool) { return j.proc.LineMode() }

// Elapsed is how long the job ran, or has been running so far.
func (j *Job) Elapsed(now time.Time) time.Duration {
	if j.Ended.IsZero() {
		return now.Sub(j.Started)
	}
	return j.Ended.Sub(j.Started)
}

// LastLine is the newest non-blank line of output, for previews.
func (j *Job) LastLine() string {
	for i := j.Output.Len() - 1; i >= 0; i-- {
		if s := strings.TrimSpace(j.Output.At(i).Text()); s != "" {
			return s
		}
	}
	return ""
}

// Tail is the newest n non-blank lines of output, oldest first, for
// previews.
func (j *Job) Tail(n int) []string {
	var out []string
	for i := j.Output.Len() - 1; i >= 0 && len(out) < n; i-- {
		if s := strings.TrimRight(j.Output.At(i).Text(), " "); strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	slices.Reverse(out)
	return out
}

// Manager owns every job, running or finished, oldest first.
type Manager struct {
	jobs   []*Job
	nextID int
	lines  int // scrollback limit for new jobs
}

func NewManager(scrollbackLines int) *Manager {
	return &Manager{nextID: 1, lines: scrollbackLines}
}

// SetScrollback changes the output limit for jobs started from now on.
func (m *Manager) SetScrollback(lines int) { m.lines = lines }

// Start runs cmdline in s on a terminal of cols×rows cells.
func (m *Manager) Start(s *shell.Session, cmdline string, cols, rows int, now time.Time) (*Job, error) {
	proc, err := s.Start(cmdline, cols, rows)
	if err != nil {
		return nil, err
	}
	out := terminal.NewScrollback(m.lines)
	j := &Job{
		ID:            m.nextID,
		Command:       cmdline,
		Started:       now,
		Output:        out,
		proc:          proc,
		parser:        terminal.NewParser(out),
		screen:        vt.NewEmulator(cols, rows),
		cursorVisible: true,
	}
	j.parser.Begin()
	j.screen.SetScrollbackSize(0) // the line history is Output
	j.screen.SetCallbacks(vt.Callbacks{CursorVisibility: func(v bool) { j.cursorVisible = v }})
	// What the emulator writes as terminal input (encoded keys, answers to
	// queries) goes to the program. It ends when the screen is closed.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := j.screen.Read(buf)
			if n > 0 {
				proc.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	m.nextID++
	m.jobs = append(m.jobs, j)
	return j, nil
}

// Poll records every job's pending output in its scrollback, then hands the
// event to fn. Call it once per frame. fn may remove jobs.
func (m *Manager) Poll(now time.Time, fn func(*Job, shell.Event)) {
	for _, j := range slices.Clone(m.jobs) {
		j.proc.Drain(func(ev shell.Event) {
			if !ev.Done {
				j.parser.Write(ev.Data, now)
				j.screen.Write(ev.Data)
			} else {
				j.parser.End()
				j.closeScreen()
				j.Ended, j.Status, j.Killed = now, ev.Status, ev.Killed
				switch {
				case ev.Killed:
					j.Output.Append(terminal.System, "killed", now)
				case ev.Status != "":
					j.Output.Append(terminal.System, ev.Status, now)
				}
			}
			fn(j, ev)
		})
	}
}

// Listed returns the jobs shown in the jobs list, oldest first.
func (m *Manager) Listed() []*Job {
	var out []*Job
	for _, j := range m.jobs {
		if j.Listed {
			out = append(out, j)
		}
	}
	return out
}

// RunningInBackground counts listed jobs that are still running.
func (m *Manager) RunningInBackground() int {
	n := 0
	for _, j := range m.jobs {
		if j.Listed && j.Running() {
			n++
		}
	}
	return n
}

// Get returns the listed job with the given id, or nil.
func (m *Manager) Get(id int) *Job {
	for _, j := range m.jobs {
		if j.ID == id && j.Listed {
			return j
		}
	}
	return nil
}

// Replace puts next, a job just started, in old's place in the list, and
// forgets old.
func (m *Manager) Replace(old, next *Job) {
	m.jobs = slices.DeleteFunc(m.jobs, func(x *Job) bool { return x == next })
	if i := slices.Index(m.jobs, old); i >= 0 {
		m.jobs[i] = next
		return
	}
	m.jobs = append(m.jobs, next)
}

// Remove forgets a finished job.
func (m *Manager) Remove(j *Job) {
	m.jobs = slices.DeleteFunc(m.jobs, func(x *Job) bool { return x == j })
}

// Resize gives every running job a terminal of cols×rows cells.
func (m *Manager) Resize(cols, rows int) {
	for _, j := range m.jobs {
		j.proc.Resize(cols, rows)
		if j.Running() {
			j.screen.Resize(cols, rows)
		}
	}
}

// KillAll terminates every running job.
func (m *Manager) KillAll() {
	for _, j := range m.jobs {
		if j.Running() {
			j.Kill()
		}
	}
}
