//go:build unix

package jobs

import (
	"strings"
	"testing"
	"time"

	"github.com/informeai/doted/internal/shell"
)

// pollUntil polls m until cond holds.
func pollUntil(t *testing.T, m *Manager, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m.Poll(time.Now(), func(*Job, shell.Event) {})
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func outputOf(j *Job) string {
	var lines []string
	for i := range j.Output.Len() {
		lines = append(lines, j.Output.At(i).Text())
	}
	return strings.Join(lines, "\n")
}

func TestJobsRunConcurrentlyAndKeepOutput(t *testing.T) {
	m := NewManager(1000)
	sess := shell.NewSession(t.TempDir())
	slow, err := m.Start(sess, "echo slow-start; read _; echo slow-end", 80, 24, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fast, err := m.Start(sess, "echo one; echo two; exit 2", 80, 24, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if slow.ID != 1 || fast.ID != 2 {
		t.Fatalf("ids = %d, %d", slow.ID, fast.ID)
	}

	pollUntil(t, m, func() bool { return !fast.Running() && strings.Contains(outputOf(slow), "slow-start") })
	if got := outputOf(fast); got != "one\ntwo\nexit status 2" {
		t.Fatalf("fast output = %q", got)
	}
	if fast.Status != "exit status 2" || fast.LastLine() != "exit status 2" || fast.Ended.IsZero() {
		t.Fatalf("fast = %+v", fast)
	}
	if !slow.Running() {
		t.Fatal("slow job should still be waiting for input")
	}

	slow.Write([]byte("\r"))
	pollUntil(t, m, func() bool { return !slow.Running() })
	if got := outputOf(slow); !strings.Contains(got, "slow-end") || slow.Status != "" {
		t.Fatalf("slow output = %q status %q", got, slow.Status)
	}
}

func TestListedJobs(t *testing.T) {
	m := NewManager(1000)
	sess := shell.NewSession(t.TempDir())
	a, _ := m.Start(sess, "sleep 30", 80, 24, time.Now())
	b, _ := m.Start(sess, "true", 80, 24, time.Now())
	defer m.KillAll()

	if len(m.Listed()) != 0 || m.Get(a.ID) != nil {
		t.Fatal("jobs are only listed once marked")
	}
	a.Listed, b.Listed = true, true
	pollUntil(t, m, func() bool { return !b.Running() })

	if n := m.RunningInBackground(); n != 1 {
		t.Fatalf("RunningInBackground = %d, want 1", n)
	}
	m.Remove(b)
	if l := m.Listed(); len(l) != 1 || l[0] != a || m.Get(b.ID) != nil {
		t.Fatalf("listed = %v", l)
	}

	a.Kill()
	pollUntil(t, m, func() bool { return !a.Running() })
	if !a.Killed || a.LastLine() != "killed" {
		t.Fatalf("a = %+v, last %q", a, a.LastLine())
	}
}
