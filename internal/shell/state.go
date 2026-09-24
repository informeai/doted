package shell

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Every command runs in its own shell process, so commands can run side by
// side and move to the background. To make the shell feel like one long
// session anyway, zsh and bash commands run inside a small wrapper: it first
// restores the aliases and functions the previous command left, runs the
// command, then saves the environment, working directory and definitions to
// files. When a command ends in the foreground, the session adopts that state
// for the next one. Other shells (sh, fish...) run commands as before.

type shellKind int

const (
	plainShell shellKind = iota
	bashShell
	zshShell
)

func kindOf(shell string) shellKind {
	switch filepath.Base(shell) {
	case "bash":
		return bashShell
	case "zsh":
		return zshShell
	}
	return plainShell
}

// dumpDefinitions prints the shell's aliases and functions as a script that
// defines them again.
func dumpDefinitions(k shellKind) string {
	if k == zshShell {
		return "alias -L; functions"
	}
	return "alias -p; declare -f"
}

// wrapper runs "$3" after sourcing the definitions in "$1", then saves the
// state to files named "$2" plus .defs, .pwd and .env. "$4" is the previous
// directory, for cd -: shells don't take OLDPWD from the environment (zsh
// resets it, bash drops it), so the wrapper steps through it before going
// back to the current one, which makes the shell record it. That happens
// before the definitions are loaded, so the user's cd hooks don't run. The
// arguments move to named variables and are shifted away, so the command sees
// no $1.
func wrapper(k shellKind) string {
	prefix := ""
	if k == bashShell {
		prefix = "shopt -s expand_aliases\n" // bash leaves aliases off in scripts
	}
	return prefix + `__doted_defs=$1 __doted_out=$2 __doted_cmd=$3 __doted_oldpwd=$4
shift 4
if [ -n "$__doted_oldpwd" ] && [ -d "$__doted_oldpwd" ]; then
	__doted_here=$PWD
	builtin cd "$__doted_oldpwd" >/dev/null 2>&1 && builtin cd "$__doted_here" >/dev/null 2>&1
fi
if [ -r "$__doted_defs" ]; then . "$__doted_defs" >/dev/null 2>&1; fi
eval "$__doted_cmd"
__doted_status=$?
{ ` + dumpDefinitions(k) + `; } >"$__doted_out.defs" 2>/dev/null
pwd >"$__doted_out.pwd"
env -0 >"$__doted_out.env"
exit $__doted_status
`
}

// persistent reports whether commands keep state between them.
func (s *Session) persistent() bool { return kindOf(s.shell) != plainShell }

// commandArgs returns the arguments for the shell to run cmdline, and the
// prefix of the state files it will write ("" when it writes none).
func (s *Session) commandArgs(cmdline string) (args []string, state string, err error) {
	if !s.persistent() {
		return []string{"-c", cmdline}, "", nil
	}
	dir, err := s.stateDirectory()
	if err != nil {
		return nil, "", err
	}
	s.seq++
	state = filepath.Join(dir, fmt.Sprintf("cmd-%d", s.seq))
	return []string{"-c", wrapper(kindOf(s.shell)), "doted", s.defsPath, state, cmdline, s.oldpwd}, state, nil
}

func (s *Session) stateDirectory() (string, error) {
	if s.stateDir == "" {
		dir, err := os.MkdirTemp("", "doted-shell-")
		if err != nil {
			return "", err
		}
		s.stateDir = dir
	}
	return s.stateDir, nil
}

// Close removes the session's state files.
func (s *Session) Close() {
	if s.stateDir != "" {
		os.RemoveAll(s.stateDir)
		s.stateDir, s.defsPath = "", ""
	}
}

// Adopt makes the state a finished command left (see Process.State) the
// session's: the next commands get its environment, directory, aliases and
// functions. A command that didn't get to save its state (it ran exit, or
// was killed) changes nothing.
func (s *Session) Adopt(state string) {
	if state == "" {
		return
	}
	defer s.Discard(state)
	pwd, err := os.ReadFile(state + ".pwd")
	if err != nil {
		return
	}
	if dir := strings.TrimSpace(string(pwd)); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			s.dir = dir
		}
	}
	if env, err := os.ReadFile(state + ".env"); err == nil {
		if vars := s.cleanEnv(env); len(vars) > 0 {
			s.base = vars
		}
		s.oldpwd = envValue(env, "OLDPWD")
	}
	if _, err := os.Stat(state + ".defs"); err == nil {
		defs := filepath.Join(s.stateDir, "defs.sh")
		if os.Rename(state+".defs", defs) == nil {
			s.useDefinitions(defs)
		}
	}
}

// Discard drops the state a command left without using it, as for commands
// that finished in the background, where it would overwrite newer state.
func (s *Session) Discard(state string) {
	for _, ext := range []string{".pwd", ".env", ".defs"} {
		os.Remove(state + ext)
	}
}

// Variables the session sets itself, or that change meaning between shells,
// are left out of the adopted environment.
var managedVars = map[string]bool{
	"SHLVL": true, "_": true, "PWD": true, "OLDPWD": true,
	"TERM": true, "COLORTERM": true, "CLICOLOR": true,
}

func (s *Session) cleanEnv(dump []byte) []string {
	configured := map[string]bool{}
	for _, kv := range s.env {
		k, _, _ := strings.Cut(kv, "=")
		configured[k] = true
	}
	var vars []string
	for kv := range bytes.SplitSeq(dump, []byte{0}) {
		k, _, ok := strings.Cut(string(kv), "=")
		if ok && k != "" && !managedVars[k] && !configured[k] {
			vars = append(vars, string(kv))
		}
	}
	return vars
}

// envValue finds key in a NUL-separated environment dump.
func envValue(dump []byte, key string) string {
	for kv := range bytes.SplitSeq(dump, []byte{0}) {
		if v, ok := strings.CutPrefix(string(kv), key+"="); ok {
			return v
		}
	}
	return ""
}

// CaptureDefinitions returns a function that loads the aliases and functions
// the user's shell defines at startup (~/.zshrc, ~/.bashrc), for the first
// commands to start from. Starting an interactive shell can take a second,
// so the returned function is meant to run in the background; pass what it
// returns to UseDefinitions on the session's goroutine. It returns nil for
// shells without persistent state.
func (s *Session) CaptureDefinitions(timeout time.Duration) func() (string, error) {
	if !s.persistent() {
		return nil
	}
	dir, err := s.stateDirectory()
	if err != nil {
		return func() (string, error) { return "", err }
	}
	shell, kind := s.shell, kindOf(s.shell)
	env := append(slices.Clone(s.base), s.env...)
	out := filepath.Join(dir, "defs-startup.sh")
	return func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		// The environment the startup files leave goes next to the
		// definitions; see UseStartupEnvironment.
		cmd := exec.CommandContext(ctx, shell, "-i", "-c", "{ "+dumpDefinitions(kind)+"; } >\"$1\" 2>/dev/null; env -0 >\"$1.env\"", "doted", out)
		cmd.Env = env
		cmd.WaitDelay = time.Second // a daemon started by the rc may keep stdout open
		if err := cmd.Run(); err != nil {
			if _, statErr := os.Stat(out); statErr != nil {
				return "", fmt.Errorf("loading aliases and functions from the shell's startup files: %w", err)
			}
		}
		return out, nil
	}
}

// UseStartupEnvironment makes the environment the shell's interactive
// startup files left (~/.zshrc, ~/.bashrc), captured along with the
// definitions at defsPath, the one commands start from. An app opened from
// the desktop needs it: the login environment it imports doesn't run those
// files, and they're where PATH and tools like nvm are often set up. It
// reports whether there was one.
func (s *Session) UseStartupEnvironment(defsPath string) bool {
	data, err := os.ReadFile(defsPath + ".env")
	if err != nil {
		return false
	}
	vars := s.cleanEnv(data)
	if len(vars) == 0 {
		return false
	}
	s.base = vars
	return true
}

// UseDefinitions makes the definitions file at path the one commands start
// from, unless a command already left newer ones.
func (s *Session) UseDefinitions(path string) {
	if path == "" || s.defsPath != "" {
		return
	}
	s.useDefinitions(path)
}

func (s *Session) useDefinitions(path string) {
	s.defsPath = path
	s.names = definedNames(path)
}

// IsDefined reports whether name is one of the user's aliases or functions.
func (s *Session) IsDefined(name string) bool { return s.names[name] }

// Definitions lists the user's alias and function names.
func (s *Session) Definitions() []string {
	names := make([]string, 0, len(s.names))
	for n := range s.names {
		names = append(names, n)
	}
	return names
}

var functionLine = regexp.MustCompile(`^([^\s(){}=]+) \(\)`)

// definedNames reads the alias and function names in a definitions file, as
// printed by dumpDefinitions.
func definedNames(path string) map[string]bool {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	names := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, "alias "); ok {
			// Skip flags such as -g (zsh global aliases) and the -- guard.
			for strings.HasPrefix(rest, "-") && strings.Contains(rest, " ") && !strings.HasPrefix(rest, "-=") {
				_, rest, _ = strings.Cut(rest, " ")
			}
			if name, _, ok := strings.Cut(rest, "="); ok && name != "" {
				names[name] = true
			}
			continue
		}
		if m := functionLine.FindStringSubmatch(line); m != nil && !strings.HasPrefix(m[1], "_") {
			names[m[1]] = true // helpers starting with _ aren't meant to be typed
		}
	}
	if sc.Err() != nil {
		return names // a line too long to read: keep the names found before it
	}
	return names
}
