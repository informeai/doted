//go:build !windows

package clipboard

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Write puts text on the system clipboard.
func Write(ctx context.Context, text string) error {
	t, ok := chooseTool(runtime.GOOS, os.Getenv, exec.LookPath)
	if !ok {
		return ErrUnavailable
	}
	_, err := t.run(ctx, t.write, &text)
	return err
}

// Read returns the text on the system clipboard.
func Read(ctx context.Context) (string, error) {
	t, ok := chooseTool(runtime.GOOS, os.Getenv, exec.LookPath)
	if !ok {
		return "", ErrUnavailable
	}
	return t.run(ctx, t.read, nil)
}

// tool is a pair of commands that write and read the clipboard.
type tool struct {
	name        string
	write, read []string
	env         []string // extra environment
}

// run writes input to the command's stdin and returns its stdout.
func (t tool) run(ctx context.Context, args []string, input *string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), t.env...)
	if input != nil {
		cmd.Stdin = strings.NewReader(*input)
		// Tools like xclip and wl-copy stay behind to serve the clipboard;
		// with no stdout captured, Run returns as soon as they detach.
		return "", cmd.Run()
	}
	out, err := cmd.Output()
	return string(out), err
}

// chooseTool picks the clipboard tool for a Unix desktop from its
// environment and the commands installed (lookPath is exec.LookPath).
func chooseTool(goos string, getenv func(string) string, lookPath func(string) (string, error)) (tool, bool) {
	has := func(name string) bool { _, err := lookPath(name); return err == nil }
	switch {
	case goos == "darwin":
		// pbcopy reads and writes the text in the locale's encoding; apps
		// opened from the Finder get no LANG, which would mangle accents.
		return tool{name: "pbcopy", write: []string{"pbcopy"}, read: []string{"pbpaste"}, env: []string{"LANG=en_US.UTF-8", "LC_CTYPE=UTF-8"}}, true
	case getenv("WAYLAND_DISPLAY") != "" && has("wl-copy") && has("wl-paste"):
		return tool{name: "wl-clipboard", write: []string{"wl-copy"}, read: []string{"wl-paste", "--no-newline"}}, true
	case getenv("DISPLAY") != "" && has("xclip"):
		return tool{name: "xclip", write: []string{"xclip", "-selection", "clipboard", "-in"}, read: []string{"xclip", "-selection", "clipboard", "-out"}}, true
	case getenv("DISPLAY") != "" && has("xsel"):
		return tool{name: "xsel", write: []string{"xsel", "--clipboard", "--input"}, read: []string{"xsel", "--clipboard", "--output"}}, true
	}
	return tool{}, false
}
