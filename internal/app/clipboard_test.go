package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClipboard stands in for the system clipboard in tests.
type fakeClipboard struct {
	mu   sync.Mutex
	text string
	err  error
}

func (f *fakeClipboard) Read(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text, f.err
}

func (f *fakeClipboard) Write(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.text = text
	return nil
}

func (f *fakeClipboard) get() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text
}

func fake(g *Game) *fakeClipboard { return g.system.(*fakeClipboard) }

// awaitClipboard waits for the next background clipboard result and handles
// it, as Update would.
func awaitClipboard(t *testing.T, g *Game) {
	t.Helper()
	select {
	case ev := <-g.clipboardEvents:
		g.clipboardEvents <- ev
		g.handleClipboardEvents()
	case <-time.After(2 * time.Second):
		t.Fatal("clipboard call didn't finish")
	}
}

// settle waits for the background clipboard calls to finish and handles
// their results, as Update would.
func settle(t *testing.T, g *Game, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		g.handleClipboardEvents()
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("clipboard call didn't finish")
}

func TestCopyCutPaste(t *testing.T) {
	g := newTestGame(t)
	g.editor.Insert([]rune("git status --short")...)

	g.copySelection()
	if g.clipboard != "" || !strings.Contains(g.flashText, "nothing selected") {
		t.Fatalf("copy without a selection: clipboard %q, flash %q", g.clipboard, g.flashText)
	}

	for range len(" --short") {
		g.editor.Left(false)
	}
	for range len("status") {
		g.editor.Left(true)
	}
	g.copySelection()
	if g.clipboard != "status" || g.editor.Text() != "git status --short" || g.flashText != "copied 6 characters" {
		t.Fatalf("copy: clipboard %q, line %q, flash %q", g.clipboard, g.editor.Text(), g.flashText)
	}
	settle(t, g, func() bool { return fake(g).get() == "status" }) // on the system clipboard too
	if hint, _ := g.statusHint(time.Now()); hint != "copied 6 characters" {
		t.Fatalf("status line shows %q, want the copy message", hint)
	}
	if hint, _ := g.statusHint(time.Now().Add(flashDuration)); hint == "copied 6 characters" {
		t.Fatal("the copy message should go away")
	}

	g.cutSelection()
	if g.clipboard != "status" || g.editor.Text() != "git  --short" {
		t.Fatalf("cut: clipboard %q, line %q", g.clipboard, g.editor.Text())
	}

	g.editor.End(false)
	g.editor.Insert(' ')
	g.paste()
	settle(t, g, func() bool { return g.editor.Text() != "git  --short " })
	if g.editor.Text() != "git  --short status" {
		t.Fatalf("paste: line %q", g.editor.Text())
	}
}

func TestPasteFromOtherApps(t *testing.T) {
	g := newTestGame(t)
	fake(g).text = "echo from\nthe browser" // copied elsewhere
	g.paste()
	settle(t, g, func() bool { return !g.editor.Empty() })
	if g.editor.Text() != "echo from the browser" {
		t.Fatalf("pasted %q, want one line", g.editor.Text())
	}
}

func TestSystemClipboardFailures(t *testing.T) {
	g := newTestGame(t)
	fake(g).err = errors.New("xclip: can't open display")
	g.editor.Insert([]rune("abc")...)
	g.editor.Left(true)
	g.copySelection()
	settle(t, g, func() bool { return strings.HasPrefix(g.flashText, "copied inside doted only") })
	if g.clipboard != "c" {
		t.Fatalf("doted's own clipboard = %q", g.clipboard)
	}

	// Pasting falls back to doted's own clipboard.
	g.editor.End(false)
	g.paste()
	settle(t, g, func() bool { return g.editor.Text() == "abcc" })
	if !strings.Contains(g.flashText, "pasted doted's own") {
		t.Fatalf("flash = %q", g.flashText)
	}
}

func TestSystemClipboardCanBeTurnedOff(t *testing.T) {
	g := newTestGame(t)
	g.cfg.Clipboard.System = false
	fake(g).text = "from outside"
	g.editor.Insert([]rune("xy")...)
	g.editor.Left(true)
	g.copySelection()
	g.editor.End(false)
	g.paste() // no background call: doted's own clipboard, right away
	if g.editor.Text() != "xyy" || fake(g).get() != "from outside" {
		t.Fatalf("line %q, system clipboard %q", g.editor.Text(), fake(g).get())
	}
}

func TestKillAndYank(t *testing.T) {
	g := newTestGame(t)
	g.yank()
	if g.flashText != "clipboard is empty" || !g.editor.Empty() {
		t.Fatalf("Ctrl+Y with nothing kept: flash %q, line %q", g.flashText, g.editor.Text())
	}

	g.editor.Insert([]rune("echo hello world")...)
	g.kill(g.editor.DeleteWordBackward()) // Ctrl+W
	if g.clipboard != "world" {
		t.Fatalf("Ctrl+W kept %q", g.clipboard)
	}
	g.kill(g.editor.KillToStart()) // Ctrl+U
	if g.clipboard != "echo hello " || !g.editor.Empty() {
		t.Fatalf("Ctrl+U kept %q, line %q", g.clipboard, g.editor.Text())
	}
	if fake(g).get() != "" {
		t.Fatal("Ctrl+W and Ctrl+U must not touch the system clipboard")
	}
	g.yank() // Ctrl+Y
	if g.editor.Text() != "echo hello " {
		t.Fatalf("Ctrl+Y typed %q", g.editor.Text())
	}

	// Deleting nothing keeps the previous clipboard.
	g.editor.Reset()
	g.kill(g.editor.DeleteWordBackward())
	if g.clipboard != "echo hello " {
		t.Fatalf("an empty Ctrl+W replaced the clipboard with %q", g.clipboard)
	}
}

func TestSingleLine(t *testing.T) {
	if got := singleLine("a\nb\r\nc\td\x07e"); got != "a b  c de" {
		t.Fatalf("singleLine = %q", got)
	}
}
