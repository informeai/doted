//go:build unix

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCommandWord(t *testing.T) {
	for line, want := range map[string]string{
		"":                   "",
		"   ":                "",
		"git status":         "git",
		"  ls -la":           "ls",
		"FOO=bar make build": "make",
		"A=1 B_2=x ./run.sh": "./run.sh",
		"FOO=bar":            "",
		"=oops cmd":          "=oops",
		"1X=no cmd":          "1X=no",
		"npm run dev &":      "npm",
		"echo a=b":           "echo",
	} {
		runes := []rune(line)
		start, end := commandWord(runes)
		if got := string(runes[start:end]); got != want {
			t.Errorf("commandWord(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestClassifyCommand(t *testing.T) {
	g := newTestGame(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
	g.session.Chdir(dir)
	now := time.Now()

	for line, want := range map[string]commandKind{
		"":                     commandNone,
		"cd /tmp":              commandBuiltin,
		"help":                 commandBuiltin,
		"exit 3 &":             commandFound, // with & it runs in the shell, where exit is a builtin
		"sh -c true":           commandFound, // in the PATH
		"echo hi":              commandFound, // shell builtin, not in the PATH
		"FOO=1 sh":             commandFound,
		"./run.sh":             commandFound, // relative to doted's directory
		"./missing.sh":         commandMissing,
		"definitely-not-a-cmd": commandMissing,
	} {
		if got := g.classifyCommand(line, now); got != want {
			t.Errorf("classifyCommand(%q) = %v, want %v", line, got, want)
		}
	}

	// The same word elsewhere is looked up again: relative paths depend on
	// the directory.
	g.session.Chdir(t.TempDir())
	if got := g.classifyCommand("./run.sh", now); got != commandMissing {
		t.Errorf("./run.sh in another directory = %v, want missing", got)
	}
}

func TestClassifyCommandCacheExpires(t *testing.T) {
	g := newTestGame(t)
	dir := t.TempDir()
	g.session.Chdir(dir)
	now := time.Now()
	if got := g.classifyCommand("./later.sh", now); got != commandMissing {
		t.Fatalf("before it exists: %v", got)
	}
	os.WriteFile(filepath.Join(dir, "later.sh"), []byte("#!/bin/sh\n"), 0o755)
	if got := g.classifyCommand("./later.sh", now.Add(lookupTTL/2)); got != commandMissing {
		t.Fatalf("within the cache time the old answer stands, got %v", got)
	}
	if got := g.classifyCommand("./later.sh", now.Add(lookupTTL)); got != commandFound {
		t.Fatalf("after the cache time it should be found, got %v", got)
	}
}

func TestCommandColors(t *testing.T) {
	g := newTestGame(t)
	for k, want := range map[commandKind]string{
		commandBuiltin: "accent",
		commandFound:   "green",
		commandMissing: "error",
		commandNone:    "text",
	} {
		expected := map[string]any{
			"accent": g.theme.Accent, "green": g.theme.ANSI[2], "error": g.theme.Error, "text": g.theme.Foreground,
		}[want]
		if got := g.commandColor(k); got != expected {
			t.Errorf("color for %v = %v, want the %s color", k, got, want)
		}
	}
}

// Every name colored as a doted command must really be one.
func TestDotedBuiltinsAreHandled(t *testing.T) {
	for name := range dotedBuiltins {
		g := newTestGame(t)
		if !g.runBuiltin(name) {
			t.Errorf("%q is colored as a doted command but runBuiltin doesn't handle it", name)
		}
	}
}
