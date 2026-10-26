// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danomagnum/gologix"
)

// clientArgs are the flags of every client operation (an operation ignores
// the ones it does not use; any other flag is a usage error).
type clientArgs struct {
	host      string
	port      uint
	timeoutMS int
	transport string
	class     uint64
	instance  uint64
	attribute uint64
	data      []byte
	connected bool
	paths     []string
	tag       string
	tags      string
	count     int
	typ       string
	values    string
	route     string
	routeSet  bool
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// number parses decimal or 0x hexadecimal.
func number(s string) (uint64, error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return strconv.ParseUint(s[2:], 16, 32)
	}
	return strconv.ParseUint(s, 10, 32)
}

type numFlag struct{ v *uint64 }

func (n numFlag) String() string { return "" }
func (n numFlag) Set(s string) error {
	v, err := number(s)
	if err != nil {
		return err
	}
	*n.v = v
	return nil
}

func parseClientArgs(op string, argv []string) (*clientArgs, error) {
	a := &clientArgs{}
	fs := flag.NewFlagSet("ethernetip-interop client "+op, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var port, timeout, count uint64 = 44818, 5000, 1
	var data string
	var paths multiFlag
	fs.StringVar(&a.host, "host", "", "target address")
	fs.Var(numFlag{&port}, "port", "encapsulation port")
	fs.Var(numFlag{&timeout}, "timeout-ms", "response timeout")
	fs.StringVar(&a.transport, "transport", "tcp", "tcp (gologix has no UDP)")
	fs.Var(numFlag{&a.class}, "class", "class")
	fs.Var(numFlag{&a.instance}, "instance", "instance")
	fs.Var(numFlag{&a.attribute}, "attribute", "attribute")
	fs.StringVar(&data, "data", "", "hex data")
	fs.BoolVar(&a.connected, "connected", false, "Class 3 (gologix always uses one)")
	fs.Var(&paths, "path", "@CLASS/INSTANCE/ATTRIBUTE (repeatable)")
	fs.StringVar(&a.tag, "tag", "", "tag name")
	fs.StringVar(&a.tags, "tags", "", "tag names, comma-separated")
	fs.Var(numFlag{&count}, "count", "elements")
	fs.StringVar(&a.typ, "type", "", "CIP type")
	fs.StringVar(&a.values, "values", "", "values, comma-separated")
	fs.StringVar(&a.route, "route", "", "route to the controller, e.g. 1/0; empty: none")
	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "route" {
			a.routeSet = true
		}
	})
	if a.host == "" {
		return nil, errors.New("--host is required")
	}
	if port == 0 || port > 65535 {
		return nil, errors.New("--port out of range")
	}
	if timeout == 0 {
		return nil, errors.New("--timeout-ms must be positive")
	}
	a.port, a.timeoutMS, a.count = uint(port), int(timeout), int(count)
	if a.count < 1 || a.count > 65535 {
		return nil, errors.New("--count out of range")
	}
	switch a.transport {
	case "tcp":
	case "udp":
		return nil, errors.New("--transport udp: gologix sends ListIdentity over TCP only")
	default:
		return nil, fmt.Errorf("--transport %q: tcp or udp", a.transport)
	}
	if data != "" {
		b, err := hex.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("--data: %v", err)
		}
		a.data = b
	}
	a.paths = paths
	for _, p := range a.paths {
		if _, err := parseAttrPath(p); err != nil {
			return nil, err
		}
	}
	switch op {
	case "multiple-get":
		if len(a.paths) == 0 {
			return nil, errors.New("--path is required")
		}
	case "read-tag":
		if a.tag == "" {
			return nil, errors.New("--tag is required")
		}
	case "read-tags":
		if a.tags == "" {
			return nil, errors.New("--tags is required")
		}
	case "write-tag":
		if a.tag == "" || a.typ == "" {
			return nil, errors.New("--tag and --type are required")
		}
		if _, err := writeValue(a.typ, a.values); err != nil {
			return nil, err
		}
	}
	if a.typ != "" && op == "read-tag" {
		if _, ok := cipTypes[a.typ]; !ok && a.typ != "STRING" {
			return nil, fmt.Errorf("--type %q", a.typ)
		}
	}
	if _, err := routePath(a.route); err != nil {
		return nil, err
	}
	return a, nil
}

type attrPath struct{ class, instance, attribute uint64 }

func parseAttrPath(s string) (attrPath, error) {
	parts := strings.Split(strings.TrimPrefix(s, "@"), "/")
	if !strings.HasPrefix(s, "@") || len(parts) != 3 {
		return attrPath{}, fmt.Errorf("--path %q: want @CLASS/INSTANCE/ATTRIBUTE", s)
	}
	var v [3]uint64
	for i, p := range parts {
		n, err := number(p)
		if err != nil {
			return attrPath{}, fmt.Errorf("--path %q: %v", s, err)
		}
		v[i] = n
	}
	return attrPath{v[0], v[1], v[2]}, nil
}

// routePath turns "1/0" (or "1,0") into gologix's path bytes.
func routePath(s string) (*bytes.Buffer, error) {
	p, err := gologix.ParsePath(strings.ReplaceAll(s, "/", ","))
	if err != nil {
		return nil, fmt.Errorf("--route %q: %v", s, err)
	}
	return p, nil
}

var cipTypes = map[string]gologix.CIPType{
	"BOOL": gologix.CIPTypeBOOL, "SINT": gologix.CIPTypeSINT, "INT": gologix.CIPTypeINT,
	"DINT": gologix.CIPTypeDINT, "LINT": gologix.CIPTypeLINT, "USINT": gologix.CIPTypeUSINT,
	"UINT": gologix.CIPTypeUINT, "UDINT": gologix.CIPTypeUDINT, "ULINT": gologix.CIPTypeULINT,
	"REAL": gologix.CIPTypeREAL, "LREAL": gologix.CIPTypeLREAL,
}

// typeNames names the CIP elementary types a tag list or a reply reports.
var typeNames = map[gologix.CIPType]string{
	gologix.CIPTypeBOOL: "BOOL", gologix.CIPTypeSINT: "SINT", gologix.CIPTypeINT: "INT",
	gologix.CIPTypeDINT: "DINT", gologix.CIPTypeLINT: "LINT", gologix.CIPTypeUSINT: "USINT",
	gologix.CIPTypeUINT: "UINT", gologix.CIPTypeUDINT: "UDINT", gologix.CIPTypeULINT: "ULINT",
	gologix.CIPTypeREAL: "REAL", gologix.CIPTypeLREAL: "LREAL", gologix.CIPTypeBYTE: "BYTE",
	gologix.CIPTypeWORD: "WORD", gologix.CIPTypeDWORD: "DWORD", gologix.CIPTypeLWORD: "LWORD",
}

// writeValue converts --values to the Go value gologix's Write sends: the
// Go type selects the CIP type (gologix's GoVarToCIPType). Several values
// are a slice (Write Tag of that many elements).
func writeValue(typ, text string) (any, error) {
	if typ == "STRING" {
		return text, nil
	}
	if typ == "JSON" {
		return nil, errors.New("--type JSON: gologix writes structures only from Go types known at compile time")
	}
	if _, ok := cipTypes[typ]; !ok {
		return nil, fmt.Errorf("--type %q", typ)
	}
	parts := strings.Split(text, ",")
	if text == "" {
		return nil, errors.New("--values is required")
	}
	vals := make([]any, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		var v any
		var err error
		switch typ {
		case "BOOL":
			switch strings.ToLower(p) {
			case "1", "true":
				v = true
			case "0", "false":
				v = false
			default:
				err = fmt.Errorf("not a BOOL")
			}
		case "REAL":
			var f float64
			f, err = strconv.ParseFloat(p, 32)
			v = float32(f)
		case "LREAL":
			v, err = strconv.ParseFloat(p, 64)
		case "SINT":
			var n int64
			n, err = strconv.ParseInt(p, 0, 8)
			v = int8(n)
		case "INT":
			var n int64
			n, err = strconv.ParseInt(p, 0, 16)
			v = int16(n)
		case "DINT":
			var n int64
			n, err = strconv.ParseInt(p, 0, 32)
			v = int32(n)
		case "LINT":
			v, err = strconv.ParseInt(p, 0, 64)
		case "USINT":
			var n uint64
			n, err = strconv.ParseUint(p, 0, 8)
			v = uint8(n)
		case "UINT":
			var n uint64
			n, err = strconv.ParseUint(p, 0, 16)
			v = uint16(n)
		case "UDINT":
			var n uint64
			n, err = strconv.ParseUint(p, 0, 32)
			v = uint32(n)
		case "ULINT":
			v, err = strconv.ParseUint(p, 0, 64)
		}
		if err != nil {
			return nil, fmt.Errorf("--values %q: %v", p, err)
		}
		vals[i] = v
	}
	if len(vals) == 1 {
		return vals[0], nil
	}
	// A typed slice: gologix sends len(slice) elements.
	switch typ {
	case "BOOL":
		return toSlice[bool](vals), nil
	case "REAL":
		return toSlice[float32](vals), nil
	case "LREAL":
		return toSlice[float64](vals), nil
	case "SINT":
		return toSlice[int8](vals), nil
	case "INT":
		return toSlice[int16](vals), nil
	case "DINT":
		return toSlice[int32](vals), nil
	case "LINT":
		return toSlice[int64](vals), nil
	case "USINT":
		return toSlice[uint8](vals), nil
	case "UINT":
		return toSlice[uint16](vals), nil
	case "UDINT":
		return toSlice[uint32](vals), nil
	default:
		return toSlice[uint64](vals), nil
	}
}

func toSlice[T any](vals []any) []T {
	out := make([]T, len(vals))
	for i, v := range vals {
		out[i] = v.(T)
	}
	return out
}

type result map[string]any

func newResult(op string) result {
	return result{"schemaVersion": "1.0", "adapter": adapterName, "operation": op, "ok": false, "error": nil}
}

func (r result) fail(code string, err error) int {
	r["ok"] = false
	r["error"] = map[string]string{"code": code, "message": err.Error()}
	emit(r)
	return 1
}

func (r result) done() int {
	r["ok"] = true
	r["error"] = nil
	emit(r)
	return 0
}

// errorCode classifies an error of gologix for the result document.
func errorCode(err error) string {
	var ne net.Error
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &ne) && ne.Timeout()) {
		return "timeout"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return "connection-lost"
	}
	return "failed"
}

// replyError classifies an error of a request: gologix disconnects on a
// transport error; with the connection still up the reply was unusable.
func replyError(c *gologix.Client, err error) string {
	if c.Connected() {
		return "protocol"
	}
	return errorCode(err)
}

func runClient(op string, argv []string) int {
	a, err := parseClientArgs(op, argv)
	if err != nil {
		return usage("%v", err)
	}
	r := newResult(op)
	timeout := time.Duration(a.timeoutMS) * time.Millisecond

	c := gologix.NewClient(a.host)
	c.Controller.IpAddress = a.host
	c.Controller.Port = a.port
	c.SocketTimeout = timeout
	c.AutoConnect = false
	c.KeepAliveAutoStart = false
	// The route of the Forward_Open: none for the generic operations (an
	// adapter answers at its own address), backplane slot 0 for the Logix
	// ones (gologix's default), unless --route says otherwise.
	route := a.route
	if !a.routeSet {
		switch op {
		case "read-tag", "read-tags", "write-tag", "tag-list":
			route = "1/0"
		}
	}
	c.Controller.Path, _ = routePath(route)

	// gologix opens a session and a Class 3 connection (Large_Forward_Open,
	// Forward_Open if refused) for every operation: it has no unconnected
	// messaging. Its ListIdentity and ListServices go over that TCP session.
	if err := connectWithin(c, timeout); err != nil {
		code := "connect-failed"
		if errorCode(err) == "timeout" {
			code = "timeout"
		}
		return r.fail(code, err)
	}
	defer func() {
		if c.Connected() {
			_ = c.Disconnect()
		}
	}()

	switch op {
	case "list-identity":
		id, err := c.ListIdentity()
		if err != nil {
			return r.fail(replyError(c, err), err)
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, id.SocketAddress.Address)
		r["identity"] = map[string]any{
			"vendorId": uint16(id.Vendor), "deviceType": uint16(id.DeviceType), "productCode": id.ProductCode,
			"revision": fmt.Sprintf("%d.%d", id.Revision&0xFF, id.Revision>>8), "status": id.Status,
			"serialNumber": id.SerialNumber, "productName": id.ProductName, "state": id.State,
			"socketAddress": fmt.Sprintf("%s:%d", ip, id.SocketAddress.Port),
		}
		return r.done()
	case "list-services":
		svcs, err := c.ListServices()
		if err != nil {
			return r.fail(replyError(c, err), err)
		}
		out := make([]map[string]any, 0, len(svcs))
		for _, s := range svcs {
			out = append(out, map[string]any{"name": s.Name, "version": s.EncapProtocolVersion, "capabilities": uint16(s.Capabilities)})
		}
		r["services"] = out
		return r.done()
	case "get-attribute", "set-attribute", "get-attributes-all":
		var service gologix.CIPService
		var path *bytes.Buffer
		var data []byte
		switch op {
		case "get-attribute":
			service = gologix.CIPService_GetAttributeSingle
			path, err = gologix.Serialize(gologix.CIPClass(a.class), gologix.CIPInstance(a.instance), gologix.CIPAttribute(a.attribute))
		case "set-attribute":
			service = gologix.CIPService_SetAttributeSingle
			path, err = gologix.Serialize(gologix.CIPClass(a.class), gologix.CIPInstance(a.instance), gologix.CIPAttribute(a.attribute))
			data = a.data
		default:
			service = gologix.CIPService_GetAttributeAll
			path, err = gologix.Serialize(gologix.CIPClass(a.class), gologix.CIPInstance(a.instance))
		}
		if err != nil {
			return r.fail("failed", err)
		}
		if data == nil {
			data = []byte{}
		}
		rep, err := generic(c, service, path.Bytes(), data)
		if err != nil {
			return r.fail(replyError(c, err), err)
		}
		r["status"], r["extendedStatus"], r["data"] = rep.status, rep.ext, hex.EncodeToString(rep.data)
		return r.done()
	case "multiple-get":
		return multipleGet(c, a, r)
	case "read-tag":
		return readTag(c, a, r)
	case "read-tags":
		return readTags(c, a, r)
	case "write-tag":
		return writeTag(c, a, r)
	case "tag-list":
		return tagList(c, r)
	}
	return usage("unknown operation %s", op)
}

// connectWithin runs gologix's Connect, bounded: Connect dials with the
// socket timeout and reads every reply with it.
func connectWithin(c *gologix.Client, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- c.Connect() }()
	select {
	case err := <-done:
		return err
	case <-time.After(4*timeout + time.Second):
		return fmt.Errorf("connect: %w", os.ErrDeadlineExceeded)
	}
}

type genericReply struct {
	status int
	ext    []int
	data   []byte
}

// generic sends one request with gologix's GenericCIPMessage (over the Class
// 3 connection) and parses the reply's general status, additional status
// and data from the reply item gologix returns.
func generic(c *gologix.Client, service gologix.CIPService, path, data []byte) (genericReply, error) {
	item, err := c.GenericCIPMessage(service, path, data)
	if item == nil {
		if err == nil {
			err = errors.New("no reply")
		}
		return genericReply{}, err
	}
	// Connected data item: sequence count (2), reply service, reserved,
	// general status, additional status size (words), additional status, data.
	b := item.Data
	if len(b) < 6 {
		return genericReply{}, fmt.Errorf("reply too short: %x", b)
	}
	rep := genericReply{status: int(b[4]), ext: []int{}}
	n := int(b[5])
	if len(b) < 6+2*n {
		return genericReply{}, fmt.Errorf("reply too short for its additional status: %x", b)
	}
	for i := 0; i < n; i++ {
		rep.ext = append(rep.ext, int(binary.LittleEndian.Uint16(b[6+2*i:])))
	}
	rep.data = append([]byte{}, b[6+2*n:]...)
	return rep, nil
}

// multipleGet sends one Multiple Service Packet (0x0A to @2/1) of
// Get_Attribute_Single requests with GenericCIPMessage. gologix packs
// Multiple Service Packets only for tag reads; the adapter packs this one.
func multipleGet(c *gologix.Client, a *clientArgs, r result) int {
	reqs := make([][]byte, len(a.paths))
	for i, s := range a.paths {
		p, _ := parseAttrPath(s)
		path, err := gologix.Serialize(gologix.CIPClass(p.class), gologix.CIPInstance(p.instance), gologix.CIPAttribute(p.attribute))
		if err != nil {
			return r.fail("failed", err)
		}
		reqs[i] = append([]byte{byte(gologix.CIPService_GetAttributeSingle), byte(path.Len() / 2)}, path.Bytes()...)
	}
	var body bytes.Buffer
	n := len(reqs)
	binary.Write(&body, binary.LittleEndian, uint16(n))
	off := 2 + 2*n
	for _, q := range reqs {
		binary.Write(&body, binary.LittleEndian, uint16(off))
		off += len(q)
	}
	for _, q := range reqs {
		body.Write(q)
	}
	router, _ := gologix.Serialize(gologix.CipObject_MessageRouter, gologix.CIPInstance(1))
	rep, err := generic(c, gologix.CIPService_MultipleService, router.Bytes(), body.Bytes())
	if err != nil {
		return r.fail(replyError(c, err), err)
	}
	r["status"] = rep.status
	replies := []map[string]any{}
	d := rep.data
	if len(d) >= 2 {
		cnt := int(binary.LittleEndian.Uint16(d))
		offs := make([]int, 0, cnt)
		for i := 0; i < cnt && 2+2*i+2 <= len(d); i++ {
			offs = append(offs, int(binary.LittleEndian.Uint16(d[2+2*i:])))
		}
		for i, o := range offs {
			end := len(d)
			if i+1 < len(offs) {
				end = offs[i+1]
			}
			if o+4 > len(d) || end > len(d) || o+4 > end {
				return r.fail("protocol", fmt.Errorf("bad Multiple Service reply: %x", d))
			}
			sz := int(d[o+3])
			start := o + 4 + 2*sz
			if start > end {
				start = end
			}
			replies = append(replies, map[string]any{"status": int(d[o+2]), "data": hex.EncodeToString(d[start:end])})
		}
	}
	r["replies"] = replies
	return r.done()
}

// cipStatusByText recovers the general status from gologix's error texts,
// which carry it as CIPStatus.String().
var cipStatusByText = func() map[string]int {
	m := map[string]int{}
	seen := map[string]int{}
	for i := 0; i < 256; i++ {
		s := gologix.CIPStatus(i).String()
		seen[s]++
		m[s] = i
	}
	for s, n := range seen {
		if n > 1 {
			delete(m, s)
		}
	}
	return m
}()

var (
	reReadStatus  = regexp.MustCompile(`returned status (.*)$`)
	reWriteStatus = regexp.MustCompile(`got status (\d+):(\d+):(\d+) `)
	reMultiStatus = regexp.MustCompile(`\. Status (\d+)$`)
)

// tagStatus maps an error of gologix's tag services to a CIP general
// status: the one gologix names in its error, -1 when it names none.
func tagStatus(err error) int {
	if err == nil {
		return 0
	}
	msg := err.Error()
	if m := reReadStatus.FindStringSubmatch(msg); m != nil {
		if s, ok := cipStatusByText[m[1]]; ok {
			return s
		}
	}
	if m := reWriteStatus.FindStringSubmatch(msg); m != nil {
		if s, err := strconv.Atoi(m[1]); err == nil && s <= 255 {
			return s
		}
	}
	if m := reMultiStatus.FindStringSubmatch(msg); m != nil {
		if s, err := strconv.Atoi(m[1]); err == nil && s <= 255 {
			return s
		}
	}
	return -1
}

// tagFailure reports an error of a tag service: a failed operation when
// the connection is gone (gologix disconnects on a transport error),
// otherwise an answered request with the status gologix reports.
func tagFailure(c *gologix.Client, r result, err error) (int, bool) {
	if !c.Connected() {
		return r.fail(errorCode(err), err), true
	}
	return 0, false
}

func jsonValue(v any) any {
	switch x := v.(type) {
	case []byte:
		return hex.EncodeToString(x)
	case float32:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		// The shortest decimal that is the float32: 21.5, not 21.5000000001.
		g, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'g', -1, 32), 64)
		return g
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return x
	}
	return v
}

func goTypeName(v any) string {
	t, _ := gologix.GoVarToCIPType(v)
	if n, ok := typeNames[t]; ok {
		return n
	}
	return ""
}

func readTag(c *gologix.Client, a *clientArgs, r result) int {
	dt := gologix.CIPTypeUnknown
	if a.typ == "STRING" {
		dt = gologix.CIPTypeSTRING
	} else if a.typ != "" {
		dt = cipTypes[a.typ]
	}
	v, err := c.ReadSingle(a.tag, dt, uint16(a.count))
	if err != nil {
		if code, failed := tagFailure(c, r, err); failed {
			return code
		}
		r["status"], r["values"], r["errorText"] = tagStatus(err), []any{}, err.Error()
		return r.done()
	}
	vals, typ := tagValues(v, a.typ, a.count)
	r["status"], r["values"], r["type"], r["errorText"] = 0, vals, typ, nil
	return r.done()
}

// tagValues turns what gologix's ReadSingle returns into the document's
// values and the type name: a scalar, []any of elements, a STRING as
// bytes, a structure as its raw bytes (hex).
func tagValues(v any, typ string, count int) ([]any, any) {
	if typ == "STRING" {
		var out []any
		switch x := v.(type) {
		case []byte:
			out = []any{string(x)}
		case []any:
			for _, e := range x {
				if b, ok := e.([]byte); ok {
					out = append(out, string(b))
				} else {
					out = append(out, fmt.Sprint(e))
				}
			}
		default:
			out = []any{fmt.Sprint(x)}
		}
		return out, "STRING"
	}
	switch x := v.(type) {
	case []byte:
		// A structure: gologix returns its data without the structure handle.
		return []any{hex.EncodeToString(x)}, "STRUCT"
	case []any:
		out := make([]any, len(x))
		name := ""
		for i, e := range x {
			out[i] = jsonValue(e)
			name = goTypeName(e)
		}
		return out, nullable(name)
	}
	return []any{jsonValue(v)}, nullable(goTypeName(v))
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// readTags reads the tags with gologix's ReadList: Multiple Service Packets
// of Read Tag, the types taken from the replies.
func readTags(c *gologix.Client, a *clientArgs, r result) int {
	names := strings.Split(a.tags, ",")
	types := make([]any, len(names))
	elems := make([]int, len(names))
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
		elems[i] = 1
	}
	vals, err := c.ReadList(names, types, elems)
	results := make([]map[string]any, len(names))
	if err != nil {
		if code, failed := tagFailure(c, r, err); failed {
			return code
		}
		// gologix fails the whole list when one reply fails.
		for i, n := range names {
			results[i] = map[string]any{"tag": n, "values": []any{}, "status": tagStatus(err), "errorText": err.Error()}
		}
		r["results"] = results
		return r.done()
	}
	for i, n := range names {
		vs, typ := tagValues(vals[i], "", 1)
		results[i] = map[string]any{"tag": n, "values": vs, "type": typ, "status": 0, "errorText": nil}
	}
	r["results"] = results
	return r.done()
}

func writeTag(c *gologix.Client, a *clientArgs, r result) int {
	v, _ := writeValue(a.typ, a.values)
	err := c.Write(a.tag, v)
	if err != nil {
		if code, failed := tagFailure(c, r, err); failed {
			return code
		}
		r["status"], r["errorText"] = tagStatus(err), err.Error()
		// gologix reports one additional status word: "got status G:N:W".
		if m := reWriteStatus.FindStringSubmatch(err.Error()); m != nil && m[2] != "0" {
			if w, err := strconv.Atoi(m[3]); err == nil && w <= 0xFFFF {
				r["extendedStatus"] = []int{w}
			}
		}
		return r.done()
	}
	r["status"], r["errorText"] = 0, nil
	return r.done()
}

// tagList reads the controller's tag list with gologix's ListAllTags (the
// Symbol object's instance attribute list, and the templates of structure
// tags). gologix does not read the controller's program name.
func tagList(c *gologix.Client, r result) int {
	if err := c.ListAllTags(0); err != nil {
		if !c.Connected() {
			return r.fail(errorCode(err), err)
		}
		return r.fail("failed", err)
	}
	names := make([]string, 0, len(c.KnownTags))
	for k := range c.KnownTags {
		names = append(names, k)
	}
	sort.Strings(names)
	tags := make([]map[string]any, 0, len(names))
	for _, k := range names {
		t := c.KnownTags[k]
		var typ any
		if t.UDT != nil {
			typ = t.UDT.Name
		} else if t.Info.Template_ID() == 0 {
			if n, ok := typeNames[t.Info.Type]; ok {
				typ = n
			}
		}
		dims := []int{}
		for _, d := range t.Array_Order {
			if d > 0 {
				dims = append(dims, d)
			}
		}
		tags = append(tags, map[string]any{"name": t.Name, "type": typ, "dims": dims})
	}
	r["tags"] = tags
	r["programName"] = nil
	return r.done()
}
