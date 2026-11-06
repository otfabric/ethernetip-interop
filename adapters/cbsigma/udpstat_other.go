// SPDX-License-Identifier: MIT

//go:build !linux

package main

type udpCount struct{ in, out uint64 }

// udpCounters has no source outside Linux: sent and received read 0.
func udpCounters() udpCount { return udpCount{} }
