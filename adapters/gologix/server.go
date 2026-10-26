// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/danomagnum/gologix"
)

const defaultReadyFile = "/run/ethernetip-interop/ready"

// serverFeatures is what gologix's Server does with the adapter's providers.
var serverFeatures = map[string]bool{
	"listIdentityTcp":    true,
	"listIdentityUdp":    false,
	"listServices":       false,
	"listInterfaces":     false,
	"getAttributeSingle": false,
	"setAttributeSingle": false,
	"getAttributesAll":   true,
	"getAttributeList":   false,
	"multipleService":    false,
	"unconnectedSend":    true,
	"class3":             true,
	"class1":             true,
	"largeForwardOpen":   true,
	"multicast":          false,
	"inputOnly":          true,
	"listenOnly":         false,
	"logixTags":          true,
	"readTagFragmented":  false,
	"writeTagFragmented": false,
	"readModifyWrite":    false,
	"tagList":            false,
	"templates":          false,
	"configData":         false,
	"tcpipObject":        false,
	"ethernetLinkObject": false,
	"errorsCloseSession": true,
}

// fixture is the part of a fixture document the server uses
// (fixtures/schema/ethernetip-fixture.schema.json).
type fixture struct {
	SchemaVersion string `json:"schemaVersion"`
	Name          string `json:"name"`
	Identity      struct {
		VendorID     *int    `json:"vendorId"`
		DeviceType   *int    `json:"deviceType"`
		ProductCode  *int    `json:"productCode"`
		Revision     string  `json:"revision"`
		SerialNumber *uint32 `json:"serialNumber"`
		ProductName  string  `json:"productName"`
	} `json:"identity"`
	Assemblies []struct {
		Instance int    `json:"instance"`
		Kind     string `json:"kind"`
		Size     int    `json:"size"`
	} `json:"assemblies"`
	EchoOutputToInput bool              `json:"echoOutputToInput"`
	Slot              int               `json:"slot"`
	Templates         []json.RawMessage `json:"templates"`
	Tags              []fixtureTag      `json:"tags"`
}

type fixtureTag struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Count   int       `json:"count"`
	Values  []float64 `json:"values"`
	Strings []string  `json:"strings"`
	Pattern *struct {
		Multiplier float64 `json:"multiplier"`
		Offset     float64 `json:"offset"`
	} `json:"pattern"`
	Fields json.RawMessage `json:"fields"`
}

// loadFixture reads a fixture and checks that gologix can serve it.
func loadFixture(path string) (*fixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fx := &fixture{}
	if err := json.Unmarshal(b, fx); err != nil {
		return nil, err
	}
	if fx.SchemaVersion != "1.0" {
		return nil, fmt.Errorf("schemaVersion %q, want 1.0", fx.SchemaVersion)
	}
	if fx.Name == "" {
		return nil, errors.New("no name")
	}
	id := fx.Identity
	if id.VendorID == nil || id.DeviceType == nil || id.ProductCode == nil || id.SerialNumber == nil ||
		id.Revision == "" || id.ProductName == "" {
		return nil, errors.New("incomplete identity")
	}
	if _, _, err := revision(id.Revision); err != nil {
		return nil, err
	}
	if len(fx.Templates) > 0 {
		// gologix's server has no Template object (0x6C) and writes only
		// the STRING structure.
		return nil, errors.New("templates: gologix serves no structure templates")
	}
	for _, a := range fx.Assemblies {
		if a.Kind == "explicit" {
			// gologix has no Assembly object: assembly data is reachable only
			// as Class 1 I/O.
			return nil, fmt.Errorf("assembly %d (%s): gologix has no Assembly object for explicit messaging", a.Instance, a.Kind)
		}
	}
	for _, t := range fx.Tags {
		if _, err := tagValue(t); err != nil {
			return nil, fmt.Errorf("tag %s: %v", t.Name, err)
		}
	}
	return fx, nil
}

func hasClass1(fx *fixture) bool {
	for _, a := range fx.Assemblies {
		if a.Kind == "input" {
			return true
		}
	}
	return false
}

func revision(s string) (int, int, error) {
	p := strings.Split(s, ".")
	if len(p) != 2 {
		return 0, 0, fmt.Errorf("revision %q", s)
	}
	ma, err1 := strconv.Atoi(p[0])
	mi, err2 := strconv.Atoi(p[1])
	if err1 != nil || err2 != nil || ma < 0 || ma > 255 || mi < 0 || mi > 255 {
		return 0, 0, fmt.Errorf("revision %q", s)
	}
	return ma, mi, nil
}

// tagValue builds the Go value gologix serves for a fixture tag: gologix
// derives the CIP type of a reply from the Go type (GoVarToCIPType), so a
// DINT is an int32, a DINT array an []int32. USINT and ULINT have no Go
// type of their own there (uint8 is BYTE, uint64 LWORD); BOOL arrays are
// DWORDs in Logix, which gologix's server does not model.
func tagValue(t fixtureTag) (any, error) {
	n := t.Count
	if n == 0 {
		n = 1
	}
	if t.Type == "STRING" {
		if len(t.Strings) != n {
			return nil, errors.New("STRING tag without strings")
		}
		if t.Count == 0 {
			return t.Strings[0], nil
		}
		return append([]string{}, t.Strings...), nil
	}
	vals := make([]float64, n)
	switch {
	case t.Pattern != nil:
		for i := range vals {
			vals[i] = float64(i)*t.Pattern.Multiplier + t.Pattern.Offset
		}
	case len(t.Values) == n:
		copy(vals, t.Values)
	default:
		return nil, errors.New("values do not match count")
	}
	scalar := t.Count == 0
	switch t.Type {
	case "BOOL":
		if !scalar {
			return nil, errors.New("BOOL arrays (DWORDs in Logix) are not served by gologix")
		}
		return vals[0] != 0, nil
	case "SINT":
		return typed[int8](vals, scalar), nil
	case "INT":
		return typed[int16](vals, scalar), nil
	case "DINT":
		return typed[int32](vals, scalar), nil
	case "LINT":
		return typed[int64](vals, scalar), nil
	case "UINT":
		return typed[uint16](vals, scalar), nil
	case "UDINT":
		return typed[uint32](vals, scalar), nil
	case "REAL":
		return typed[float32](vals, scalar), nil
	case "LREAL":
		return typed[float64](vals, scalar), nil
	case "USINT", "ULINT":
		return nil, fmt.Errorf("type %s: gologix replies with BYTE/LWORD for it", t.Type)
	}
	return nil, fmt.Errorf("type %s: not served by gologix (no structures)", t.Type)
}

type numeric interface {
	int8 | int16 | int32 | int64 | uint16 | uint32 | float32 | float64
}

func typed[T numeric](vals []float64, scalar bool) any {
	out := make([]T, len(vals))
	for i, v := range vals {
		if _, isFloat := any(out[i]).(float32); !isFloat {
			if _, isDouble := any(out[i]).(float64); !isDouble {
				v = math.Round(v)
			}
		}
		out[i] = T(v)
	}
	if scalar {
		return out[0]
	}
	return out
}

// tagProvider serves the fixture's controller-scoped tags to gologix's
// Server (its CIPEndpoint and TagLister interfaces). It follows gologix's
// MapTagProvider (names without case, "name[N]" element access), keeps the
// fixture's spelling for the tag list, and keeps each tag's type and size:
// a write of another type is refused, a write of n elements to an array
// writes elements 0..n-1.
type tagProvider struct {
	mu    sync.Mutex
	tags  map[string]any    // lower-case name -> Go value
	names map[string]string // lower-case name -> fixture's name
}

func newTagProvider(fx *fixture) *tagProvider {
	p := &tagProvider{tags: map[string]any{}, names: map[string]string{}}
	for _, t := range fx.Tags {
		v, _ := tagValue(t)
		k := strings.ToLower(t.Name)
		p.tags[k] = v
		p.names[k] = t.Name
	}
	return p
}

func splitElement(tag string) (string, int, bool) {
	if !strings.HasSuffix(tag, "]") {
		return tag, 0, false
	}
	i := strings.LastIndex(tag, "[")
	if i < 0 {
		return tag, 0, false
	}
	n, err := strconv.Atoi(tag[i+1 : len(tag)-1])
	if err != nil {
		return tag, 0, false
	}
	return tag[:i], n, true
}

func (p *tagProvider) TagRead(tag string, qty int16) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	base, idx, elem := splitElement(strings.ToLower(tag))
	v, ok := p.tags[base]
	if !ok {
		return nil, fmt.Errorf("tag %s not found", tag)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		if elem {
			return nil, fmt.Errorf("tag %s is not an array", tag)
		}
		return v, nil
	}
	if qty < 1 {
		qty = 1
	}
	end := idx + int(qty)
	if idx < 0 || end > rv.Len() {
		return nil, fmt.Errorf("tag %s: elements %d..%d of %d", tag, idx, end-1, rv.Len())
	}
	if elem && qty == 1 {
		return rv.Index(idx).Interface(), nil
	}
	// A copy: gologix serializes it after the lock is released.
	out := reflect.MakeSlice(rv.Type(), int(qty), int(qty))
	reflect.Copy(out, rv.Slice(idx, end))
	return out.Interface(), nil
}

func (p *tagProvider) TagWrite(tag string, value any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	base, idx, _ := splitElement(strings.ToLower(tag))
	v, ok := p.tags[base]
	if !ok {
		return fmt.Errorf("tag %s not found", tag)
	}
	rv := reflect.ValueOf(v)
	var in []any
	if s, ok := value.([]any); ok {
		in = s
	} else {
		in = []any{value}
	}
	if rv.Kind() != reflect.Slice {
		if len(in) != 1 || reflect.TypeOf(in[0]) != rv.Type() {
			return fmt.Errorf("tag %s: %T written to %T", tag, value, v)
		}
		p.tags[base] = in[0]
		return nil
	}
	et := rv.Type().Elem()
	if idx < 0 || idx+len(in) > rv.Len() {
		return fmt.Errorf("tag %s: %d elements from %d of %d", tag, len(in), idx, rv.Len())
	}
	for i, e := range in {
		if reflect.TypeOf(e) != et {
			return fmt.Errorf("tag %s: %T written to an array of %v", tag, e, et)
		}
		rv.Index(idx + i).Set(reflect.ValueOf(e))
	}
	return nil
}

func (p *tagProvider) IORead() ([]byte, error) { return nil, errors.New("no I/O on the tag path") }
func (p *tagProvider) IOWrite(items []gologix.CIPItem) error {
	return errors.New("no I/O on the tag path")
}

func (p *tagProvider) TagList() []gologix.ServerTagInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]gologix.ServerTagInfo, 0, len(p.tags))
	for k, v := range p.tags {
		t, _ := gologix.GoVarToCIPType(v)
		out = append(out, gologix.ServerTagInfo{Name: p.names[k], Type: t})
	}
	return out
}

// ioProvider serves the fixture's assemblies as gologix's Class 1 I/O: the
// input assembly is produced at the RPI (IORead), O->T data lands in the
// output assembly (IOWrite) and, with echoOutputToInput, in the input
// assembly too. gologix resolves an I/O connection by the first two octets
// of its connection path (here the Assembly class segment 20 04), not by
// its connection points.
type ioProvider struct {
	mu     sync.Mutex
	input  []byte
	output []byte
	echo   bool
}

func newIOProvider(fx *fixture) *ioProvider {
	p := &ioProvider{echo: fx.EchoOutputToInput}
	for _, a := range fx.Assemblies {
		switch a.Kind {
		case "input":
			p.input = make([]byte, a.Size)
		case "output":
			p.output = make([]byte, a.Size)
		}
	}
	return p
}

func (p *ioProvider) IORead() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte{}, p.input...), nil
}

// IOWrite takes one O->T datagram: items[1] is the connected data item,
// sequence count (2), run/idle header (4), data (as gologix's IOProvider).
func (p *ioProvider) IOWrite(items []gologix.CIPItem) error {
	if len(items) != 2 {
		return fmt.Errorf("%d items", len(items))
	}
	d := items[1].Data
	if len(d) < 6 {
		return nil // a heartbeat: no data
	}
	data := d[6:]
	p.mu.Lock()
	defer p.mu.Unlock()
	copy(p.output, data)
	if p.echo {
		copy(p.input, data)
	}
	return nil
}

func (p *ioProvider) TagRead(tag string, qty int16) (any, error) {
	return nil, errors.New("no tags on the I/O path")
}
func (p *ioProvider) TagWrite(tag string, value any) error {
	return errors.New("no tags on the I/O path")
}

// srvLogger is gologix's server log, on stderr.
func srvLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func ownIP() string {
	c, err := net.Dial("udp", "10.255.255.255:1")
	if err == nil {
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).IP.String()
	}
	return "0.0.0.0"
}

func serve(argv []string) int {
	fs := flag.NewFlagSet("ethernetip-interop server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fixturePath := fs.String("fixture", "/fixtures/"+serverFixtures[0]+"/fixture.json", "fixture to serve")
	var port uint64 = 44818
	fs.Var(numFlag{&port}, "port", "TCP port of the encapsulation layer")
	readyFile := fs.String("ready-file", defaultReadyFile, "readiness file; empty: none")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() > 0 || port == 0 || port > 65535 {
		return usage("invalid arguments")
	}
	fx, err := loadFixture(*fixturePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture %s: %v\n", *fixturePath, err)
		return 4
	}

	// gologix's Server runs in a child process: a panic in one of its
	// connection goroutines (a malformed or unexpected request) ends the
	// whole process, and the contract wants exit 1 and no "stopped" event
	// then, not Go's exit 2.
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "executable: %v\n", err)
		return 1
	}
	child := exec.Command(exe, targetCommand, *fixturePath, strconv.FormatUint(port, 10))
	child.Stdout, child.Stderr = os.Stderr, os.Stderr
	child.SysProcAttr = childAttr()
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start target: %v\n", err)
		return 1
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	removeReady := func() {
		if *readyFile != "" {
			os.Remove(*readyFile)
		}
	}
	stop := func() int {
		removeReady()
		_ = child.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = child.Process.Kill()
			<-exited
		}
		emit(map[string]any{"event": "stopped", "adapter": adapterName})
		return 0
	}

	// Readiness: the target accepts a TCP connection.
	deadline := time.Now().Add(30 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		select {
		case err := <-exited:
			fmt.Fprintf(os.Stderr, "gologix target ended: %v\n", err)
			return 1
		case <-sigs:
			return stop()
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "gologix target does not accept connections")
			_ = child.Process.Kill()
			return 1
		}
	}
	if *readyFile != "" {
		if err := os.WriteFile(*readyFile, nil, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "ready file: %v\n", err)
			_ = child.Process.Kill()
			return 1
		}
	}
	emit(map[string]any{"event": "ready", "adapter": adapterName, "address": fmt.Sprintf("%s:%d", ownIP(), port), "fixture": fx.Name})

	select {
	case <-sigs:
		return stop()
	case err := <-exited:
		fmt.Fprintf(os.Stderr, "gologix target ended: %v\n", err)
		removeReady()
		return 1
	}
}

// targetCommand runs gologix's Server in the child process of "server".
const targetCommand = "__gologix-target"

// runTarget serves the fixture with gologix's Server until the process is
// terminated. Arguments: fixture path, port (checked by "server").
func runTarget(args []string) int {
	if len(args) != 2 {
		return 2
	}
	fx, err := loadFixture(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture %s: %v\n", args[0], err)
		return 4
	}
	port, err := strconv.Atoi(args[1])
	if err != nil {
		return 2
	}

	// Tags at backplane port 1, slot <slot> (gologix's usual "1,0"), and at
	// the Message Router (20 02) for a Forward_Open without a route. I/O at
	// the Assembly class (20 04), where an I/O connection path starts.
	router := gologix.NewRouter()
	tags := newTagProvider(fx)
	router.Handle([]byte{1, byte(fx.Slot)}, tags)
	router.Handle([]byte{0x20, 0x02}, tags)
	if hasClass1(fx) {
		router.Handle([]byte{0x20, 0x04}, newIOProvider(fx))
	}
	srv := gologix.NewServer(router)
	srv.Logger = srvLogger()
	ma, mi, _ := revision(fx.Identity.Revision)
	// gologix keeps the Identity attributes as int16/uint32/string.
	srv.Attributes[1] = int16(uint16(*fx.Identity.VendorID))
	srv.Attributes[2] = int16(uint16(*fx.Identity.DeviceType))
	srv.Attributes[3] = int16(uint16(*fx.Identity.ProductCode))
	srv.Attributes[4] = int16(uint16(ma | mi<<8))
	srv.Attributes[6] = *fx.Identity.SerialNumber
	srv.Attributes[7] = fx.Identity.ProductName

	tcp, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen tcp: %v\n", err)
		return 1
	}
	udp, err := net.ListenPacket("udp", "0.0.0.0:2222")
	if err != nil {
		tcp.Close()
		fmt.Fprintf(os.Stderr, "listen udp 2222: %v\n", err)
		return 1
	}
	err = srv.Serve(tcp, udp)
	fmt.Fprintf(os.Stderr, "gologix server: %v\n", err)
	return 1
}
