package terminal

import (
	"strings"
	"time"
)

// Kind tells the renderer how a line should be styled when its cells use the
// default foreground.
type Kind int

const (
	Output  Kind = iota // what a command wrote to its terminal
	Error               // errors reported by doted (e.g. a failed cd)
	Command             // echo of what the user typed
	System              // messages from doted itself
)

// Line is a single logical line of the scrollback. At records when it got its
// first content so the renderer can animate its entrance.
type Line struct {
	Cells []Cell
	Kind  Kind
	At    time.Time
}

func (l Line) Text() string {
	var b strings.Builder
	for _, c := range l.Cells {
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// Scrollback holds the output history shown above the input, oldest first.
type Scrollback struct {
	lines   []Line
	limit   int
	dropped int // lines trimmed or cleared so far; see Seq
}

func NewScrollback(limit int) *Scrollback {
	return &Scrollback{limit: limit}
}

// Append adds plain text, splitting it on newlines into separate lines.
func (s *Scrollback) Append(kind Kind, text string, at time.Time) {
	for part := range strings.SplitSeq(text, "\n") {
		s.push(Line{Cells: cellsFromString(part), Kind: kind, At: at})
	}
}

func (s *Scrollback) push(l Line) {
	s.lines = append(s.lines, l)
	// Trim in batches so we don't shift the whole slice on every append.
	if s.limit > 0 && len(s.lines) > s.limit+s.limit/4 {
		n := len(s.lines) - s.limit
		s.lines = append(s.lines[:0], s.lines[n:]...)
		s.dropped += n
	}
}

// last returns the newest line; only valid until the next push.
func (s *Scrollback) last() *Line { return &s.lines[len(s.lines)-1] }

func (s *Scrollback) pop() { s.lines = s.lines[:len(s.lines)-1] }

// SetLimit changes how many lines are kept; extra old lines go on the next append.
func (s *Scrollback) SetLimit(n int) { s.limit = n }

func (s *Scrollback) Len() int { return len(s.lines) }

func (s *Scrollback) At(i int) Line { return s.lines[i] }

func (s *Scrollback) Clear() {
	s.dropped += len(s.lines)
	s.lines = s.lines[:0]
}

// Seq is a number for the line at index i that stays the same while older
// lines are trimmed or cleared, unlike its index. Selections hold on to it.
func (s *Scrollback) Seq(i int) int { return s.dropped + i }

// Index returns the current index of the line numbered seq, and whether it
// is still there.
func (s *Scrollback) Index(seq int) (int, bool) {
	i := seq - s.dropped
	return i, i >= 0 && i < len(s.lines)
}

// Rows is the number of visual rows the scrollback takes when wrapped to cols.
func (s *Scrollback) Rows(cols int) int {
	n := 0
	for _, l := range s.lines {
		n += RowCount(len(l.Cells), cols)
	}
	return n
}
