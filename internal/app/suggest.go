package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/informeai/doted/internal/shell"
)

// Autosuggestions, as in the fish shell: while the cursor is at the end of
// the line, the rest of a likely line is shown faded after it, and Tab (or
// Right) accepts it. The suggestion comes from, in order:
//
//  1. history: the newest command that starts with what was typed;
//  2. the command word: doted's commands, shell builtins and PATH programs;
//  3. the last argument: files and directories, relative to doted's
//     directory.

// commandsTTL is how long the list of PATH programs is reused.
const commandsTTL = 30 * time.Second

type suggestCache struct {
	line, dir string
	suffix    string

	commands   []string
	commandsAt time.Time
}

// suggestion is what to show after the cursor, "" for nothing. It is
// recomputed only when the line or the directory change.
func (g *Game) suggestion(now time.Time) string {
	if !g.editor.AtEnd() || strings.TrimSpace(g.editor.Text()) == "" || g.target != nil {
		return ""
	}
	line, dir := g.editor.Text(), g.session.Dir()
	if c := &g.suggest; c.line != line || c.dir != dir {
		c.line, c.dir = line, dir
		c.suffix = g.computeSuggestion(line, now)
	}
	return g.suggest.suffix
}

// acceptSuggestion types the suggestion, reporting whether there was one.
func (g *Game) acceptSuggestion() bool {
	now := time.Now()
	suffix := g.suggestion(now)
	if suffix == "" {
		return false
	}
	promptLen := utf8.RuneCountInString(g.promptText())
	from := promptLen + g.editor.Cursor()
	g.editor.Insert([]rune(suffix)...)
	g.touch()
	g.typed()
	g.startZap(from, promptLen+g.editor.Cursor(), now)
	return true
}

func (g *Game) computeSuggestion(line string, now time.Time) string {
	if s := suggestFromHistory(g.editor.History(), line); s != "" {
		return s
	}
	runes := []rune(line)
	start, end := commandWord(runes)
	if end == len(runes) { // still typing the command word
		word := string(runes[start:end])
		// Ties go to doted's own commands, then the user's aliases and
		// functions, then shell builtins, then programs.
		return completeFrom(word, g.dotedNames(), g.session.Definitions(), shell.ShellBuiltins(), g.pathCommands(now))
	}
	// Otherwise complete the last argument as a path.
	if unicode.IsSpace(runes[len(runes)-1]) {
		return ""
	}
	i := strings.LastIndexFunc(line, unicode.IsSpace)
	return completePath(line[i+1:], g.session.Dir())
}

// suggestFromHistory returns the rest of the newest history entry that
// starts with line.
func suggestFromHistory(history []string, line string) string {
	for _, h := range slices.Backward(history) {
		if len(h) > len(line) && strings.HasPrefix(h, line) {
			return h[len(line):]
		}
	}
	return ""
}

// completeFrom returns the rest of the best candidate starting with word:
// the shortest, then the one from the earliest group, then the first
// alphabetically.
func completeFrom(word string, groups ...[]string) string {
	if word == "" {
		return ""
	}
	best, bestGroup := "", 0
	for group, candidates := range groups {
		for _, c := range candidates {
			if len(c) <= len(word) || !strings.HasPrefix(c, word) {
				continue
			}
			better := best == "" || len(c) < len(best) ||
				len(c) == len(best) && (group < bestGroup || group == bestGroup && c < best)
			if better {
				best, bestGroup = c, group
			}
		}
	}
	if best == "" {
		return ""
	}
	return best[len(word):]
}

func (g *Game) dotedNames() []string {
	names := make([]string, 0, len(dotedBuiltins))
	for name := range dotedBuiltins {
		names = append(names, name)
	}
	return names
}

// pathCommands lists the PATH programs, rescanned every commandsTTL.
func (g *Game) pathCommands(now time.Time) []string {
	c := &g.suggest
	if c.commands == nil || now.Sub(c.commandsAt) >= commandsTTL {
		c.commands = g.session.Commands()
		c.commandsAt = now
	}
	return c.commands
}

// completePath completes arg as a file or directory path relative to dir,
// adding a slash after directories. Hidden entries are only offered once a
// dot is typed.
func completePath(arg, dir string) string {
	expanded := arg
	if rest, ok := strings.CutPrefix(arg, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = home + "/" + rest
		}
	}
	parent, base := filepath.Split(expanded)
	if base == "" {
		return "" // "dir/" alone: many entries, nothing to prefer
	}
	if !filepath.IsAbs(parent) {
		parent = filepath.Join(dir, parent)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if isDir(filepath.Join(parent, name)) {
			name += "/"
		}
		names = append(names, name)
	}
	return completeFrom(base, names)
}

func isDir(path string) bool {
	info, err := os.Stat(path) // follows symlinks, like the shell does
	return err == nil && info.IsDir()
}
