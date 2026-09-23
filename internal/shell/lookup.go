package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode"
)

// FindCommand reports whether name would run as a program: a path to an
// executable file (relative to the session's directory), or a name found in
// the PATH commands get, which is the login environment plus the config's
// [shell.env]. Shell builtins and aliases are not programs; see
// IsShellBuiltin.
func (s *Session) FindCommand(name string) bool {
	if name == "" {
		return false
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		path := expandHomePath(name)
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.dir, path)
		}
		return isExecutable(path)
	}
	_, ok := s.LookPath(name)
	return ok
}

// LookPath finds the program name in the PATH commands get, like
// exec.LookPath but with the session's environment and directory.
func (s *Session) LookPath(name string) (string, bool) {
	for _, dir := range filepath.SplitList(s.lookupEnv("PATH")) {
		if dir == "" {
			dir = "." // an empty PATH entry means the current directory
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(s.dir, dir)
		}
		for _, candidate := range executableNames(name) {
			if path := filepath.Join(dir, candidate); isExecutable(path) {
				return path, true
			}
		}
	}
	return "", false
}

// Getenv is the value of key in the environment commands get.
func (s *Session) Getenv(key string) string { return s.lookupEnv(key) }

// Commands lists the programs in the PATH commands get, each name once.
func (s *Session) Commands() []string {
	seen := map[string]bool{}
	var names []string
	for _, dir := range filepath.SplitList(s.lookupEnv("PATH")) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue // relative entries depend on the directory; skip them here
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if seen[name] || e.IsDir() {
				continue
			}
			if isExecutable(filepath.Join(dir, name)) {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// lookupEnv returns a variable as commands would see it: the config's value
// if set, else the base environment's.
func (s *Session) lookupEnv(key string) string {
	prefix := key + "="
	for _, list := range [][]string{s.env, s.base} {
		for _, kv := range slices.Backward(list) { // later entries win
			if v, ok := strings.CutPrefix(kv, prefix); ok {
				return v
			}
		}
	}
	return ""
}

func expandHomePath(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// executableNames lists the file names that run as name: itself on Unix,
// and name plus each PATHEXT extension on Windows.
func executableNames(name string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(name) != "" {
		return []string{name}
	}
	exts := os.Getenv("PATHEXT")
	if exts == "" {
		exts = ".COM;.EXE;.BAT;.CMD"
	}
	var names []string
	for ext := range strings.SplitSeq(exts, ";") {
		names = append(names, name+strings.ToLower(ext))
	}
	return names
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

// shellBuiltins are commands the common shells (sh, bash, zsh) run
// themselves, so they work without being in the PATH.
var shellBuiltins = map[string]bool{
	".": true, ":": true, "[": true, "[[": true, "alias": true, "autoload": true,
	"bg": true, "bind": true, "bindkey": true, "break": true, "builtin": true,
	"cd": true, "command": true, "continue": true, "declare": true, "dirs": true,
	"disown": true, "echo": true, "eval": true, "exec": true, "exit": true,
	"export": true, "false": true, "fc": true, "fg": true, "getopts": true,
	"hash": true, "history": true, "jobs": true, "kill": true, "let": true,
	"local": true, "noglob": true, "popd": true, "printf": true, "pushd": true,
	"pwd": true, "read": true, "readonly": true, "return": true, "set": true,
	"setopt": true, "shift": true, "source": true, "test": true, "time": true,
	"times": true, "trap": true, "true": true, "type": true, "typeset": true,
	"ulimit": true, "umask": true, "unalias": true, "unset": true,
	"unsetopt": true, "wait": true, "whence": true, "where": true, "which": true,
	// Keywords that start compound commands.
	"if": true, "for": true, "while": true, "until": true, "case": true,
	"function": true, "select": true, "{": true, "(": true, "!": true,
}

// ShellBuiltins lists the builtin commands of the usual shells, without the
// keywords and punctuation, for completion.
func ShellBuiltins() []string {
	var names []string
	for name := range shellBuiltins {
		if len(name) > 1 && unicode.IsLetter(rune(name[0])) {
			names = append(names, name)
		}
	}
	return names
}

// IsShellBuiltin reports whether name is a builtin or keyword of the usual
// shells, which runs without being a program in the PATH.
func IsShellBuiltin(name string) bool { return shellBuiltins[name] }
