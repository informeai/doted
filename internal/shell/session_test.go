//go:build unix

package shell

import (
	"os"
	"path/filepath"
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

func TestImportLoginEnvironment(t *testing.T) {
	dir := t.TempDir()
	// A "login shell" whose profile prints noise and exports a variable.
	fake := filepath.Join(dir, "fakesh")
	script := "#!/bin/sh\necho 'welcome banner'\nexport FROM_PROFILE='yes=really'\nexec /bin/sh \"$@\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	sess := NewSession(dir)
	sess.Configure(fake, nil)
	if err := sess.ImportLoginEnvironment(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	r, err := sess.Start(`echo "[$FROM_PROFILE]"`, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := wait(t, r, 5*time.Second); !strings.Contains(out, "[yes=really]") {
		t.Fatalf("output %q: profile variable not imported", out)
	}

	sess.Configure("/nonexistent/shell", nil)
	if err := sess.ImportLoginEnvironment(time.Second); err == nil {
		t.Fatal("expected an error for a missing shell")
	}
}

func TestFindCommand(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.Mkdir(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "mytool"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(bin, "notexec"), []byte("data"), 0o644)
	os.WriteFile(filepath.Join(dir, "build.sh"), []byte("#!/bin/sh\n"), 0o755)

	sess := NewSession(dir)
	sess.Configure("/bin/sh", map[string]string{"PATH": bin})
	for name, want := range map[string]bool{
		"mytool":        true,  // in the configured PATH
		"notexec":       false, // there, but not executable
		"sh":            false, // not in this PATH
		"nope":          false,
		"./build.sh":    true, // relative to the session's directory
		"./missing.sh":  false,
		"bin":           false, // a directory isn't a command
		"/bin/sh":       true,
		"":              false,
		"bin/mytool":    true,
		"./bin/notexec": false,
	} {
		if got := sess.FindCommand(name); got != want {
			t.Errorf("FindCommand(%q) = %v, want %v", name, got, want)
		}
	}

	// Without the override, the real PATH is used.
	sess.Configure("/bin/sh", nil)
	if !sess.FindCommand("sh") {
		t.Error("sh should be found in the system PATH")
	}
}

func TestIsShellBuiltin(t *testing.T) {
	for _, name := range []string{"echo", "export", "source", "if", "["} {
		if !IsShellBuiltin(name) {
			t.Errorf("%q should be a shell builtin", name)
		}
	}
	if IsShellBuiltin("git") {
		t.Error("git is not a shell builtin")
	}
}

func TestCommands(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for dir, names := range map[string][]string{a: {"alpha", "beta"}, b: {"beta", "gamma"}} {
		for _, n := range names {
			os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755)
		}
	}
	os.WriteFile(filepath.Join(a, "data.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(a, "subdir"), 0o755)

	sess := NewSession(t.TempDir())
	sess.Configure("/bin/sh", map[string]string{"PATH": a + string(os.PathListSeparator) + b + string(os.PathListSeparator) + "relative"})
	got := map[string]int{}
	for _, n := range sess.Commands() {
		got[n]++
	}
	if len(got) != 3 || got["alpha"] != 1 || got["beta"] != 1 || got["gamma"] != 1 {
		t.Fatalf("Commands() = %v, want alpha, beta and gamma once each", got)
	}
}
