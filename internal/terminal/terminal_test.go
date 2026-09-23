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
		e.Left()
	}
	e.Insert([]rune("hello ")...)
	if got := e.Text(); got != "echo hello world" {
		t.Fatalf("Text() = %q", got)
	}
	e.End()
	e.DeleteWordBackward()
	if got := e.Text(); got != "echo hello " {
		t.Fatalf("after Ctrl+W: %q", got)
	}
	e.Home()
	e.Delete()
	e.End()
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
