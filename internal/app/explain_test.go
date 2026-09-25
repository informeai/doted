//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// explainGame is a game whose claude is a script that prints its
// arguments, the context it got and a suggestion.
func explainGame(t *testing.T) *Game {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"args: $*\"\ncat\necho 'The file is missing.'\necho '▸ echo fixed'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LANG", "pt_BR.UTF-8")
	return newTestGame(t)
}

func runAttached(t *testing.T, g *Game, cmd string) {
	t.Helper()
	run(g, cmd)
	tickUntil(t, g, func() bool { return g.attached == nil })
}

func TestExplainTheLastFailure(t *testing.T) {
	g := explainGame(t)
	runAttached(t, g, "echo about to fail; ls /no-such-dir-doted")
	runAttached(t, g, "echo fine")
	runAttached(t, g, "explain")
	text := mainText(g)
	for _, want := range []string{
		"ls /no-such-dir-dot… · exit ",
		"asking claude…",
		"--allowedTools Read,Grep,Glob --model haiku",
		"Answer in pt-BR.",
		"command: echo about to fail; ls /no-such-dir-doted",
		"about to fail",
		"▸ echo fixed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "command: echo fine") {
		t.Fatal("explained the command that worked")
	}

	// The suggestion goes to the prompt on a click.
	var seq int
	for i := range g.scrollback.Len() {
		if _, ok := g.suggestionAt(g.scrollback, i); ok {
			seq = g.scrollback.Seq(i)
		}
	}
	if seq == 0 {
		t.Fatal("no suggestion found")
	}
	g.runBlockAction(blockAction{kind: actSuggest, seq: seq})
	if got := g.editor.Text(); got != "echo fixed" {
		t.Fatalf("prompt = %q", got)
	}

	// An answer isn't explained itself.
	g.editor.Reset()
	runAttached(t, g, "explain")
	if n := strings.Count(mainText(g), "command: echo about to fail"); n != 2 {
		t.Fatalf("second explain asked about something else (%d)", n)
	}
}

func TestExplainAJob(t *testing.T) {
	g := explainGame(t)
	run(g, "echo boom; exit 3 &")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool { return !j.Running() })
	runAttached(t, g, "explain #1")
	text := mainText(g)
	if !strings.Contains(text, "job #1 · echo boom; exit 3 · exit 3") || !strings.Contains(text, "result: exit 3") || !strings.Contains(text, "\nboom") {
		t.Fatalf("job explained:\n%s", text)
	}
}

func TestExplainErrors(t *testing.T) {
	g := explainGame(t)
	run(g, "explain")
	if !strings.Contains(lastLine(g), "no command has failed here") {
		t.Fatalf("nothing failed: %q", lastLine(g))
	}
	run(g, "explain #9")
	if !strings.Contains(lastLine(g), "no job #9") {
		t.Fatalf("no job: %q", lastLine(g))
	}
	g.cfg.Explain.Tool = "nope"
	run(g, "explain")
	if !strings.Contains(lastLine(g), `tool = "nope" isn't known`) {
		t.Fatalf("unknown tool: %q", lastLine(g))
	}
}

func TestExplainLanguage(t *testing.T) {
	g := explainGame(t)
	if got := g.explainLanguage(); got != "pt-BR" {
		t.Fatalf("from LANG: %q", got)
	}
	g.cfg.Explain.Language = "English"
	if got := g.explainLanguage(); got != "English" {
		t.Fatalf("configured: %q", got)
	}
}

func TestExplainPicksTheTool(t *testing.T) {
	g := explainGame(t)
	g.cfg.Explain.Tool = "nope" // the argument wins over the config
	run(g, "echo boom; exit 3 &")
	j := g.jobs.Listed()[0]
	tickUntil(t, g, func() bool { return !j.Running() })
	runAttached(t, g, "explain #1 claude")
	if text := mainText(g); !strings.Contains(text, "exit 3 · asking claude…") || !strings.Contains(text, "result: exit 3") {
		t.Fatalf("explain #1 claude:\n%s", text)
	}
	runAttached(t, g, "explain claude 1") // either order
	if n := strings.Count(mainText(g), "result: exit 3"); n != 2 {
		t.Fatalf("explain claude 1 ran %d times", n)
	}
}

func TestParseExplainArgs(t *testing.T) {
	for _, c := range []struct{ arg, job, tool, err string }{
		{"", "", "", ""},
		{"#6", "#6", "", ""},
		{"6 claude", "6", "claude", ""},
		{"claude", "", "claude", ""},
		{"#6 ollama", "", "", `"ollama" isn't a job or a tool doted knows (claude, cursor)`},
		{"cursor 6", "6", "cursor", ""},
		{"#6 #7", "", "", "one job at a time"},
		{"claude claude", "", "", "usage: explain [#n] [tool]"},
	} {
		job, tool, err := parseExplainArgs(c.arg)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: error %v, want %q", c.arg, err, c.err)
			}
			continue
		}
		if err != nil || job != c.job || tool != c.tool {
			t.Errorf("%q = %q, %q, %v", c.arg, job, tool, err)
		}
	}
}

func TestExplainWithCursor(t *testing.T) {
	g := explainGame(t)
	bin := t.TempDir()
	// agent -p gets the prompt and the context as one argument, then the
	// options.
	script := "#!/bin/sh\necho \"opts: $1 $3 $4 $5 $6 n=$#\"\nprintf '%s\\n' \"$2\"\necho '▸ echo from cursor'\n"
	if err := os.WriteFile(filepath.Join(bin, "agent"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	g.session.Configure("", map[string]string{"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH")})
	runAttached(t, g, "ls /no-such-dir-doted")
	runAttached(t, g, "explain cursor")
	text := mainText(g)
	for _, want := range []string{
		"asking cursor…",
		"opts: -p --mode ask --output-format text n=6",
		"The context describes a command that failed",
		"Answer in pt-BR.",
		"\nContext:\ncommand: ls /no-such-dir-doted",
		"▸ echo from cursor",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--model") {
		t.Fatal("claude's model went to cursor")
	}
}

func TestExplainToolMissing(t *testing.T) {
	g := explainGame(t)
	runAttached(t, g, "ls /no-such-dir-doted")
	run(g, "explain cursor")
	if got := lastLine(g); !strings.Contains(got, "cursor isn't installed (agent or cursor-agent): curl https://cursor.com/install") {
		t.Fatalf("missing tool: %q", got)
	}
}
