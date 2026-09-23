//go:build unix

package shell

import (
	"strings"
	"testing"
	"time"
)

// wait drains events until the command finishes and returns everything it
// wrote to the terminal.
func wait(t *testing.T, r *Process, timeout time.Duration) (output string, done Event) {
	t.Helper()
	var out strings.Builder
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		finished := false
		r.Drain(func(ev Event) {
			if ev.Done {
				done, finished = ev, true
				return
			}
			out.Write(ev.Data)
		})
		if finished {
			return out.String(), done
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("command did not finish in %s; output so far: %q", timeout, out.String())
	return
}

// waitFor drains output until it contains want.
func waitFor(t *testing.T, r *Process, want string) {
	t.Helper()
	var out strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.Drain(func(ev Event) { out.Write(ev.Data) })
		if strings.Contains(out.String(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never saw %q; output: %q", want, out.String())
}

func TestProcessRunsInATerminal(t *testing.T) {
	sess := NewSession(t.TempDir())
	r, err := sess.Start("test -t 0 && test -t 1 && echo is-a-tty; stty size; echo err >&2; printf 'no newline'; exit 3", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	// Processes are independent: a second one runs alongside the first.
	other, err := sess.Start("echo concurrent", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := wait(t, other, 5*time.Second); !strings.Contains(out, "concurrent") {
		t.Fatalf("second process printed %q", out)
	}

	out, done := wait(t, r, 5*time.Second)
	for _, want := range []string{"is-a-tty\r\n", "30 100\r\n", "err\r\n", "no newline"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q is missing %q", out, want)
		}
	}
	if done.ExitCode != 3 || done.Status != "exit status 3" || done.Killed {
		t.Fatalf("done = %+v", done)
	}
	if r.Running() {
		t.Fatal("still running after Done")
	}
}

func TestProcessForwardsInput(t *testing.T) {
	sess := NewSession(t.TempDir())
	r, err := sess.Start(`printf 'name? '; read name; echo "hello $name"`, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, r, "name? ")
	if err := r.Write([]byte("doted\r")); err != nil {
		t.Fatal(err)
	}
	out, done := wait(t, r, 5*time.Second)
	if !strings.Contains(out, "hello doted") || done.Status != "" {
		t.Fatalf("output %q, done %+v", out, done)
	}
}

func TestProcessCtrlCInterrupts(t *testing.T) {
	sess := NewSession(t.TempDir())
	r, err := sess.Start("echo ready; sleep 30", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, r, "ready")
	if err := r.Write([]byte{0x03}); err != nil { // what Ctrl+C sends
		t.Fatal(err)
	}
	if _, done := wait(t, r, 5*time.Second); done.Status == "" || done.Killed {
		t.Fatalf("done = %+v, want a failure from SIGINT", done)
	}
}

func TestProcessResize(t *testing.T) {
	sess := NewSession(t.TempDir())
	r, err := sess.Start(`echo ready; read _; stty size`, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, r, "ready")
	if err := r.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	r.Write([]byte("\r"))
	if out, _ := wait(t, r, 5*time.Second); !strings.Contains(out, "40 120") {
		t.Fatalf("output %q, want new size 40 120", out)
	}
}

func TestProcessBackgroundJobDoesNotHang(t *testing.T) {
	sess := NewSession(t.TempDir())
	// nohup keeps the job alive past SIGHUP, so it holds the terminal open.
	r, err := sess.Start("nohup sleep 30 >/dev/null 2>&1 & echo started", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, done := wait(t, r, 3*time.Second)
	if !strings.Contains(out, "started") || done.Status != "" {
		t.Fatalf("output %q, done %+v", out, done)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %s to finish", elapsed)
	}
}

func TestProcessKillEndsProcessGroup(t *testing.T) {
	sess := NewSession(t.TempDir())
	r, err := sess.Start("trap '' INT; (sleep 30; echo late) & sleep 30", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	r.Kill()
	if _, done := wait(t, r, 5*time.Second); !done.Killed {
		t.Fatalf("done = %+v, want Killed", done)
	}
}

func TestChdir(t *testing.T) {
	dir := t.TempDir()
	sess := NewSession("/")
	if err := sess.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := sess.Chdir("does-not-exist"); err == nil {
		t.Fatal("expected error for missing directory")
	}
	r, err := sess.Start("pwd -P", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := wait(t, r, 5*time.Second); strings.TrimSpace(out) == "" {
		t.Fatal("pwd printed nothing")
	}
}

func TestConfigureShellAndEnv(t *testing.T) {
	sess := NewSession(t.TempDir())
	sess.Configure("/bin/sh", map[string]string{"DOTED_TEST": "from-config", "PAGER": "less"})
	r, err := sess.Start(`echo "$DOTED_TEST $PAGER"`, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := wait(t, r, 5*time.Second); !strings.Contains(out, "from-config less") {
		t.Fatalf("output %q: config env should be set and override doted's defaults", out)
	}
}
