//go:build !unix

package shell

import (
	"errors"
	"os"
	"os/exec"
)

var errNoPTY = errors.New("running commands needs a PTY, which is only supported on Unix for now")

func startPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) { return nil, errNoPTY }

func setSize(f *os.File, cols, rows int) error { return errNoPTY }

func lineMode(f *os.File) (canonical, ok bool) { return false, false }
