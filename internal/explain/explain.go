// Package explain asks an AI command line tool why a command failed and
// how to fix it.
//
// doted gathers what it knows of the failure (the command, how it ended,
// the end of its output, with secrets masked) and runs the tool on it as an
// ordinary command, whose answer shows as its output. The tools may read
// the project's files, but not change them or run anything: Claude Code
// with only its reading tools, Cursor's agent in its ask mode.
package explain

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// Failure is what's known of the command to explain.
type Failure struct {
	Command string   // as it was typed
	Result  string   // "exit 1", "killed", "still running, printing errors"
	Dir     string   // where it ran
	Branch  string   // the git branch there, if any
	System  string   // "macOS, zsh"
	Output  []string // its last lines
}

// Context is the text the tool gets on its standard input.
func (f Failure) Context() string {
	var b strings.Builder
	fmt.Fprintf(&b, "command: %s\n", f.Command)
	fmt.Fprintf(&b, "result: %s\n", f.Result)
	if f.Dir != "" {
		fmt.Fprintf(&b, "directory: %s\n", f.Dir)
	}
	if f.Branch != "" {
		fmt.Fprintf(&b, "git branch: %s\n", f.Branch)
	}
	if f.System != "" {
		fmt.Fprintf(&b, "system: %s\n", f.System)
	}
	b.WriteString("output (its last lines):\n")
	for _, l := range f.Output {
		b.WriteString(Redact(l))
		b.WriteByte('\n')
	}
	return b.String()
}

// SuggestionPrefix starts the lines of an answer that hold a command to run.
const SuggestionPrefix = "▸ "

// Prompt is what the tool is asked; language names the language to answer
// in ("" for English).
func Prompt(language string) string {
	if language == "" {
		language = "English"
	}
	return "The context describes a command that failed in a terminal, with the end of its output. " +
		"Explain briefly why it failed and how to fix it: at most 6 short lines of plain text, no Markdown, no headings. " +
		"You may read the project's files to find the cause. " +
		"When a command would fix it, put it on a line of its own starting with \"" + SuggestionPrefix + "\" (at most two such lines). " +
		"Answer in " + language + "."
}

// Tool is an AI command line tool that can explain.
type Tool struct {
	Name     string   // as explain and the config name it
	Programs []string // its executable, by the names it goes by
	Model    string   // the model used when none is set
	Install  string   // how to get it
	// Command is the command line that asks prompt of the tool program,
	// with the context read from the file at input.
	Command func(program, prompt, model, input string) string
}

// Tools are the tools doted knows, by name.
var Tools = map[string]Tool{
	"claude": {
		Name:     "claude",
		Programs: []string{"claude"},
		Model:    "haiku",
		Install:  "npm install -g @anthropic-ai/claude-code",
		// The context comes on standard input; reading tools only.
		Command: func(program, prompt, model, input string) string {
			cmd := program + " -p " + Quote(prompt) + " --allowedTools 'Read,Grep,Glob'"
			if model != "" {
				cmd += " --model " + Quote(model)
			}
			return cmd + " < " + Quote(input)
		},
	},
	"cursor": {
		Name:     "cursor",
		Programs: []string{"agent", "cursor-agent"}, // cursor-agent on older installs
		Install:  "curl https://cursor.com/install -fsS | bash",
		// agent -p "<prompt>", with the context after the prompt; ask mode
		// reads, but changes nothing.
		Command: func(program, prompt, model, input string) string {
			cmd := program + ` -p "$(printf '%s\n\nContext:\n' ` + Quote(prompt) + `; cat ` + Quote(input) + `)" --mode ask --output-format text`
			if model != "" {
				cmd += " --model " + Quote(model)
			}
			return cmd
		},
	},
}

// ToolNames lists the known tools, for messages.
func ToolNames() string {
	names := make([]string, 0, len(Tools))
	for n := range Tools {
		names = append(names, n)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// CommandLine is the command that runs tool's program on f and then
// removes the context file, ending as the tool did; an empty model is the
// tool's. The file is written here.
func CommandLine(tool Tool, program string, f Failure, model, language string) (string, error) {
	if model == "" {
		model = tool.Model
	}
	file, err := os.CreateTemp("", "doted-explain-*.txt")
	if err != nil {
		return "", err
	}
	_, err = file.WriteString(f.Context())
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	name := Quote(file.Name())
	return tool.Command(program, Prompt(language), model, file.Name()) + "; __doted_s=$?; rm -f " + name + "; (exit $__doted_s)", nil
}

// Quote quotes s for the shell, in single quotes.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Suggestion returns the command a line of an answer suggests, if any.
func Suggestion(line string) (string, bool) {
	cmd, ok := strings.CutPrefix(strings.TrimSpace(line), strings.TrimSpace(SuggestionPrefix))
	cmd = strings.Trim(strings.TrimSpace(cmd), "`")
	if !ok || cmd == "" {
		return "", false
	}
	return cmd, true
}

// secrets are patterns of tokens and passwords that mustn't leave the
// machine; the group, when there is one, is the part masked.
var secrets = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), // a JWT
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+([A-Za-z0-9._~+/=-]{8,})`),
	regexp.MustCompile(`(?i)[A-Za-z0-9_]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)["']?\s*[:=]\s*["']?([^\s"',;]+)`),
	regexp.MustCompile(`://[^/\s:@]+:([^@\s/]+)@`), // a password in a URL
}

// Redact masks what looks like a secret in line.
func Redact(line string) string {
	for _, re := range secrets {
		line = re.ReplaceAllStringFunc(line, func(m string) string {
			sub := re.FindStringSubmatchIndex(m)
			if len(sub) >= 4 && sub[2] >= 0 {
				return m[:sub[2]] + "‹redacted›" + m[sub[3]:]
			}
			return "‹redacted›"
		})
	}
	return line
}
