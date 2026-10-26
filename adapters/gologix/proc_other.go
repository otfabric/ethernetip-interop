// SPDX-License-Identifier: MIT

//go:build !linux

package main

import "syscall"

func childAttr() *syscall.SysProcAttr { return nil }
