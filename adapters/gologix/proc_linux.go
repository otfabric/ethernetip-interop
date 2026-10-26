// SPDX-License-Identifier: MIT

package main

import "syscall"

// childAttr ends the target with the adapter process.
func childAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
