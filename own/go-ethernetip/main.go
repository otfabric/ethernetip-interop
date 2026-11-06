// SPDX-License-Identifier: MIT

// Command ethernetip-interop is the ethernetip-interop-otfabric image:
// go-ethernetip behind the container contract (docs/CONTAINER_CONTRACT.md).
//
//	server  serves a fixture with go-ethernetip's server and Logix tag
//	        server: identity, assemblies, Class 1 and Class 3 connections,
//	        controller tags and templates
//	client  runs one operation with go-ethernetip's client, scanner and
//	        Logix client and prints one JSON result document
//
// It is OT Fabric's own stack, not an independent implementation: results
// against it are never independent evidence for go-ethernetip (README.md).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
)

const (
	adapterName   = "otfabric"
	schemaVersion = "1.0"
	contract      = "1.0"
	fixtureRoot   = "/fixtures"
	readyDefault  = "/run/ethernetip-interop/ready"
)

// Set with -ldflags by the Dockerfile.
var (
	adapterVersion   = "dev"
	upstreamVersion  = "dev"
	upstreamRevision = "0000000000000000000000000000000000000000"
)

const (
	exitOK             = 0
	exitFailed         = 1
	exitUsage          = 2
	exitInvalidFixture = 4
)

var (
	stdoutMu sync.Mutex
	stdout   io.Writer = os.Stdout
	stderr   io.Writer = os.Stderr
)

// emit prints one JSON document on one line of stdout.
func emit(doc any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		logf("cannot encode a document: %v", err)
		return
	}
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	_, _ = stdout.Write(buf.Bytes())
}

func logf(format string, a ...any) {
	_, _ = fmt.Fprintf(stderr, "ethernetip-interop: "+format+"\n", a...)
}

func usage(format string, a ...any) int {
	logf(format, a...)
	return exitUsage
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return usage("usage: ethernetip-interop server|client <operation>|print-capabilities|print-fixture [NAME]|version")
	}
	switch args[0] {
	case "server":
		return serve(args[1:])
	case "client":
		if len(args) < 2 || !slices.Contains(clientOperations, args[1]) {
			return usage("unknown or missing client operation")
		}
		return clientOp(args[1], args[2:])
	case "print-capabilities":
		emit(capabilities())
		return exitOK
	case "print-fixture":
		name := serverFixtures[0]
		if len(args) > 1 {
			name = args[1]
		}
		return printFixture(name)
	case "version":
		emit(map[string]any{
			"adapter": adapterName, "adapterVersion": adapterVersion, "upstream": "go-ethernetip",
			"upstreamVersion": upstreamVersion, "upstreamRevision": upstreamRevision, "contract": contract,
		})
		return exitOK
	}
	return usage("unknown command %q", args[0])
}

// printFixture writes /fixtures/<name>/fixture.json byte for byte.
func printFixture(name string) int {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return usage("invalid fixture name %q", name)
	}
	root, err := os.OpenRoot(fixtureRoot)
	if err != nil {
		return usage("no fixtures: %v", err)
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(name + "/fixture.json")
	if err != nil {
		return usage("no fixture %s", name)
	}
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	_, _ = stdout.Write(b)
	return exitOK
}

// clientOperations are the operations "client" runs, in the order of the
// capability document. Not "discover", which is pycomm3's discover().
var clientOperations = []string{
	"list-identity", "list-services", "get-attribute", "set-attribute", "get-attributes-all",
	"multiple-get", "identity", "io", "read-tag", "read-tags", "write-tag", "tag-list",
}

// serverFixtures are the fixtures "server" serves, the default first: the
// Logix controller, so that the cross-stack self-test exercises the tag
// services as well as the CIP objects and Class 1 I/O.
var serverFixtures = []string{"logix", "adapter", "logix-structures"}

func capabilities() map[string]any {
	return map[string]any{
		"schemaVersion":  schemaVersion,
		"adapter":        adapterName,
		"adapterVersion": adapterVersion,
		"protocol":       "ethernetip",
		"upstream": map[string]any{
			"name": "go-ethernetip", "version": upstreamVersion, "revision": upstreamRevision,
			"license": "MIT", "language": "Go", "independentEngine": false,
		},
		"roles":            map[string]bool{"server": true, "client": true},
		"fixtures":         serverFixtures,
		"clientOperations": clientOperations,
		"clientFeatures": map[string]bool{
			"listIdentityTcp":    true,
			"listIdentityUdp":    true,
			"listServices":       true,
			"getAttributeSingle": true,
			"setAttributeSingle": true,
			"getAttributesAll":   true,
			"multipleService":    true,
			"unconnectedSend":    true,
			"class3":             true,
			"class1":             true,
			"largeForwardOpen":   true,
			"multicast":          true,
			"inputOnly":          true,
			"listenOnly":         true,
			"logixTags":          true,
			"readTagFragmented":  true,
			"writeTagFragmented": true,
			// The contract's write-tag --type BOOL (a bit of an integer or
			// of a BOOL array) and STRING/JSON values: README.md.
			"readModifyWrite": false,
			"templates":       false,
			"tagList":         true,
			"configData":      true,
		},
		"serverFeatures": map[string]bool{
			"listIdentityTcp":    true,
			"listIdentityUdp":    true,
			"listServices":       true,
			"listInterfaces":     true,
			"getAttributeSingle": true,
			"setAttributeSingle": true,
			"getAttributesAll":   true,
			"getAttributeList":   true,
			"multipleService":    true,
			"unconnectedSend":    true,
			"class3":             true,
			"class1":             true,
			"largeForwardOpen":   true,
			"multicast":          true,
			"inputOnly":          true,
			"listenOnly":         true,
			"logixTags":          true,
			"readTagFragmented":  true,
			"writeTagFragmented": true,
			"readModifyWrite":    true,
			"tagList":            true,
			"templates":          true,
			"configData":         true,
			"tcpipObject":        true,
			"ethernetLinkObject": true,
			"errorsCloseSession": false,
		},
	}
}
