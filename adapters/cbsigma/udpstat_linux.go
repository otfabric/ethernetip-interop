// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"strconv"
	"strings"
)

type udpCount struct{ in, out uint64 }

// udpCounters reads the UDP datagrams received and sent in this network
// namespace (/proc/net/snmp). enip's IOConn does not count its packets; in
// the container the I/O connection is the only UDP traffic while it runs,
// so the difference over the exchange is its packets each way.
func udpCounters() udpCount {
	b, err := os.ReadFile("/proc/net/snmp")
	if err != nil {
		return udpCount{}
	}
	var head []string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "Udp:" {
			continue
		}
		if head == nil {
			head = f
			continue
		}
		var c udpCount
		for i := 1; i < len(f) && i < len(head); i++ {
			v, _ := strconv.ParseUint(f[i], 10, 64)
			switch head[i] {
			case "InDatagrams":
				c.in = v
			case "OutDatagrams":
				c.out = v
			}
		}
		return c
	}
	return udpCount{}
}
