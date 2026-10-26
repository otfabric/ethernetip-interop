// SPDX-License-Identifier: MIT

// Command ethernetip-interop is the ethernetip-interop adapter for gologix
// (https://github.com/danomagnum/gologix), a Go EtherNet/IP and Logix
// library with a client (originator) and a server (target).
//
//	server  serves a fixture with gologix's Server: identity, controller tags
//	        (Class 3 and Unconnected_Send) and Class 1 I/O
//	client  runs one operation with gologix's Client and prints one JSON
//	        result document
//
// The container contract is docs/CONTAINER_CONTRACT.md of the
// ethernetip-interop repository.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const adapterName = "gologix"

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
	// gologix logs through slog: everything goes to stderr, stdout is JSON only.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return usage("usage: ethernetip-interop server|client <op>|print-capabilities|print-fixture [NAME]|version")
	}
	switch args[0] {
	case "server":
		return serve(args[1:])
	case targetCommand:
		return runTarget(args[1:])
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
			"adapter": adapterName, "adapterVersion": adapterVersion, "upstream": "gologix",
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

// serverFixtures are the fixtures "server" serves, the default first.
var serverFixtures = []string{"logix", "adapter"}

func capabilities() map[string]any {
	return map[string]any{
		"schemaVersion":  "1.0",
		"adapter":        adapterName,
		"adapterVersion": adapterVersion,
		"protocol":       "ethernetip",
		"upstream": map[string]any{
			"name": "gologix", "version": upstreamVersion, "revision": upstreamRevision,
			"license": "MIT", "language": "Go", "independentEngine": true,
		},
		"roles":            map[string]bool{"server": true, "client": true},
		"fixtures":         serverFixtures,
		"clientOperations": clientOperations,
		"clientFeatures": map[string]bool{
			"listIdentityTcp":    true,
			"listIdentityUdp":    false,
			"listServices":       true,
			"getAttributeSingle": true,
			"setAttributeSingle": true,
			"getAttributesAll":   true,
			"multipleService":    true,
			"unconnectedSend":    false,
			"class3":             true,
			"class1":             false,
			"largeForwardOpen":   true,
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
		"serverFeatures": serverFeatures,
	}
}
