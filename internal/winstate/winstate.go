// Package winstate remembers doted's window between runs: its size, where it
// was and whether it was maximized.
package winstate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Window is the saved window. Sizes and positions are in device-independent
// pixels; the position is relative to the named monitor's top-left corner.
type Window struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	HasPosition bool   `json:"has_position"`
	Monitor     string `json:"monitor,omitempty"`
	Maximized   bool   `json:"maximized"`
}

// Path is where the window is saved: $XDG_STATE_HOME/doted/window.json,
// falling back to ~/.local/state/doted/window.json.
func Path() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "doted", "window.json")
}

// Load reads the saved window, reporting whether there was a usable one.
func Load(path string) (Window, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Window{}, false
	}
	var w Window
	if json.Unmarshal(data, &w) != nil || w.Width <= 0 || w.Height <= 0 {
		return Window{}, false
	}
	return w, true
}

// Save writes the window, replacing the file in one step so a crash never
// leaves half of it.
func Save(path string, w Window) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
