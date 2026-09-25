package explain

import (
	"os"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"key AKIAIOSFODNN7EXAMPLE here":                  "key ‹redacted› here",
		"token ghp_abcdefghijklmnopqrstuvwxyz0123":       "token ‹redacted›",
		"Authorization: Bearer abc.def-ghi_jkl":          "Authorization: Bearer ‹redacted›",
		"DB_PASSWORD=hunter22 npm start":                 "DB_PASSWORD=‹redacted› npm start",
		"GITHUB_TOKEN: abc":                              "GITHUB_TOKEN: ‹redacted›",
		"used 1200 tokens":                               "used 1200 tokens",
		"password=hunter22 other":                        "password=‹redacted› other",
		`"api_key": "abc123xyz"`:                         `"api_key": "‹redacted›"`,
		"postgres://user:s3cret@localhost:5432/db":       "postgres://user:‹redacted›@localhost:5432/db",
		"OPENAI sk-proj-abcdefghijklmnopqrstuv failed":   "OPENAI ‹redacted› failed",
		"src/cart.ts:42:7 - error TS2322: Type 'string'": "src/cart.ts:42:7 - error TS2322: Type 'string'",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContext(t *testing.T) {
	f := Failure{Command: "npm run dev", Result: "exit 1", Dir: "~/app", Branch: "main", System: "macOS, zsh", Output: []string{"Error: listen EADDRINUSE :5173", "token=abc123"}}
	got := f.Context()
	for _, want := range []string{"command: npm run dev\n", "result: exit 1\n", "git branch: main\n", "EADDRINUSE", "token=‹redacted›"} {
		if !strings.Contains(got, want) {
			t.Errorf("context lacks %q:\n%s", want, got)
		}
	}
}

func TestCommandLine(t *testing.T) {
	line, err := CommandLine(Tools["claude"], "claude", Failure{Command: "x", Result: "exit 1"}, "", "pt-BR")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "claude -p '") || !strings.Contains(line, "--allowedTools 'Read,Grep,Glob' --model 'haiku' < '") || !strings.Contains(line, "Answer in pt-BR.") {
		t.Fatalf("command line = %s", line)
	}
	file := line[strings.Index(line, "< '")+3:]
	file = file[:strings.Index(file, "'")]
	data, err := os.ReadFile(file)
	os.Remove(file)
	if err != nil || !strings.Contains(string(data), "command: x") {
		t.Fatalf("context file %s: %q, %v", file, data, err)
	}
}

func TestSuggestion(t *testing.T) {
	for line, want := range map[string]string{
		"▸ npm install":        "npm install",
		"  ▸ `lsof -ti :5173`": "lsof -ti :5173",
		"The port is in use.":  "",
		"▸":                    "",
	} {
		got, ok := Suggestion(line)
		if got != want || ok != (want != "") {
			t.Errorf("Suggestion(%q) = %q, %v", line, got, ok)
		}
	}
}
