// Package notify shows desktop notifications without cgo, with the
// system's own tools: osascript on macOS, notify-send on Linux and
// PowerShell on Windows.
package notify

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrUnavailable means the system has no way to show notifications that
// doted knows of, such as Linux without notify-send.
var ErrUnavailable = errors.New("no notification tool found (install notify-send)")

const timeout = 10 * time.Second

// Send shows a notification and returns once it's been handed to the system.
func Send(title, body string) error {
	args := Command(runtime.GOOS, title, body)
	if _, err := exec.LookPath(args[0]); err != nil {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, args[0], args[1:]...).Run()
}

// Command is the command line that shows a notification on goos.
func Command(goos, title, body string) []string {
	switch goos {
	case "darwin":
		quote := func(s string) string {
			return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
		}
		return []string{"osascript", "-e", "display notification " + quote(body) + " with title " + quote(title)}
	case "windows":
		quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
		script := "Add-Type -AssemblyName System.Windows.Forms; " +
			"$n = New-Object System.Windows.Forms.NotifyIcon; " +
			"$n.Icon = [System.Drawing.SystemIcons]::Information; $n.Visible = $true; " +
			"$n.ShowBalloonTip(5000, " + quote(title) + ", " + quote(body) + ", 'Info'); " +
			"Start-Sleep -Seconds 6; $n.Dispose()"
		return []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", script}
	}
	return []string{"notify-send", "--app-name=doted", title, body}
}
