//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package shell

import "golang.org/x/sys/unix"

const getTermios = unix.TIOCGETA
