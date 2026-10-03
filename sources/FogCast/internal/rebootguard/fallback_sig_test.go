package rebootguard

import "syscall"

var (
	sigterm = syscall.SIGTERM
	sigzero = syscall.Signal(0)
)
