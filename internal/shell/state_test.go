//go:build unix

package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// persistentShells are the shells with persistent state available here.
func persistentShells(t *testing.T) []string {
	var shells []string
	for _, name := range []string{"zsh", "bash"} {
		if path, err := exec.LookPath(name); err == nil {
			shells = append(shells, path)
		}
	}
	if len(shells) == 0 {
		t.Skip("neither zsh nor bash is installed")
	}
	return shells
}

// runAdopt runs cmdline to the end in the foreground and adopts its state,
// returning what it printed.
func runAdopt(t *testing.T, s *Session, cmdline string) string {
	t.Helper()
	p, err := s.Start(cmdline, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := wait(t, p, 5*time.Second)
	s.Adopt(p.State())
	return out
}

func TestStatePersistsBetweenCommands(t *testing.T) {
	for _, sh := range persistentShells(t) {
		t.Run(filepath.Base(sh), func(t *testing.T) {
			dir := t.TempDir()
			s := NewSession(dir)
			t.Cleanup(s.Close)
			s.Configure(sh, nil)

			runAdopt(t, s, "export DOTED_X=persisted")
			runAdopt(t, s, "mkdir -p sub && cd sub")
			runAdopt(t, s, "alias hi='echo alias-ran'")
			runAdopt(t, s, "greet() { echo \"function-ran $1\"; }")

			if out := runAdopt(t, s, `echo "[$DOTED_X]"`); !strings.Contains(out, "[persisted]") {
				t.Errorf("exported variable lost: %q", out)
			}
			if got, _ := filepath.EvalSymlinks(s.Dir()); got != mustEval(t, filepath.Join(dir, "sub")) {
				t.Errorf("directory after cd = %q, want %q", s.Dir(), filepath.Join(dir, "sub"))
			}
			if out := runAdopt(t, s, "hi"); !strings.Contains(out, "alias-ran") {
				t.Errorf("alias lost: %q", out)
			}
			if out := runAdopt(t, s, "greet doted"); !strings.Contains(out, "function-ran doted") {
				t.Errorf("function lost: %q", out)
			}
			if !s.IsDefined("hi") || !s.IsDefined("greet") {
				t.Errorf("defined names = %v, want hi and greet", s.Definitions())
			}
			// cd - goes back: the previous directory carries over too.
			runAdopt(t, s, "cd "+dir)
			if out := runAdopt(t, s, "cd - >/dev/null && pwd"); !strings.HasSuffix(strings.TrimSpace(out), "sub") {
				t.Errorf("cd - went to %q, want .../sub", strings.TrimSpace(out))
			}
			// The wrapper's own arguments don't leak into the command.
			if out := runAdopt(t, s, `echo "[$1][$#]"`); !strings.Contains(out, "[][0]") {
				t.Errorf("command saw wrapper arguments: %q", out)
			}
		})
	}
}

func TestDiscardedStateIsNotUsed(t *testing.T) {
	for _, sh := range persistentShells(t) {
		t.Run(filepath.Base(sh), func(t *testing.T) {
			s := NewSession(t.TempDir())
			t.Cleanup(s.Close)
			s.Configure(sh, nil)

			p, _ := s.Start("export DOTED_BG=1", 80, 24)
			wait(t, p, 5*time.Second)
			s.Discard(p.State()) // it ended in the background
			if out := runAdopt(t, s, `echo "[$DOTED_BG]"`); !strings.Contains(out, "[]") {
				t.Errorf("a background job's state leaked: %q", out)
			}
			if _, err := os.Stat(p.State() + ".env"); err == nil {
				t.Error("discarded state files were left behind")
			}
		})
	}
}

func TestExitAndShellLevel(t *testing.T) {
	for _, sh := range persistentShells(t) {
		t.Run(filepath.Base(sh), func(t *testing.T) {
			s := NewSession(t.TempDir())
			t.Cleanup(s.Close)
			s.Configure(sh, nil)
			runAdopt(t, s, "export DOTED_KEEP=1")

			// exit ends the shell before it saves: the state stays as it was.
			p, _ := s.Start("export DOTED_KEEP=2; exit 3", 80, 24)
			if _, done := wait(t, p, 5*time.Second); done.ExitCode != 3 {
				t.Fatalf("exit status = %d, want 3", done.ExitCode)
			}
			s.Adopt(p.State())
			if out := runAdopt(t, s, `echo "[$DOTED_KEEP]"`); !strings.Contains(out, "[1]") {
				t.Errorf("state after exit: %q", out)
			}

			// SHLVL doesn't climb with every command.
			first := runAdopt(t, s, `echo "lvl=$SHLVL"`)
			for range 3 {
				runAdopt(t, s, "true")
			}
			if again := runAdopt(t, s, `echo "lvl=$SHLVL"`); first != again {
				t.Errorf("SHLVL changed from %q to %q", first, again)
			}
		})
	}
}

func TestCaptureDefinitionsFromStartupFiles(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	rc := t.TempDir()
	os.WriteFile(filepath.Join(rc, ".zshrc"), []byte("alias ll='ls -l'\nproj() { cd ~/Projects; }\n_helper() { :; }\n"), 0o644)
	s := NewSession(t.TempDir())
	t.Cleanup(s.Close)
	s.Configure(zsh, map[string]string{"ZDOTDIR": rc})

	path, err := s.CaptureDefinitions(10 * time.Second)()
	if err != nil {
		t.Fatal(err)
	}
	s.UseDefinitions(path)
	if !s.IsDefined("ll") || !s.IsDefined("proj") || s.IsDefined("_helper") {
		t.Fatalf("names = %v, want ll and proj, without _helper", s.Definitions())
	}
	if out := runAdopt(t, s, "alias ll"); !strings.Contains(out, "ls -l") {
		t.Fatalf("the rc's alias isn't available to commands: %q", out)
	}
}

func TestPlainShellRunsWithoutState(t *testing.T) {
	s := NewSession(t.TempDir())
	t.Cleanup(s.Close)
	s.Configure("/bin/sh", nil)
	p, err := s.Start("echo plain", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := wait(t, p, 5*time.Second); !strings.Contains(out, "plain") || p.State() != "" {
		t.Fatalf("output %q, state %q", out, p.State())
	}
	if s.CaptureDefinitions(time.Second) != nil {
		t.Fatal("sh has no definitions to capture")
	}
}

func TestDefinedNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "defs")
	os.WriteFile(path, []byte(strings.Join([]string{
		"alias -- -='cd -'",
		"alias -g ...=../..",
		"alias gst='git status'",
		"alias ll='ls -lh'", // bash's alias -p looks the same
		"mkcd () {",
		"\tmkdir -p \"$1\" && cd \"$1\"",
		"}",
		"_private () {",
		"}",
		"greet () ",
		"{ ",
		"    echo hi",
		"}",
	}, "\n")), 0o644)
	names := definedNames(path)
	for _, want := range []string{"-", "...", "gst", "ll", "mkcd", "greet"} {
		if !names[want] {
			t.Errorf("missing %q in %v", want, names)
		}
	}
	if names["_private"] {
		t.Error("helpers starting with _ should be left out")
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// startupHome writes a variable, an alias and a function into every startup
// file of zsh and bash, and a tool in ~/bin that only .zshrc and .bashrc put
// on the PATH.
func startupHome(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	files := map[string]string{
		".zshenv":       "export S_ZSHENV=yes\n",
		".zprofile":     "export S_ZPROFILE=yes\n",
		".zshrc":        "export S_ZSHRC=yes\nexport PATH=\"$HOME/bin:$PATH\"\nalias s_al='echo alias-ok'\ns_fn() { echo fn-ok; }\n",
		".zlogin":       "export S_ZLOGIN=yes\n",
		".bash_profile": "export S_BASH_PROFILE=yes\n",
		".bashrc":       "export S_BASHRC=yes\nexport PATH=\"$HOME/bin:$PATH\"\nalias s_al='echo alias-ok'\ns_fn() { echo fn-ok; }\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(home, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(home, "bin"), 0o755)
	os.WriteFile(filepath.Join(home, "bin", "s-tool"), []byte("#!/bin/sh\necho tool-ok\n"), 0o755)
	return home
}

// Opened from the desktop, doted starts from a minimal environment and
// imports the login one; the startup environment captured with the
// definitions adds what ~/.zshrc and ~/.bashrc set, as a terminal would have.
func TestStartupFilesReachCommandsFromTheDesktop(t *testing.T) {
	want := map[string][]string{
		"zsh":  {"S_ZSHENV=yes", "S_ZPROFILE=yes", "S_ZSHRC=yes", "S_ZLOGIN=yes"},
		"bash": {"S_BASH_PROFILE=yes", "S_BASHRC=yes"},
	}
	for _, sh := range persistentShells(t) {
		t.Run(filepath.Base(sh), func(t *testing.T) {
			home := startupHome(t)
			s := NewSession(home)
			t.Cleanup(s.Close)
			s.Configure(sh, nil)
			s.base = []string{"PATH=/usr/bin:/bin", "HOME=" + home} // what Finder gives
			if err := s.ImportLoginEnvironment(5 * time.Second); err != nil {
				t.Fatal(err)
			}
			path, err := s.CaptureDefinitions(5 * time.Second)()
			if err != nil {
				t.Fatal(err)
			}
			s.UseDefinitions(path)
			if !s.UseStartupEnvironment(path) {
				t.Fatal("no startup environment was captured")
			}

			out := runAdopt(t, s, `env | grep '^S_' | sort; s_al; s_fn; s-tool`)
			for _, w := range append(want[filepath.Base(sh)], "alias-ok", "fn-ok", "tool-ok") {
				if !strings.Contains(out, w) {
					t.Errorf("missing %s in:\n%s", w, out)
				}
			}
			// doted itself sees the rc's PATH: the tool counts as a command.
			if !s.FindCommand("s-tool") {
				t.Error("a tool on the PATH set in the rc should be found")
			}
		})
	}
}

// Variables exported in a command carry on to the next ones, and a PATH
// changed that way is the one doted checks commands against.
func TestExportedVariablesAreRecognized(t *testing.T) {
	for _, sh := range persistentShells(t) {
		t.Run(filepath.Base(sh), func(t *testing.T) {
			home := startupHome(t)
			s := NewSession(home)
			t.Cleanup(s.Close)
			s.Configure(sh, nil)
			if s.FindCommand("s-tool") {
				t.Skip("s-tool is already on the PATH")
			}
			runAdopt(t, s, `export S_LATER=set; export PATH="$HOME/bin:$PATH"`)
			if out := runAdopt(t, s, `echo "[$S_LATER]"; s-tool`); !strings.Contains(out, "[set]") || !strings.Contains(out, "tool-ok") {
				t.Errorf("exports didn't carry on: %q", out)
			}
			if !s.FindCommand("s-tool") || s.Getenv("S_LATER") != "set" {
				t.Error("doted should check commands against the exported PATH and see the variable")
			}
		})
	}
}
