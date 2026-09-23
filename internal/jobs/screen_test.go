//go:build unix

package jobs

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/informeai/doted/internal/shell"
)

func startJob(t *testing.T, cmd string) (*Manager, *Job) {
	t.Helper()
	m := NewManager(1000)
	sess := shell.NewSession(t.TempDir())
	sess.Configure("/bin/sh", nil)
	j, err := m.Start(sess, cmd, 40, 10, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.KillAll() })
	return m, j
}

func screenRow(j *Job, y int) string {
	var b strings.Builder
	for x := range j.Screen().Width() {
		c := j.Screen().CellAt(x, y)
		if c == nil || c.Content == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(c.Content)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func TestFullScreenProgram(t *testing.T) {
	m, j := startJob(t, `echo before; printf '\033[?1049h\033[2;3Hhello'; sleep 0.4; printf '\033[?1049l'; echo after`)
	pollUntil(t, m, func() bool { return j.FullScreen() && strings.Contains(screenRow(j, 1), "hello") })
	if got := screenRow(j, 1); got != "  hello" {
		t.Fatalf("row 2 of the screen = %q, want the text at column 3", got)
	}
	pollUntil(t, m, func() bool { return !j.Running() })
	if j.FullScreen() {
		t.Fatal("still full screen after the program left it")
	}
	if got := outputOf(j); got != "before\nafter" {
		t.Fatalf("line history = %q: the full-screen part must stay out of it", got)
	}
}

func TestKeysFollowTheProgramsModes(t *testing.T) {
	// The program asks for application cursor keys, then reads a raw key.
	m, j := startJob(t, `printf '\033[?1h'; stty raw -echo; echo ready; k=$(dd bs=1 count=3 2>/dev/null); stty sane; printf '%s' "$k" | od -An -tx1`)
	pollUntil(t, m, func() bool { return strings.Contains(outputOf(j), "ready") })
	j.SendKey(uv.KeyPressEvent{Code: uv.KeyUp})
	pollUntil(t, m, func() bool { return !j.Running() })
	// od pads its columns differently per system: compare the bytes alone.
	if got := strings.Join(strings.Fields(outputOf(j)), " "); !strings.Contains(got, "1b 4f 41") {
		t.Fatalf("Up in application mode arrived as %q, want ESC O A (1b 4f 41)", got)
	}
}

func TestQueriesAreAnswered(t *testing.T) {
	// Asking where the cursor is: without an answer, programs hang.
	m, j := startJob(t, `stty raw -echo; printf '\033[6n'; r=$(dd bs=1 count=6 2>/dev/null); stty sane; printf '%s' "$r" | od -An -c`)
	pollUntil(t, m, func() bool { return !j.Running() })
	if got := outputOf(j); !strings.Contains(got, "R") {
		t.Fatalf("no cursor position report: %q", got)
	}
}

func TestLess(t *testing.T) {
	if _, err := exec.LookPath("less"); err != nil {
		t.Skip("less is not installed")
	}
	m, j := startJob(t, "seq 1 300 | LESS= less")
	pollUntil(t, m, func() bool { return j.FullScreen() && screenRow(j, 0) == "1" })
	if got := screenRow(j, 8); got != "9" {
		t.Fatalf("row 9 = %q, want 9", got)
	}
	j.SendKey(uv.KeyPressEvent{Code: uv.KeySpace}) // next page
	pollUntil(t, m, func() bool { return screenRow(j, 0) != "1" })
	j.SendText("q")
	pollUntil(t, m, func() bool { return !j.Running() })
	if strings.Contains(outputOf(j), "150") {
		t.Fatalf("less's screen leaked into the line history: %q", outputOf(j))
	}
}

func TestResizeReachesTheScreen(t *testing.T) {
	m, j := startJob(t, "sleep 5")
	m.Resize(60, 20)
	if j.Screen().Width() != 60 || j.Screen().Height() != 20 {
		t.Fatalf("screen is %dx%d after resize, want 60x20", j.Screen().Width(), j.Screen().Height())
	}
}
