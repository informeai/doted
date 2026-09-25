package terminal

import (
	"slices"
	"testing"
	"time"
)

func TestWrap(t *testing.T) {
	tests := []struct {
		in   string
		cols int
		want []string
	}{
		{"", 4, []string{""}},
		{"abc", 4, []string{"abc"}},
		{"abcd", 4, []string{"abcd"}},
		{"abcdefghi", 4, []string{"abcd", "efgh", "i"}},
		{"ãéíõú", 2, []string{"ãé", "íõ", "ú"}},
	}
	for _, tt := range tests {
		var got []string
		for _, row := range Wrap([]rune(tt.in), tt.cols) {
			got = append(got, string(row))
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("Wrap(%q, %d) = %q, want %q", tt.in, tt.cols, got, tt.want)
		}
		if n := RowCount(len([]rune(tt.in)), tt.cols); n != len(tt.want) {
			t.Errorf("RowCount(%q, %d) = %d, want %d", tt.in, tt.cols, n, len(tt.want))
		}
	}
}

func TestScrollbackSplitsAndTrims(t *testing.T) {
	s := NewScrollback(4)
	s.Append(Output, "a\nb", time.Now())
	if s.Len() != 2 || s.At(1).Text() != "b" {
		t.Fatalf("got %d lines, last %q", s.Len(), s.At(s.Len()-1).Text())
	}
	for range 10 {
		s.Append(Output, "x", time.Now())
	}
	if s.Len() > 5 {
		t.Fatalf("scrollback grew to %d lines past its limit", s.Len())
	}
}

func TestEditorEditing(t *testing.T) {
	var e Editor
	e.Insert([]rune("echo world")...)
	for range 5 {
		e.Left(false)
	}
	e.Insert([]rune("hello ")...)
	if got := e.Text(); got != "echo hello world" {
		t.Fatalf("Text() = %q", got)
	}
	e.End(false)
	e.DeleteWordBackward()
	if got := e.Text(); got != "echo hello " {
		t.Fatalf("after Ctrl+W: %q", got)
	}
	e.Home(false)
	e.Delete()
	e.End(false)
	e.Backspace()
	if got := e.Text(); got != "cho hello" {
		t.Fatalf("after Delete/Backspace: %q", got)
	}
}

func TestEditorHistory(t *testing.T) {
	var e Editor
	for _, cmd := range []string{"one", "two", "two"} {
		e.Insert([]rune(cmd)...)
		e.Submit()
	}
	e.Insert([]rune("draft")...)

	e.HistoryPrev()
	if e.Text() != "two" {
		t.Fatalf("prev = %q, want two (duplicates collapsed)", e.Text())
	}
	e.HistoryPrev()
	e.HistoryPrev() // already at the oldest entry
	if e.Text() != "one" {
		t.Fatalf("prev = %q, want one", e.Text())
	}
	e.HistoryNext()
	e.HistoryNext()
	if e.Text() != "draft" || e.Cursor() != len("draft") {
		t.Fatalf("draft not restored: %q cursor %d", e.Text(), e.Cursor())
	}
}

func TestEditorSelection(t *testing.T) {
	var e Editor
	e.Insert([]rune("git status")...)
	if _, _, ok := e.Selection(); ok {
		t.Fatal("selection before any Shift")
	}

	// Shift+Left x6 selects "status".
	for range 6 {
		e.Left(true)
	}
	if s, end, ok := e.Selection(); !ok || s != 4 || end != 10 || e.SelectedText() != "status" {
		t.Fatalf("selection = %d..%d %v %q", s, end, ok, e.SelectedText())
	}

	// Typing replaces it.
	e.Insert([]rune("log")...)
	if e.Text() != "git log" || e.Cursor() != 7 {
		t.Fatalf("after typing over the selection: %q cursor %d", e.Text(), e.Cursor())
	}
	if _, _, ok := e.Selection(); ok {
		t.Fatal("selection survived typing")
	}

	// Shift+Home, then Backspace deletes it all.
	e.Home(true)
	if e.SelectedText() != "git log" {
		t.Fatalf("Shift+Home selected %q", e.SelectedText())
	}
	e.Backspace()
	if !e.Empty() {
		t.Fatalf("Backspace left %q", e.Text())
	}
}

func TestEditorSelectionCollapsesAndShrinks(t *testing.T) {
	var e Editor
	e.Insert([]rune("abcdef")...)
	e.Home(false)
	e.Right(true)
	e.Right(true)
	e.Right(true) // "abc" selected, cursor at 3
	e.Left(true)  // back to "ab"
	if e.SelectedText() != "ab" {
		t.Fatalf("selection = %q, want ab", e.SelectedText())
	}
	e.Left(true)
	e.Left(true) // cursor back on the anchor: nothing selected
	if _, _, ok := e.Selection(); ok {
		t.Fatal("selection should be empty back at the anchor")
	}

	e.End(true)   // select "abcdef" from 0
	e.Left(false) // collapses to the start
	if _, _, ok := e.Selection(); ok || e.Cursor() != 0 {
		t.Fatalf("Left without Shift should collapse to the start, cursor %d", e.Cursor())
	}
	e.End(true)
	e.Home(false)
	e.Right(true)
	e.Right(true)
	e.Right(false) // collapses to the end
	if _, _, ok := e.Selection(); ok || e.Cursor() != 2 {
		t.Fatalf("Right without Shift should collapse to the end, cursor %d", e.Cursor())
	}

	// Deleting keys act on the selection.
	e.End(false)
	e.Left(true)
	e.Left(true)
	e.DeleteWordBackward()
	if e.Text() != "abcd" {
		t.Fatalf("Ctrl+W with a selection: %q", e.Text())
	}
	e.End(false)
	e.Home(true)
	e.Delete()
	if !e.Empty() {
		t.Fatalf("Delete with a selection left %q", e.Text())
	}
}

func TestEditorSelectionClearedByLineChanges(t *testing.T) {
	var e Editor
	e.Insert([]rune("one")...)
	e.Submit()
	e.Insert([]rune("two")...)
	e.Home(true)
	e.HistoryPrev()
	if _, _, ok := e.Selection(); ok {
		t.Fatal("history kept the selection")
	}
	e.End(false)
	e.Home(true)
	e.Reset()
	if _, _, ok := e.Selection(); ok {
		t.Fatal("Reset kept the selection")
	}
}

func TestEditorCutAndKillReturnText(t *testing.T) {
	var e Editor
	e.Insert([]rune("git commit -m msg")...)
	if got := e.Cut(); got != "" {
		t.Fatalf("Cut without a selection = %q", got)
	}
	for range len("msg") {
		e.Left(true)
	}
	if got := e.Cut(); got != "msg" || e.Text() != "git commit -m " {
		t.Fatalf("Cut = %q, line %q", got, e.Text())
	}
	if got := e.DeleteWordBackward(); got != "-m " || e.Text() != "git commit " {
		t.Fatalf("Ctrl+W removed %q, line %q", got, e.Text())
	}
	if got := e.KillToStart(); got != "git commit " || !e.Empty() {
		t.Fatalf("Ctrl+U removed %q, line %q", got, e.Text())
	}
}

func TestEditorHistoryLimitAndIgnoreSpace(t *testing.T) {
	var e Editor
	e.SetHistory([]string{"a", "b", "c"}, 2)
	if h := e.History(); len(h) != 2 || h[0] != "b" {
		t.Fatalf("SetHistory kept %q, want the newest 2", h)
	}
	e.Insert([]rune(" secret")...)
	e.Submit()
	e.Insert([]rune("d")...)
	e.Submit()
	if h := e.History(); len(h) != 2 || h[0] != "c" || h[1] != "d" {
		t.Fatalf("history = %q, want c and d (limit 2, space-prefixed line left out)", h)
	}
}

func TestScrollbackSeqSurvivesTrimmingAndClearing(t *testing.T) {
	s := NewScrollback(4)
	s.Append(Output, "a\nb\nc", time.Now())
	seqB := s.Seq(1)
	for range 5 { // past limit+limit/4: the oldest lines go
		s.Append(Output, "x", time.Now())
	}
	if _, ok := s.Index(seqB); ok {
		t.Fatal("b was trimmed, but its seq still resolves")
	}
	s.Append(Output, "y", time.Now())
	seqY := s.Seq(s.Len() - 1)
	s.Append(Output, "z", time.Now()) // shifts nothing, but check y still resolves to y
	if i, ok := s.Index(seqY); !ok || s.At(i).Text() != "y" {
		t.Fatalf("seq of y resolves to %d, %v", i, ok)
	}
	last := s.Seq(s.Len() - 1)
	if i, ok := s.Index(last); !ok || i != s.Len()-1 {
		t.Fatalf("Index(Seq(last)) = %d, %v", i, ok)
	}
	s.Clear()
	if _, ok := s.Index(last); ok {
		t.Fatal("a cleared line should be gone")
	}
	s.Append(Output, "new", time.Now())
	if s.Seq(0) <= last {
		t.Fatalf("a line after Clear got seq %d, not after %d", s.Seq(0), last)
	}
}

func TestScrollbackCountsClears(t *testing.T) {
	sb := NewScrollback(0)
	sb.Clear() // nothing to clear
	if sb.Clears() != 0 {
		t.Fatal("clearing an empty scrollback shouldn't count")
	}
	sb.Append(Output, "x", time.Now())
	sb.Clear()
	if sb.Clears() != 1 {
		t.Fatalf("clears = %d", sb.Clears())
	}
}
