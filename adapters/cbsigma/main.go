// SPDX-License-Identifier: MIT

// Command ethernetip-interop is the ethernetip-interop adapter for cB-Sigma's
// enip (https://github.com/cB-Sigma/enip), a Go EtherNet/IP library with a
// client (scanner) and an adapter (server) role.
//
//	server  serves a fixture with enip's server package (the controller of
//	        cB-Sigma's enip-sim since its v0.2.0): identity, controller tags,
//	        structures, tag browsing, Class 3 connections, Unconnected_Send
//	        and Class 1 I/O
//	client  runs one operation with enip's Client (explicit messaging, Logix
//	        tags, browsing) or its OpenIO (Class 1 I/O) and prints one JSON
//	        result document
//
// The container contract is docs/CONTAINER_CONTRACT.md of the
// ethernetip-interop repository.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

const adapterName = "cbsigma"

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
	_, _ = os.Stdout.Write(append(b, '\n'))
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ethernetip-interop: "+format+"\n", args...)
}

func usage(format string, args ...any) int {
	logf(format, args...)
	return 2
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return usage("usage: ethernetip-interop server|client <op>|print-capabilities|print-fixture [NAME]|version")
	}
	switch args[0] {
	case "server":
		return serve(args[1:])
	case "client":
		if len(args) < 2 || !slices.Contains(clientOperations, args[1]) {
			return usage("unknown or missing client operation")
		}
		return runClient(args[1], args[2:])
	case "print-capabilities":
		emit(capabilities())
		return 0
	case "print-fixture":
		name := serverFixtures[0]
		if len(args) > 1 {
			name = args[1]
		}
		return printFixture(name)
	case "version":
		emit(map[string]any{
			"adapter": adapterName, "adapterVersion": adapterVersion, "upstream": "cB-Sigma/enip",
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
	_, _ = os.Stdout.Write(b)
	return 0
}

// clientOperations are the operations "client" runs, in the order of the
// capability document.
var clientOperations = []string{
	"list-identity", "get-attribute", "set-attribute", "get-attributes-all", "multiple-get",
	"io", "read-tag", "read-tags", "write-tag", "tag-list",
}

// serverFixtures are the fixtures "server" serves, the default first.
var serverFixtures = []string{"logix", "logix-structures"}

func capabilities() map[string]any {
	return map[string]any{
		"schemaVersion":  "1.0",
		"adapter":        adapterName,
		"adapterVersion": adapterVersion,
		"protocol":       "ethernetip",
		"upstream": map[string]any{
			"name": "cB-Sigma/enip", "version": upstreamVersion, "revision": upstreamRevision,
			"license": "Apache-2.0", "language": "Go", "independentEngine": true,
		},
		"roles":            map[string]bool{"server": true, "client": true},
		"fixtures":         serverFixtures,
		"clientOperations": clientOperations,
		"clientFeatures": map[string]bool{
			"listIdentityTcp":    true,
			"listIdentityUdp":    true,
			"listServices":       false,
			"getAttributeSingle": true,
			"setAttributeSingle": true,
			"getAttributesAll":   true,
			"multipleService":    true,
			"unconnectedSend":    true,
			"class3":             true,
			"class1":             true,
			"largeForwardOpen":   false,
			"multicast":          false,
			"inputOnly":          false,
			"listenOnly":         false,
			"logixTags":          true,
			"readTagFragmented":  true,
			"writeTagFragmented": true,
			"readModifyWrite":    false,
			"tagList":            true,
			"templates":          false,
			"configData":         false,
		},
		"serverFeatures": map[string]bool{
			"listIdentityTcp":    true,
			"listIdentityUdp":    true,
			"listServices":       false,
			"listInterfaces":     false,
			"getAttributeSingle": true,
			"setAttributeSingle": true,
			"getAttributesAll":   true,
			"getAttributeList":   false,
			"multipleService":    true,
			"unconnectedSend":    true,
			"class3":             true,
			"class1":             true,
			"largeForwardOpen":   true,
			"multicast":          false,
			"inputOnly":          false,
			"listenOnly":         false,
			"logixTags":          true,
			"readTagFragmented":  true,
			"writeTagFragmented": true,
			"readModifyWrite":    false,
			"tagList":            true,
			"templates":          true,
			"configData":         false,
			"tcpipObject":        false,
			"ethernetLinkObject": false,
			"errorsCloseSession": false,
		},
	}
}
