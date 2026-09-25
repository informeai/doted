package app

import (
	"image/color"
	"strings"
	"time"
	"unicode"

	"github.com/informeai/doted/internal/shell"
)

// As you type, the command word of the input is colored by what it is:
// doted's own commands in the accent color, programs that exist (and shell
// builtins, aliases and functions) in green, and anything else in red.

type commandKind int

const (
	commandNone    commandKind = iota // nothing typed yet
	commandBuiltin                    // one of doted's commands
	commandFound                      // a program or a shell builtin
	commandMissing                    // nothing runs by that name
)

// dotedBuiltins are the names runBuiltin handles.
var dotedBuiltins = map[string]bool{
	"jobs": true, "fg": true, "help": true, "exit": true, "quit": true, "pipeline": true, "explain": true,
}

// lookupTTL is how long a lookup is trusted, so a program installed while
// doted is open turns green soon.
const lookupTTL = 2 * time.Second

type lookup struct {
	found bool
	at    time.Time
}

// commandWord finds the command in line: the first word after any leading
// VAR=value assignments. It returns the word's rune range.
func commandWord(line []rune) (start, end int) {
	i := 0
	for {
		for i < len(line) && unicode.IsSpace(line[i]) {
			i++
		}
		start = i
		for i < len(line) && !unicode.IsSpace(line[i]) {
			i++
		}
		if !isAssignment(string(line[start:i])) {
			return start, i
		}
	}
}

// isAssignment reports whether word looks like NAME=value.
func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// classifyCommand says what the input's command word is, looking programs up
// through a short-lived cache.
func (g *Game) classifyCommand(line string, now time.Time) commandKind {
	runes := []rune(line)
	start, end := commandWord(runes)
	if start == end {
		return commandNone
	}
	word := string(runes[start:end])
	_, background := cutBackground(strings.TrimSpace(line))
	switch {
	case dotedBuiltins[word] && (!background || word == "pipeline"): // `exit 3 &` goes to the shell; `pipeline x &` doesn't
		return commandBuiltin
	case shell.IsShellBuiltin(word), g.session.IsDefined(word): // aliases and functions too
		return commandFound
	}

	// Relative paths depend on the directory, so it is part of the key.
	key := g.session.Dir() + "\x00" + word
	if l, ok := g.lookups[key]; ok && now.Sub(l.at) < lookupTTL {
		return kindOf(l.found)
	}
	found := g.session.FindCommand(word)
	if g.lookups == nil || len(g.lookups) > 512 {
		g.lookups = map[string]lookup{}
	}
	g.lookups[key] = lookup{found: found, at: now}
	return kindOf(found)
}

func kindOf(found bool) commandKind {
	if found {
		return commandFound
	}
	return commandMissing
}

func (g *Game) commandColor(k commandKind) color.RGBA {
	switch k {
	case commandBuiltin:
		return g.theme.Accent
	case commandFound:
		return g.theme.ANSI[2] // green
	case commandMissing:
		return g.theme.Error
	}
	return g.theme.Foreground
}
