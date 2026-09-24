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

func TestParserProgress(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{"docker pull redraws its lines",
			[]string{"a: Waiting\r\nb: Waiting\r\n", "\x1b[2A\x1b[2Ka: Pulling\r\n\x1b[2Kb: Waiting\r\n", "\x1b[2A\x1b[2Ka: Done\r\n\x1b[2Kb: Done\r\n"},
			[]string{"a: Done", "b: Done"}},
		{"a spinner on the line above",
			[]string{"⠋ installing\r\n", "\x1b[1A\x1b[2K⠙ installing\r\n", "\x1b[1A\x1b[2K✓ installed\r\n"},
			[]string{"✓ installed"}},
		{"cursor to the start of a line above",
			[]string{"one\r\ntwo\r\n\x1b[2FONE\r\n"},
			[]string{"ONE", "two"}},
		{"erase below the cursor",
			[]string{"1\r\n2\r\n3\r\n\x1b[2A\x1b[J"},
			[]string{"1"}},
		{"never above the command's first line",
			[]string{"x\r\n\x1b[5Ay"},
			[]string{"y"}},
		{"save and restore the cursor",
			[]string{"a\x1b7\r\nb\r\nc\x1b8Z"},
			[]string{"aZ", "b", "c"}},
		{"CSI s and u",
			[]string{"a\x1b[s\r\nb\x1b[uZ"},
			[]string{"aZ", "b"}},
		{"line feed in the middle moves down",
			[]string{"1\r\n2\r\n3\x1b[2A\r\nX"},
			[]string{"1", "X", "3"}},
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

func TestParserCursorPositionOnScreen(t *testing.T) {
	sb := NewScrollback(0)
	p := NewParser(sb)
	p.Rows = 3
	p.Begin()
	// The screen is the last three lines: row 1 is "2".
	p.Write([]byte("1\r\n2\r\n3\r\n4\x1b[1;1HX\x1b[3;2HY"), time.Now())
	p.End()
	var got []string
	for i := range sb.Len() {
		got = append(got, sb.At(i).Text())
	}
	if !slices.Equal(got, []string{"1", "X", "3", "4Y"}) {
		t.Fatalf("got %q", got)
	}
}

func TestParserKeepsEarlierLines(t *testing.T) {
	sb := NewScrollback(0)
	sb.Append(Command, "$ docker pull", time.Now())
	p := NewParser(sb)
	p.Begin()
	p.Write([]byte("layer\r\n\x1b[9A\x1b[2Kover"), time.Now())
	p.End()
	if sb.At(0).Text() != "$ docker pull" || sb.At(1).Text() != "over" {
		t.Fatalf("got %q, %q", sb.At(0).Text(), sb.At(1).Text())
	}
}

func TestParserRedrawKeepsTheEntrance(t *testing.T) {
	sb := NewScrollback(0)
	p := NewParser(sb)
	p.Begin()
	t0 := time.Now()
	p.Write([]byte("10%"), t0)
	p.Write([]byte("\r\x1b[2K50%"), t0.Add(time.Second))
	if !sb.At(0).At.Equal(t0) || sb.At(0).Text() != "50%" {
		t.Fatalf("a redrawn line should keep when it showed up: %v, %q", sb.At(0).At, sb.At(0).Text())
	}
}

func TestParserWideCharacters(t *testing.T) {
	_, lines := run("日本|🚀|x")
	l := lines[0]
	// 日 and 本 and 🚀 take two columns each.
	if len(l.Cells) != 9 || l.Cells[1].Rune != WideTail || l.Cells[6].Rune != WideTail || l.Text() != "日本|🚀|x" {
		t.Fatalf("%d cells, text %q, runes %q", len(l.Cells), l.Text(), string(l.Runes()))
	}
	// Writing over half of a wide character blanks the other half.
	_, lines = run("日本\rab")
	if got := lines[0].Text(); got != "ab本" {
		t.Fatalf("over the first: %q", got)
	}
	_, lines = run("日本\x1b[2Gx")
	if got := lines[0].Text(); got != " x本" {
		t.Fatalf("over the tail: %q", got)
	}
	// Combining marks and joiners take no column.
	_, lines = run("é|")
	if len(lines[0].Cells) != 2 {
		t.Fatalf("combining mark took a cell: %q", lines[0].Text())
	}
}

func TestStringOf(t *testing.T) {
	if got := StringOf([]rune{'日', WideTail, 'a'}); got != "日a" {
		t.Fatalf("got %q", got)
	}
}
