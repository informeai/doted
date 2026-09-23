package app

import (
	"strings"
	"testing"
	"time"
)

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
	if g.editor.Text() != "git  --short status" {
		t.Fatalf("paste: line %q", g.editor.Text())
	}

	// Pasting over a selection replaces it.
	g.editor.Home(false)
	for range len("git") {
		g.editor.Right(true)
	}
	g.paste()
	if g.editor.Text() != "status  --short status" {
		t.Fatalf("paste over a selection: line %q", g.editor.Text())
	}
}

func TestKillAndYank(t *testing.T) {
	g := newTestGame(t)
	g.paste()
	if g.flashText != "clipboard is empty" || !g.editor.Empty() {
		t.Fatalf("paste with nothing copied: flash %q, line %q", g.flashText, g.editor.Text())
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
	g.paste() // Ctrl+Y
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
