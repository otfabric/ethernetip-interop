// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/go-ethernetip/cip"
)

// The repository's fixtures are not in the build stage, so these tests carry
// their own. scripts/smoke.sh and scripts/interop.sh test against the real ones.
const testFixture = `{
  "schemaVersion": "1.0", "name": "logix",
  "identity": {"vendorId": 1, "deviceType": 14, "productCode": 54, "revision": "20.11",
               "serialNumber": 2882400001, "productName": "test controller"},
  "assemblies": [{"instance": 100, "kind": "input", "size": 4}, {"instance": 150, "kind": "output", "size": 4}],
  "echoOutputToInput": true, "slot": 0,
  "tags": [
    {"name": "Counter", "type": "DINT", "values": [42]},
    {"name": "Ratio", "type": "LREAL", "values": [0.125]},
    {"name": "Running", "type": "BOOL", "values": [1]},
    {"name": "Bits", "type": "BOOL", "count": 40, "pattern": {"multiplier": 1, "offset": 0}},
    {"name": "Samples", "type": "DINT", "count": 300, "pattern": {"multiplier": 7, "offset": -1000}}
  ]
}`

func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out, errb bytes.Buffer
	stdoutMu.Lock()
	oldOut, oldErr := stdout, stderr
	stdout, stderr = &out, &errb
	stdoutMu.Unlock()
	t.Cleanup(func() {
		stdoutMu.Lock()
		stdout, stderr = oldOut, oldErr
		stdoutMu.Unlock()
		if t.Failed() {
			t.Logf("stderr: %s", errb.String())
		}
	})
	return &out
}

func TestRevision(t *testing.T) {
	if r, err := revision("255.0"); err != nil || r != (cip.Revision{Major: 255, Minor: 0}) {
		t.Errorf("255.0: %v %v", r, err)
	}
	for _, s := range []string{"256.1", "1.300", "x.1", "1", "-1.2"} {
		if _, err := revision(s); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
}

func TestWidthFlags(t *testing.T) {
	var a u16Flag
	if err := a.Set("0xFFFF"); err != nil || a != 0xFFFF {
		t.Errorf("u16 max: %v %v", a, err)
	}
	if err := a.Set("65536"); err == nil {
		t.Error("u16 65536 accepted")
	}
	var b u32Flag
	if err := b.Set("4294967296"); err == nil {
		t.Error("u32 4294967296 accepted")
	}
}

func TestCapabilities(t *testing.T) {
	out := capture(t)
	if code := run([]string{"print-capabilities"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("%v: %q", err, out.String())
	}
	if up := doc["upstream"].(map[string]any); up["independentEngine"] != false || doc["adapter"] != "otfabric" {
		t.Errorf("%v", doc)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{}, {"frobnicate"}, {"client"}, {"client", "discover", "--host", "x"}, {"client", "get-attribute"},
		{"client", "get-attribute", "--host", "x", "--frobnicate", "1"},
		{"client", "list-identity", "--host", "x", "--transport", "carrier-pigeon"},
		{"client", "list-services", "--host", "x", "--transport", "udp"},
		{"client", "set-attribute", "--host", "x", "--data", "zz"},
		{"client", "get-attribute", "--host", "x", "--class", "65536"},
		{"client", "io", "--host", "x", "--output", "150", "--input", "100", "--output-size", "2", "--output-data", "00"},
		{"client", "write-tag", "--host", "x", "--tag", "A", "--type", "STRING", "--values", "x"},
		{"client", "write-tag", "--host", "x", "--tag", "A", "--type", "DINT", "--values", "x"},
		{"print-fixture", "../logix"}, {"print-fixture", ""},
		{"server", "--frobnicate", "1"},
	} {
		out := capture(t)
		if code := run(args); code != exitUsage || out.Len() != 0 {
			t.Errorf("%q: exit %d, stdout %q", args, code, out.String())
		}
	}
}

func TestFixtures(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := loadFixture(write("good.json", testFixture)); err != nil {
		t.Error(err)
	}
	if _, err := loadFixture(write("other.json", `{"name": "opener-sample"}`)); err == nil {
		t.Error("a fixture it does not serve was accepted")
	}
	if _, err := loadFixture(write("bad.json", `{"name": `)); err == nil {
		t.Error("invalid JSON accepted")
	}
	for _, body := range []string{
		strings.Replace(testFixture, `"20.11"`, `"300.1"`, 1),
		strings.Replace(testFixture, `"kind": "input"`, `"kind": "frob"`, 1),
		strings.Replace(testFixture, `"type": "DINT", "values"`, `"type": "FROB", "values"`, 1),
	} {
		f, err := loadFixture(write("x.json", body))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := newServer(f); err == nil {
			t.Errorf("served: %s", body)
		}
	}
	out := capture(t)
	if code := run([]string{"server", "--fixture", filepath.Join(dir, "none.json")}); code != exitInvalidFixture {
		t.Errorf("missing fixture: exit %d %q", code, out.String())
	}
}

// TestLoopback runs the client operations against the server, both of this
// adapter, over TCP on the loopback interface.
func TestLoopback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fixture.json")
	if err := os.WriteFile(p, []byte(testFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := loadFixture(p)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := newServer(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })
	_, port, _ := net.SplitHostPort(srv.Addr().String())
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatal(err)
	}
	do := func(op string, args ...string) map[string]any {
		t.Helper()
		out := capture(t)
		code := run(append([]string{"client", op, "--host", "127.0.0.1", "--port", port}, args...))
		var doc map[string]any
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatalf("%s: exit %d, %q", op, code, out.String())
		}
		if code != exitOK || doc["ok"] != true {
			t.Fatalf("%s %v: exit %d, %v", op, args, code, doc)
		}
		return doc
	}
	check := func(what string, got, want any) {
		t.Helper()
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		if !bytes.Equal(g, w) {
			t.Errorf("%s: %s, want %s", what, g, w)
		}
	}
	check("list-identity", do("list-identity")["identity"].(map[string]any)["productName"], "test controller")
	check("list-services", len(do("list-services")["services"].([]any)), 1)
	check("get-attribute", do("get-attribute", "--class", "1", "--instance", "1", "--attribute", "7")["data"], "0f7465737420636f6e74726f6c6c6572")
	check("error status", do("get-attribute", "--class", "1", "--instance", "1", "--attribute", "99")["status"], 0x14)
	check("connected", do("get-attribute", "--class", "1", "--instance", "1", "--attribute", "1", "--connected")["data"], "0100")
	check("get-attributes-all", strings.HasPrefix(do("get-attributes-all", "--class", "1", "--instance", "1")["data"].(string), "01000e003600140b"), true)
	do("set-attribute", "--class", "4", "--instance", "150", "--attribute", "3", "--data", "0a0b0c0d")
	check("echo", do("get-attribute", "--class", "4", "--instance", "100", "--attribute", "3")["data"], "0a0b0c0d")
	check("multiple-get", len(do("multiple-get", "--path", "@1/1/1", "--path", "@1/1/7")["replies"].([]any)), 2)
	check("identity", do("identity")["identity"].(map[string]any)["productCode"], 54)
	check("read-tags", do("read-tags", "--tags", "Counter,Ratio,Running")["results"],
		[]map[string]any{{"status": 0, "tag": "Counter", "values": []any{42}}, {"status": 0, "tag": "Ratio", "values": []any{0.125}},
			{"status": 0, "tag": "Running", "values": []any{true}}})
	vals := do("read-tag", "--tag", "Samples", "--count", "300")["values"].([]any)
	check("fragmented", []any{len(vals), vals[299]}, []any{300, 299*7 - 1000})
	do("write-tag", "--tag", "Samples", "--type", "DINT", "--values", "5,6,7")
	check("write-tag", do("read-tag", "--tag", "Samples", "--count", "4")["values"], []any{5, 6, 7, 3*7 - 1000})
	check("unknown tag", do("read-tag", "--tag", "NoSuchTag")["status"] != 0, true)
	check("tag-list", len(do("tag-list")["tags"].([]any)), 5)
}

func TestUnreachable(t *testing.T) {
	out := capture(t)
	code := run([]string{"client", "get-attribute", "--host", "127.0.0.1", "--port", "1", "--timeout-ms", "500"})
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || code != exitFailed || doc["ok"] != false || doc["error"] == nil {
		t.Errorf("exit %d, %q", code, out.String())
	}
}
