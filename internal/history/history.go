// Package history keeps the commands typed in doted across sessions, in a
// plain file with one command per line.
package history

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Path is where the history lives: $XDG_DATA_HOME/doted/history, falling
// back to ~/.local/share/doted/history.
func Path() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "doted", "history")
}

// Load returns the newest max commands in path, oldest first, without
// repeating a command right after itself. A missing file is an empty
// history. When the file has grown past twice max, it is rewritten with just
// what was kept, so it doesn't grow forever.
func Load(path string, max int) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || len(lines) > 0 && lines[len(lines)-1] == line {
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	total := len(lines)
	if total > max {
		lines = lines[total-max:]
	}
	if total > 2*max {
		if err := rewrite(path, lines); err != nil {
			return lines, err
		}
	}
	return lines, nil
}

// Keep reports whether line should be saved: not blank, and not starting
// with a space, which (as in bash's HISTCONTROL=ignorespace) marks a command
// to keep out of the history, say because it holds a password.
func Keep(line string) bool {
	return strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ")
}

// Append adds line to the end of the file at path, creating it if needed.
// Appends are atomic for lines this short, so several doted windows can
// share one file.
func Append(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(oneLine(line) + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func rewrite(path string, lines []string) error {
	tmp := path + ".tmp"
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(oneLine(l))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// oneLine keeps a command on its line of the file.
func oneLine(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}
