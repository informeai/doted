// Package jobs tracks the commands doted runs. Each job keeps its complete
// output in its own scrollback, so it can be moved to the background and
// brought back later without losing anything it printed meanwhile.
package jobs

import (
	"slices"
	"strings"
	"time"

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
}

func (j *Job) Running() bool { return j.proc.Running() }

// Col is the job's cursor column on its newest output line.
func (j *Job) Col() int { return j.parser.Col() }

// AltScreen reports whether the job asked for a full-screen display.
func (j *Job) AltScreen() bool { return j.parser.AltScreen }

func (j *Job) Write(b []byte) error { return j.proc.Write(b) }

func (j *Job) Kill() { j.proc.Kill() }

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
		ID:      m.nextID,
		Command: cmdline,
		Started: now,
		Output:  out,
		proc:    proc,
		parser:  terminal.NewParser(out),
	}
	j.parser.Begin()
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
			} else {
				j.parser.End()
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

// Remove forgets a finished job.
func (m *Manager) Remove(j *Job) {
	m.jobs = slices.DeleteFunc(m.jobs, func(x *Job) bool { return x == j })
}

// Resize gives every running job a terminal of cols×rows cells.
func (m *Manager) Resize(cols, rows int) {
	for _, j := range m.jobs {
		j.proc.Resize(cols, rows)
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
