// SPDX-License-Identifier: MIT

// Command ethernetip-interop is the ethernetip-interop adapter for
// go-ethernet-ip (https://github.com/loki-os/go-ethernet-ip), a small Go
// EtherNet/IP client (originator) for Logix controllers.
//
//	client  runs one operation with go-ethernet-ip's EIPTCP and Tag and
//	        prints one JSON result document
//
// go-ethernet-ip has no target, so the image has no server role.
//
// The container contract is docs/CONTAINER_CONTRACT.md of the
// ethernetip-interop repository.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const adapterName = "lokieip"

// Set by the image (Dockerfile ENV).
var (
	adapterVersion   = envOr("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev")
	upstreamVersion  = envOr("ETHERNETIP_INTEROP_UPSTREAM_VERSION", "")
	upstreamRevision = envOr("ETHERNETIP_INTEROP_UPSTREAM_REVISION", "")
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// emit prints one JSON document on one line of stdout.
func emit(doc any) {
	b, err := json.Marshal(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot encode document: %v\n", err)
		os.Exit(1)
	}
	os.Stdout.Write(append(b, '\n'))
}

func usage(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	return 2
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return usage("usage: ethernetip-interop client <op>|print-capabilities|print-fixture [NAME]|version")
	}
	switch args[0] {
	case "server":
		return usage("the lokieip adapter has no server role: go-ethernet-ip is a client only")
	case "client":
		if len(args) < 2 || !hasOperation(args[1]) {
			return usage("unknown or missing client operation")
		}
		return runClient(args[1], args[2:])
	case "print-capabilities":
		emit(capabilities())
		return 0
	case "print-fixture":
		name := "logix"
		if len(args) > 1 {
			name = args[1]
		}
		return printFixture(name)
	case "version":
		emit(map[string]any{
			"adapter": adapterName, "adapterVersion": adapterVersion, "upstream": "go-ethernet-ip",
			"upstreamVersion": upstreamVersion, "upstreamRevision": upstreamRevision, "contract": "1.0",
		})
		return 0
	}
	return usage("unknown command %s", args[0])
}

// printFixture writes /fixtures/<name>/fixture.json byte for byte.
func printFixture(name string) int {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return usage("invalid fixture name %q", name)
	}
	b, err := os.ReadFile("/fixtures/" + name + "/fixture.json")
	if err != nil {
		return usage("no fixture %s", name)
	}
	os.Stdout.Write(b)
	return 0
}

// clientOperations are the operations "client" runs, in the order of the
// capability document.
var clientOperations = []string{
	"list-identity", "list-services", "get-attribute", "set-attribute", "get-attributes-all",
	"multiple-get", "read-tag", "read-tags", "write-tag", "tag-list",
}

func hasOperation(op string) bool {
	for _, o := range clientOperations {
		if o == op {
			return true
		}
	}
	return false
}

func capabilities() map[string]any {
	return map[string]any{
		"schemaVersion":  "1.0",
		"adapter":        adapterName,
		"adapterVersion": adapterVersion,
		"protocol":       "ethernetip",
		"upstream": map[string]any{
			"name": "go-ethernet-ip", "version": upstreamVersion, "revision": upstreamRevision,
			"license": "WTFPL", "language": "Go", "independentEngine": true,
		},
		"roles":            map[string]bool{"server": false, "client": true},
		"fixtures":         []string{},
		"clientOperations": clientOperations,
		"clientFeatures": map[string]bool{
			"listIdentityTcp":    true,
			"listIdentityUdp":    false,
			"listServices":       true,
			"getAttributeSingle": true,
			"setAttributeSingle": true,
			"getAttributesAll":   true,
			"multipleService":    true,
			"unconnectedSend":    true,
			"class3":             true,
			"class1":             false,
			"largeForwardOpen":   false,
			"multicast":          false,
			"inputOnly":          false,
			"listenOnly":         false,
			"logixTags":          true,
			"readTagFragmented":  false,
			"writeTagFragmented": false,
			"readModifyWrite":    false,
			"tagList":            true,
			"templates":          false,
			"configData":         false,
		},
		"serverFeatures": map[string]bool{},
	}
}
