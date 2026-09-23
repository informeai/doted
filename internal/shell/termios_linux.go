//go:build linux

package shell

import "golang.org/x/sys/unix"

const getTermios = unix.TCGETS
