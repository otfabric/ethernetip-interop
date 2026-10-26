// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	eip "github.com/loki-os/go-ethernet-ip"
	listid "github.com/loki-os/go-ethernet-ip/messages/listIdentity"
	"github.com/loki-os/go-ethernet-ip/messages/packet"
	"github.com/loki-os/go-ethernet-ip/path"
	"github.com/loki-os/go-ethernet-ip/types"
)

// clientArgs are the flags of every client operation (an operation ignores
// the ones it does not use; any other flag is a usage error).
type clientArgs struct {
	host      string
	port      uint16
	timeout   time.Duration
	class     uint64
	instance  uint64
	attribute uint64
	data      []byte
	connected bool
	paths     []attrPath
	tag       string
	tags      []string
	count     int
	typ       string
	values    string
	slot      uint8
	routed    bool // --route given
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
	var data, transport, tags, route string
	var paths multiFlag
	fs.StringVar(&a.host, "host", "", "target address")
	fs.Var(numFlag{&port}, "port", "encapsulation port")
	fs.Var(numFlag{&timeout}, "timeout-ms", "response timeout")
	fs.StringVar(&transport, "transport", "tcp", "tcp (go-ethernet-ip has no UDP)")
	fs.Var(numFlag{&a.class}, "class", "class")
	fs.Var(numFlag{&a.instance}, "instance", "instance")
	fs.Var(numFlag{&a.attribute}, "attribute", "attribute")
	fs.StringVar(&data, "data", "", "hex data")
	fs.BoolVar(&a.connected, "connected", false, "over a Class 3 connection (Forward_Open)")
	fs.Var(&paths, "path", "@CLASS/INSTANCE/ATTRIBUTE (repeatable)")
	fs.StringVar(&a.tag, "tag", "", "tag name")
	fs.StringVar(&tags, "tags", "", "tag names, comma-separated")
	fs.Var(numFlag{&count}, "count", "elements")
	fs.StringVar(&a.typ, "type", "", "CIP type")
	fs.StringVar(&a.values, "values", "", "values, comma-separated")
	fs.StringVar(&route, "route", "", "1/<slot>: route through Unconnected_Send to that backplane slot")
	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	fs.Visit(func(f *flag.Flag) { a.routed = a.routed || f.Name == "route" })
	if a.host == "" {
		return nil, errors.New("--host is required")
	}
	if port == 0 || port > 65535 {
		return nil, errors.New("--port out of range")
	}
	if timeout == 0 {
		return nil, errors.New("--timeout-ms must be positive")
	}
	if count < 1 || count > 65535 {
		return nil, errors.New("--count out of range")
	}
	a.port, a.timeout, a.count = uint16(port), time.Duration(timeout)*time.Millisecond, int(count)
	switch transport {
	case "tcp":
	case "udp":
		return nil, errors.New("--transport udp: go-ethernet-ip sends ListIdentity and ListServices over its TCP session only")
	default:
		return nil, fmt.Errorf("--transport %q: tcp or udp", transport)
	}
	if data != "" {
		b, err := hex.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("--data: %v", err)
		}
		a.data = b
	}
	for _, s := range paths {
		p, err := parseAttrPath(s)
		if err != nil {
			return nil, err
		}
		a.paths = append(a.paths, p)
	}
	if a.routed {
		// go-ethernet-ip's route is its Config.Slot: port 1 (backplane), one slot.
		parts := strings.Split(strings.ReplaceAll(route, ",", "/"), "/")
		if len(parts) != 2 || parts[0] != "1" {
			return nil, fmt.Errorf("--route %q: go-ethernet-ip routes to 1/<slot> only", route)
		}
		s, err := strconv.ParseUint(parts[1], 10, 8)
		if err != nil {
			return nil, fmt.Errorf("--route %q: %v", route, err)
		}
		a.slot = uint8(s)
	}
	if tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				a.tags = append(a.tags, t)
			}
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
		if _, ok := cipTypes[a.typ]; !ok && a.typ != "" && a.typ != "STRING" {
			return nil, fmt.Errorf("--type %q", a.typ)
		}
	case "read-tags":
		if len(a.tags) == 0 {
			return nil, errors.New("--tags is required")
		}
	case "write-tag":
		if a.tag == "" || a.typ == "" {
			return nil, errors.New("--tag and --type are required")
		}
		if _, err := encodeValue(a.typ, a.values); err != nil {
			return nil, err
		}
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

// CIP elementary type codes (go-ethernet-ip names those up to LREAL).
var cipTypes = map[string]types.UInt{
	"BOOL": eip.BOOL, "SINT": eip.SINT, "INT": eip.INT, "DINT": eip.DINT, "LINT": eip.LINT,
	"USINT": eip.USINT, "UINT": eip.UINT, "UDINT": eip.UDINT, "ULINT": eip.ULINT,
	"REAL": eip.REAL, "LREAL": eip.LREAL,
}

var typeSizes = map[types.UInt]int{
	eip.BOOL: 1, eip.SINT: 1, eip.INT: 2, eip.DINT: 4, eip.LINT: 8, eip.USINT: 1, eip.UINT: 2,
	eip.UDINT: 4, eip.ULINT: 8, eip.REAL: 4, eip.LREAL: 8,
}

// encodeValue turns the one value of --values into the octets of a Write
// Tag (little-endian), or a STRING's text into its characters.
func encodeValue(typ, text string) ([]byte, error) {
	if typ == "STRING" {
		return []byte(text), nil
	}
	if typ == "JSON" {
		return nil, errors.New("--type JSON: go-ethernet-ip writes no structures (only STRING)")
	}
	if _, ok := cipTypes[typ]; !ok {
		return nil, fmt.Errorf("--type %q", typ)
	}
	if text == "" {
		return nil, errors.New("--values is required")
	}
	if strings.Contains(text, ",") {
		return nil, errors.New("--values: go-ethernet-ip writes a tag whole; the adapter writes scalar tags only, one value")
	}
	p := strings.TrimSpace(text)
	var v any
	var err error
	switch typ {
	case "BOOL":
		switch strings.ToLower(p) {
		case "1", "true":
			v = uint8(1)
		case "0", "false":
			v = uint8(0)
		default:
			err = errors.New("not a BOOL")
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
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, v)
	return b.Bytes(), nil
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

// errTimeout: a library call did not return within the timeout.
var errTimeout = errors.New("no reply within the timeout")

// errPanic: go-ethernet-ip panicked on a reply it could not parse.
type errPanic struct{ v any }

func (e errPanic) Error() string { return fmt.Sprintf("go-ethernet-ip could not parse the reply: %v", e.v) }

// errorCode classifies an error for the result document.
func errorCode(err error) string {
	var ne net.Error
	var pe errPanic
	switch {
	case errors.Is(err, errTimeout), errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed),
		errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "connection-lost"
	case errors.As(err, &pe):
		return "protocol"
	}
	return "failed"
}

// session is one go-ethernet-ip TCP session. go-ethernet-ip sets no socket
// deadlines (its connection is unexported), so every call is bounded here.
type session struct {
	tcp     *eip.EIPTCP
	timeout time.Duration
	mu      sync.Mutex // one library call at a time
}

// call runs f with the timeout; a panic of the library is returned as an
// error. A call that times out keeps running in its goroutine: the process
// ends right after with the result document.
func (s *session) call(f func() error) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				done <- errPanic{v}
			}
		}()
		s.mu.Lock()
		defer s.mu.Unlock()
		done <- f()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(s.timeout):
		return errTimeout
	}
}

func (s *session) close() {
	// UnRegisterSession waits for a reply that the specification does not
	// define: most targets close the connection, which ends the wait.
	d := s.timeout
	if d > 500*time.Millisecond {
		d = 500 * time.Millisecond
	}
	t := &session{tcp: s.tcp, timeout: d}
	_ = t.call(s.tcp.UnRegisterSession)
}

func runClient(op string, argv []string) int {
	a, err := parseClientArgs(op, argv)
	if err != nil {
		return usage("%v", err)
	}
	r := newResult(op)

	cfg := eip.DefaultConfig()
	cfg.TCPPort = a.port
	cfg.Slot = a.slot
	tcp, err := eip.NewTCP(a.host, cfg)
	if err != nil {
		return r.fail("connect-failed", err)
	}
	s := &session{tcp: tcp, timeout: a.timeout}
	// Connect: TCP connection and RegisterSession.
	if err := s.call(tcp.Connect); err != nil {
		code := "connect-failed"
		if errorCode(err) == "timeout" {
			code = "timeout"
		}
		return r.fail(code, err)
	}
	defer s.close()
	if a.connected {
		// Forward_Open (0x54), Class 3, connection path 1/<slot>, @2/1.
		// go-ethernet-ip does not look at the reply's status.
		if err := s.call(tcp.ForwardOpen); err != nil {
			return r.fail(errorCode(err), fmt.Errorf("Forward_Open: %w", err))
		}
	}

	switch op {
	case "list-identity":
		return listIdentity(s, r)
	case "list-services":
		return listServices(s, r)
	case "get-attribute", "set-attribute", "get-attributes-all":
		var service types.USInt
		var p []byte
		var data []byte
		switch op {
		case "get-attribute":
			service, p = packet.ServiceGetAttributeSingle, attributePath(a.class, a.instance, a.attribute)
		case "set-attribute":
			service, p, data = packet.ServiceSetAttributeSingle, attributePath(a.class, a.instance, a.attribute), a.data
		default:
			service, p = packet.ServiceGetAttributeAll, packet.Paths(
				path.LogicalBuild(path.LogicalTypeClassID, types.UDInt(a.class), true),
				path.LogicalBuild(path.LogicalTypeInstanceID, types.UDInt(a.instance), true))
		}
		rep, err := generic(s, a, packet.NewMessageRouter(service, p, data))
		if err != nil {
			return r.fail(errorCode(err), err)
		}
		r["status"], r["extendedStatus"], r["data"] = int(rep.GeneralStatus), extStatus(rep), hex.EncodeToString(rep.ResponseData)
		return r.done()
	case "multiple-get":
		return multipleGet(s, a, r)
	case "read-tag":
		return readTag(s, a, r)
	case "read-tags":
		return readTags(s, a, r)
	case "write-tag":
		return writeTag(s, a, r)
	case "tag-list":
		return tagList(s, r)
	}
	return usage("unknown operation %s", op)
}

func listIdentity(s *session, r result) int {
	var id *listid.ListIdentity
	err := s.call(func() error {
		var err error
		id, err = s.tcp.ListIdentity()
		return err
	})
	if err != nil {
		return r.fail(errorCode(err), err)
	}
	if id == nil || len(id.Items) == 0 {
		return r.fail("protocol", errors.New("ListIdentity reply without an identity item"))
	}
	it := id.Items[0]
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, uint32(it.SinAddr))
	r["identity"] = map[string]any{
		"vendorId": int(it.VendorID), "deviceType": int(it.DeviceType), "productCode": int(it.ProductCode),
		"revision": fmt.Sprintf("%d.%d", it.Major, it.Minor), "status": int(it.Status),
		"serialNumber": uint32(it.SerialNumber), "productName": string(it.ProductName),
		"state": int(it.State), "socketAddress": fmt.Sprintf("%s:%d", ip, it.SinPort),
	}
	return r.done()
}

func listServices(s *session, r result) int {
	var out []map[string]any
	err := s.call(func() error {
		ls, err := s.tcp.ListServices()
		if err != nil {
			return err
		}
		out = make([]map[string]any, 0, len(ls.Items))
		for _, it := range ls.Items {
			out = append(out, map[string]any{
				"name":         strings.TrimRight(string(it.Name), "\x00"),
				"version":      int(it.Version),
				"capabilities": int(it.Flags),
			})
		}
		return nil
	})
	if err != nil {
		return r.fail(errorCode(err), err)
	}
	r["services"] = out
	return r.done()
}

// attributePath is @class/instance/attribute in go-ethernet-ip's logical
// segments.
func attributePath(class, instance, attribute uint64) []byte {
	return packet.Paths(
		path.LogicalBuild(path.LogicalTypeClassID, types.UDInt(class), true),
		path.LogicalBuild(path.LogicalTypeInstanceID, types.UDInt(instance), true),
		path.LogicalBuild(path.LogicalTypeAttributeID, types.UDInt(attribute), true))
}

// generic sends one request: over the Class 3 connection with --connected
// (go-ethernet-ip's Send: SendUnitData), through Unconnected_Send to
// 1/<slot> with --route (Send), otherwise as a direct unconnected request
// (SendRRData of an unconnected message).
func generic(s *session, a *clientArgs, mr *packet.MessageRouterRequest) (*packet.MessageRouterResponse, error) {
	var sd *packet.SpecificData
	err := s.call(func() error {
		var err error
		if a.connected || a.routed {
			sd, err = s.tcp.Send(mr)
		} else {
			sd, err = s.tcp.SendRRData(packet.NewUCMM(mr), 10)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return replyOf(sd)
}

// replyOf decodes the message router reply of a SendRRData / SendUnitData
// reply (go-ethernet-ip's MessageRouterResponse).
func replyOf(sd *packet.SpecificData) (*packet.MessageRouterResponse, error) {
	if sd == nil || sd.Packet == nil || len(sd.Packet.Items) < 2 {
		return nil, errors.New("reply without a data item")
	}
	d := sd.Packet.Items[1].Data
	if len(d) < 4 || len(d) < 4+2*int(d[3]) {
		return nil, fmt.Errorf("reply too short: %x", d)
	}
	rep := &packet.MessageRouterResponse{}
	rep.Decode(d)
	return rep, nil
}

func extStatus(rep *packet.MessageRouterResponse) []int {
	out := []int{}
	for i := 0; i+1 < len(rep.AdditionalStatus); i += 2 {
		out = append(out, int(binary.LittleEndian.Uint16(rep.AdditionalStatus[i:])))
	}
	return out
}

// multipleGet sends one Multiple Service Packet (0x0A to @2/1) of
// Get_Attribute_Single requests. go-ethernet-ip packs Multiple Service
// Packets only for its tag groups (unexported), so the adapter packs this
// one the same way and sends it like any other request.
func multipleGet(s *session, a *clientArgs, r result) int {
	reqs := make([][]byte, len(a.paths))
	for i, p := range a.paths {
		reqs[i] = packet.NewMessageRouter(packet.ServiceGetAttributeSingle, attributePath(p.class, p.instance, p.attribute), nil).Encode()
	}
	var body bytes.Buffer
	n := len(reqs)
	_ = binary.Write(&body, binary.LittleEndian, uint16(n))
	off := 2 + 2*n
	for _, q := range reqs {
		_ = binary.Write(&body, binary.LittleEndian, uint16(off))
		off += len(q)
	}
	for _, q := range reqs {
		body.Write(q)
	}
	router := packet.Paths(path.LogicalBuild(path.LogicalTypeClassID, 0x02, true), path.LogicalBuild(path.LogicalTypeInstanceID, 0x01, true))
	rep, err := generic(s, a, packet.NewMessageRouter(packet.ServiceMultipleServicePacket, router, body.Bytes()))
	if err != nil {
		return r.fail(errorCode(err), err)
	}
	r["status"] = int(rep.GeneralStatus)
	replies := []map[string]any{}
	d := rep.ResponseData
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
			start := o + 4 + 2*int(d[o+3])
			if start > end {
				start = end
			}
			replies = append(replies, map[string]any{"status": int(d[o+2]), "data": hex.EncodeToString(d[start:end])})
		}
	}
	r["replies"] = replies
	return r.done()
}

// ---- Logix tags ----

// go-ethernet-ip keeps a tag's value and dimensions in unexported fields
// and exposes them only through accessors of the first element; the
// adapter reads them with reflect.
func tagValue(t *eip.Tag) []byte {
	return reflect.ValueOf(t).Elem().FieldByName("value").Bytes()
}

func tagDims(t *eip.Tag) []int {
	v := reflect.ValueOf(t).Elem()
	dims := []int{}
	for _, f := range []string{"dim1Len", "dim2Len", "dim3Len"} {
		if n := int(v.FieldByName(f).Uint()); n > 0 {
			dims = append(dims, n)
		}
	}
	return dims
}

func isString(typ types.UInt) bool { return typ&0x0FFF == 0x0FCE }

// typeName names a tag's type: the elementary type, STRING, or null.
func typeName(typ types.UInt) any {
	if typ&0x8000 == 0 {
		if n, ok := eip.TypeMap[typ&0x00FF]; ok && typ&0x00FF != 0 {
			return n
		}
		return nil
	}
	if isString(typ) {
		return "STRING"
	}
	return nil
}

func jsonFloat(f float64, bits int) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	if bits == 32 {
		// The shortest decimal that is the float32: 21.5, not 21.5000000001.
		g, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'g', -1, 32), 64)
		return g
	}
	return f
}

// decodeElements decodes up to n elements of an elementary type from the
// octets go-ethernet-ip read.
func decodeElements(code types.UInt, b []byte, n int) []any {
	size := typeSizes[code]
	out := []any{}
	for i := 0; i < n && size > 0 && (i+1)*size <= len(b); i++ {
		e := b[i*size:]
		var v any
		switch code {
		case eip.BOOL:
			v = e[0] != 0
		case eip.SINT:
			v = int8(e[0])
		case eip.USINT:
			v = e[0]
		case eip.INT:
			v = int16(binary.LittleEndian.Uint16(e))
		case eip.UINT:
			v = binary.LittleEndian.Uint16(e)
		case eip.DINT:
			v = int32(binary.LittleEndian.Uint32(e))
		case eip.UDINT:
			v = binary.LittleEndian.Uint32(e)
		case eip.LINT:
			v = int64(binary.LittleEndian.Uint64(e))
		case eip.ULINT:
			v = binary.LittleEndian.Uint64(e)
		case eip.REAL:
			v = jsonFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(e))), 32)
		case eip.LREAL:
			v = jsonFloat(math.Float64frombits(binary.LittleEndian.Uint64(e)), 64)
		}
		out = append(out, v)
	}
	return out
}

// tagResult turns what a read of n elements left in a tag into values and
// a type name: elementary types decoded (ok: all n were there), one STRING
// with go-ethernet-ip's String(); structures and STRING arrays as one hex
// value of all the octets read, type null.
func tagResult(t *eip.Tag, n int) (vals []any, typ any, ok bool) {
	b := tagValue(t)
	switch {
	case t.Type&0x8000 == 0 && typeSizes[t.Type&0x00FF] > 0:
		vals = decodeElements(t.Type&0x00FF, b, n)
		return vals, typeName(t.Type), len(vals) == n
	case isString(t.Type) && n == 1:
		return []any{t.String()}, "STRING", true
	}
	return []any{hex.EncodeToString(b)}, nil, true
}

const noValue = "go-ethernet-ip reports no value: it drops the status of a refused read"

// allTags reads the controller's tag list (go-ethernet-ip's AllTags: the
// Symbol object's Get_Instance_Attribute_List, through Unconnected_Send).
func allTags(s *session) (map[string]*eip.Tag, error) {
	var tags map[string]*eip.Tag
	err := s.call(func() error {
		var err error
		tags, err = s.tcp.AllTags()
		return err
	})
	if err != nil {
		return nil, err
	}
	// AllTags does not look at the reply's status: a refused request
	// (Unconnected_Send to a missing slot, an unknown class) is parsed as
	// tag entries, which then have no name.
	for _, t := range tags {
		if t.Name() == "" {
			return nil, errPanic{"a tag without a name: the tag list request was refused or the reply is malformed"}
		}
	}
	return tags, nil
}

// lookup finds a tag of the tag list by name (Logix names are
// case-insensitive).
func lookup(tags map[string]*eip.Tag, name string) *eip.Tag {
	if t := tags[name]; t != nil {
		return t
	}
	for k, t := range tags {
		if strings.EqualFold(k, name) {
			return t
		}
	}
	return nil
}

// readTag reads one element by name with InitializeTag (Read Tag, symbolic
// path, count 1), or with --count > 1 the tag of the tag list (Read Tag of
// all its elements, by instance) and reports the first --count.
func readTag(s *session, a *clientArgs, r result) int {
	var t *eip.Tag
	n := 1
	if a.count == 1 {
		t = new(eip.Tag)
		err := s.call(func() error {
			// InitializeTag reads the tag and ignores every error; read
			// again to see a transport error.
			s.tcp.InitializeTag(a.tag, t)
			if t.Type == 0 {
				return t.Read()
			}
			return nil
		})
		if err != nil {
			return r.fail(errorCode(err), err)
		}
	} else {
		tags, err := allTags(s)
		if err != nil {
			return r.fail(errorCode(err), fmt.Errorf("tag list: %w", err))
		}
		if t = lookup(tags, a.tag); t == nil {
			r["status"], r["values"], r["type"], r["errorText"] = -1, []any{}, nil, "not in the tag list: go-ethernet-ip reads arrays by instance"
			return r.done()
		}
		if err := s.call(t.Read); err != nil {
			return r.fail(errorCode(err), err)
		}
		n = a.count
	}
	if len(tagValue(t)) == 0 {
		r["status"], r["values"], r["type"], r["errorText"] = -1, []any{}, nil, noValue
		return r.done()
	}
	vals, typ, ok := tagResult(t, n)
	if !ok {
		r["status"], r["values"], r["type"], r["errorText"] = -1, vals, typ, fmt.Sprintf("the tag has %d elements", len(vals))
		return r.done()
	}
	r["status"], r["values"], r["type"], r["errorText"] = 0, vals, typ, nil
	return r.done()
}

// readTags reads the tags with go-ethernet-ip's TagGroup: one Multiple
// Service Packet of Read Tag requests by instance, so the tags come from
// the tag list.
func readTags(s *session, a *clientArgs, r result) int {
	tags, err := allTags(s)
	if err != nil {
		return r.fail(errorCode(err), fmt.Errorf("tag list: %w", err))
	}
	group := eip.NewTagGroup(new(sync.Mutex))
	found := make([]*eip.Tag, len(a.tags))
	for i, name := range a.tags {
		if found[i] = lookup(tags, name); found[i] != nil {
			group.Add(found[i])
		}
	}
	if err := s.call(group.Read); err != nil {
		return r.fail(errorCode(err), err)
	}
	results := make([]map[string]any, len(a.tags))
	for i, name := range a.tags {
		t := found[i]
		switch {
		case t == nil:
			results[i] = map[string]any{"tag": name, "values": []any{}, "status": -1, "errorText": "not in the tag list"}
		case len(tagValue(t)) == 0:
			results[i] = map[string]any{"tag": name, "values": []any{}, "status": -1, "errorText": noValue}
		default:
			n := 1
			for _, d := range tagDims(t) {
				n *= d
			}
			vals, typ, _ := tagResult(t, n)
			results[i] = map[string]any{"tag": name, "values": vals, "type": typ, "status": 0, "errorText": nil}
		}
	}
	r["results"] = results
	return r.done()
}

// writeTag writes a scalar tag of the tag list with go-ethernet-ip's Write
// (Write Tag by instance, of the type in the tag list): the value's octets
// through SetString / SetInt32, which only store them. A STRING tag is
// written as go-ethernet-ip does: LEN and DATA members in one Multiple
// Service Packet.
func writeTag(s *session, a *clientArgs, r result) int {
	value, _ := encodeValue(a.typ, a.values)
	tags, err := allTags(s)
	if err != nil {
		return r.fail(errorCode(err), fmt.Errorf("tag list: %w", err))
	}
	t := lookup(tags, a.tag)
	if t == nil {
		return r.fail("failed", fmt.Errorf("%s is not in the tag list: go-ethernet-ip writes tags by instance", a.tag))
	}
	if len(tagDims(t)) > 0 {
		return r.fail("failed", fmt.Errorf("%s is an array: go-ethernet-ip writes it whole, with its dimension bits in the type", a.tag))
	}
	if got := typeName(t.Type); got != a.typ {
		return r.fail("failed", fmt.Errorf("%s is %v in the tag list, not %s", a.tag, got, a.typ))
	}
	if a.typ == "DINT" {
		t.SetInt32(int32(binary.LittleEndian.Uint32(value)))
	} else {
		t.SetString(string(value))
	}
	if err := s.call(t.Write); err != nil {
		return r.fail(errorCode(err), err)
	}
	// go-ethernet-ip discards the reply of a write: a reply is success.
	r["status"], r["errorText"] = 0, nil
	return r.done()
}

// tagList reads the tag list with go-ethernet-ip's AllTags. It reads no
// program name.
func tagList(s *session, r result) int {
	tags, err := allTags(s)
	if err != nil {
		return r.fail(errorCode(err), err)
	}
	names := make([]string, 0, len(tags))
	for k := range tags {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, k := range names {
		t := tags[k]
		out = append(out, map[string]any{"name": t.Name(), "type": typeName(t.Type), "dims": tagDims(t)})
	}
	r["tags"] = out
	r["programName"] = nil
	return r.done()
}
