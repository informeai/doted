package terminal

import (
	"slices"
	"testing"
	"time"
)

// run feeds chunks to a fresh parser inside Begin/End and returns the lines.
func run(chunks ...string) (*Parser, []Line) {
	sb := NewScrollback(0)
	p := NewParser(sb)
	p.Begin()
	for _, c := range chunks {
		p.Write([]byte(c), time.Now())
	}
	p.End()
	lines := make([]Line, sb.Len())
	for i := range lines {
		lines[i] = sb.At(i)
	}
	return p, lines
}

func texts(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text()
	}
	return out
}

func TestParserText(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{"crlf lines", []string{"one\r\ntwo\r\n"}, []string{"one", "two"}},
		{"no trailing newline", []string{"Password: "}, []string{"Password: "}},
		{"blank lines kept", []string{"a\r\n\r\nb\r\n"}, []string{"a", "", "b"}},
		{"carriage return overwrites", []string{"50%\r100%\r\n"}, []string{"100%"}},
		{"backspace overwrites", []string{"ab\bc\r\n"}, []string{"ac"}},
		{"tab stops", []string{"a\tb\r\n"}, []string{"a       b"}},
		{"erase to end of line", []string{"progress 10%\r\x1b[Kdone\r\n"}, []string{"done"}},
		{"cursor back", []string{"abc\x1b[2DX\r\n"}, []string{"aXc"}},
		{"osc title stripped", []string{"\x1b]0;title\x07hi\r\n"}, []string{"hi"}},
		{"charset select stripped", []string{"\x1b(Bhi\r\n"}, []string{"hi"}},
		{"escape split across chunks", []string{"\x1b[3", "1mred\x1b", "[0m\r\n"}, []string{"red"}},
		{"utf-8 split across chunks", []string{"ol\xc3", "\xa1\r\n"}, []string{"olá"}},
		{"clear screen", []string{"old\r\n\x1b[H\x1b[2J\x1b[3Jnew\r\n"}, []string{"new"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, lines := run(tt.chunks...)
			if got := texts(lines); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParserSGR(t *testing.T) {
	_, lines := run("\x1b[1;31mA\x1b[0m\x1b[38;5;208mB\x1b[48;2;1;2;3;4mC\x1b[24;39;49mD\x1b[mE\r\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	want := []Style{
		{FG: indexed(1), Attrs: Bold},
		{FG: indexed(208)},
		{FG: indexed(208), BG: Color{Kind: RGBColor, R: 1, G: 2, B: 3}, Attrs: Underline},
		{},
		{},
	}
	for i, c := range lines[0].Cells {
		if c.Style != want[i] {
			t.Errorf("cell %d (%q) style = %+v, want %+v", i, c.Rune, c.Style, want[i])
		}
	}
}

func TestParserBrightColors(t *testing.T) {
	_, lines := run("\x1b[92mA\x1b[103mB\r\n")
	cells := lines[0].Cells
	if cells[0].Style.FG != indexed(10) || cells[1].Style.BG != indexed(11) {
		t.Fatalf("styles = %+v %+v", cells[0].Style, cells[1].Style)
	}
}

func TestParserLiveLineAndCursor(t *testing.T) {
	sb := NewScrollback(0)
	p := NewParser(sb)
	p.Begin()
	if sb.Len() != 1 {
		t.Fatalf("Begin should open a live line, have %d lines", sb.Len())
	}
	p.Write([]byte("Continue? [y/N] "), time.Now())
	if p.Col() != len("Continue? [y/N] ") {
		t.Fatalf("Col() = %d", p.Col())
	}
	p.Write([]byte("y\r\n"), time.Now())
	if sb.Len() != 2 || p.Col() != 0 {
		t.Fatalf("after newline: %d lines, col %d", sb.Len(), p.Col())
	}
	p.End()
	if sb.Len() != 1 {
		t.Fatalf("End should drop the empty live line, have %d lines", sb.Len())
	}
}

func TestParserAltScreen(t *testing.T) {
	sb := NewScrollback(0)
	p := NewParser(sb)
	p.Begin()
	p.Write([]byte("\x1b[?1049h"), time.Now())
	if !p.AltScreen {
		t.Fatal("AltScreen not set")
	}
	p.Write([]byte("\x1b[?1049l"), time.Now())
	if p.AltScreen {
		t.Fatal("AltScreen not cleared")
	}
}

func TestParserSkipsTheAlternateScreen(t *testing.T) {
	// A full-screen program: its screen goes to the grid emulator, not to the
	// line history, which keeps what was printed around it.
	_, lines := run("before\r\n", "\x1b[?1049h\x1b[2J\x1b[H~\r\n~ vim screen\x1b[31mred", "\x1b[?1049l", "after\r\n")
	if got := texts(lines); !slices.Equal(got, []string{"before", "after"}) {
		t.Fatalf("lines = %q, want before and after only", got)
	}
}
