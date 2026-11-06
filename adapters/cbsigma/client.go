// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cB-Sigma/enip"
)

// logixRoute is the route of the Logix operations when --route is not
// given: a Logix controller sits behind the backplane, in slot 0.
const logixRoute = "1/0"

type result map[string]any

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// numFlag is a decimal or 0x hexadecimal number up to max.
type numFlag struct {
	v   uint64
	max uint64
}

func (n *numFlag) String() string { return strconv.FormatUint(n.v, 10) }
func (n *numFlag) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, 64)
	if err != nil || v > n.max {
		return fmt.Errorf("%q: a number up to %d", s, n.max)
	}
	n.v = v
	return nil
}

type clientFlags struct {
	host                      string
	port, timeout             int
	transport                 string
	class, instance, attr     numFlag
	data                      string
	connected                 bool
	route                     string
	routeSet                  bool
	paths                     multiFlag
	cfgInst, outInst, inInst  numFlag
	outSize, inSize, rpi, dur int
	outData                   string
	tag, tags, typ, values    string
	count                     int
}

func parseClientFlags(op string, args []string) (*clientFlags, error) {
	c := &clientFlags{class: numFlag{max: 0xFFFF}, instance: numFlag{max: 0xFFFF}, attr: numFlag{max: 0xFFFF},
		cfgInst: numFlag{max: 0xFFFF}, outInst: numFlag{max: 0xFFFF}, inInst: numFlag{max: 0xFFFF}}
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {}
	fs.StringVar(&c.host, "host", "", "target address")
	fs.IntVar(&c.port, "port", 44818, "encapsulation port of the target")
	fs.IntVar(&c.timeout, "timeout-ms", 5000, "response timeout of each request")
	fs.StringVar(&c.transport, "transport", "tcp", "tcp or udp (list-identity)")
	fs.Var(&c.class, "class", "class")
	fs.Var(&c.instance, "instance", "instance")
	fs.Var(&c.attr, "attribute", "attribute")
	fs.StringVar(&c.data, "data", "", "hex data")
	fs.BoolVar(&c.connected, "connected", false, "over a Class 3 connection")
	fs.StringVar(&c.route, "route", "", "route path, for example 1/0")
	fs.Var(&c.paths, "path", "@CLASS/INSTANCE/ATTRIBUTE (repeatable)")
	fs.Var(&c.cfgInst, "config", "configuration assembly")
	fs.Var(&c.outInst, "output", "O->T connection point")
	fs.Var(&c.inInst, "input", "T->O connection point")
	fs.IntVar(&c.outSize, "output-size", 0, "O->T data size")
	fs.IntVar(&c.inSize, "input-size", 0, "T->O data size")
	fs.IntVar(&c.rpi, "rpi-ms", 10, "requested packet interval")
	fs.IntVar(&c.dur, "duration-ms", 1000, "exchange duration")
	fs.StringVar(&c.outData, "output-data", "", "hex O->T data")
	fs.StringVar(&c.tag, "tag", "", "tag name")
	fs.StringVar(&c.tags, "tags", "", "comma-separated tag names")
	fs.IntVar(&c.count, "count", 1, "elements")
	fs.StringVar(&c.typ, "type", "", "CIP elementary type")
	fs.StringVar(&c.values, "values", "", "comma-separated values")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	fs.Visit(func(f *flag.Flag) { c.routeSet = c.routeSet || f.Name == "route" })
	switch {
	case fs.NArg() > 0:
		return nil, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	case c.host == "":
		return nil, errors.New("--host is required")
	case c.port < 1 || c.port > math.MaxUint16, c.timeout < 1, c.rpi < 1, c.dur < 0, c.count < 1, c.count > math.MaxUint16,
		c.outSize < 0, c.inSize < 0, c.outSize > 505, c.inSize > 509:
		return nil, errors.New("a numeric flag is out of range")
	}
	switch c.transport {
	case "tcp":
	case "udp":
		if op != "list-identity" {
			return nil, errors.New("--transport udp: only for list-identity")
		}
	default:
		return nil, fmt.Errorf("--transport %q: tcp or udp", c.transport)
	}
	return c, nil
}

func isLogix(op string) bool {
	switch op {
	case "read-tag", "read-tags", "write-tag", "tag-list":
		return true
	}
	return false
}

// prepared holds what is checked before anything is sent.
type prepared struct {
	route   []byte
	data    []byte
	paths   [][3]uint16
	outData []byte
	typ     enip.Type
	write   []byte
	n       int
	tagList []string
}

func prepare(op string, c *clientFlags) (*prepared, error) {
	p := &prepared{}
	route := c.route
	if !c.routeSet && isLogix(op) {
		route = logixRoute
	}
	if route != "" {
		r, err := enip.Route(strings.ReplaceAll(route, "/", ","))
		if err != nil {
			return nil, fmt.Errorf("--route %q: %w", route, err)
		}
		p.route = r
	}
	switch op {
	case "set-attribute":
		b, err := hex.DecodeString(c.data)
		if err != nil {
			return nil, fmt.Errorf("--data: %w", err)
		}
		p.data = b
	case "multiple-get":
		if len(c.paths) == 0 {
			return nil, errors.New("--path is required")
		}
		for _, s := range c.paths {
			parts := strings.Split(strings.TrimPrefix(s, "@"), "/")
			if !strings.HasPrefix(s, "@") || len(parts) != 3 {
				return nil, fmt.Errorf("--path %q: want @CLASS/INSTANCE/ATTRIBUTE", s)
			}
			var v [3]uint16
			for i, x := range parts {
				n := numFlag{max: 0xFFFF}
				if err := n.Set(x); err != nil {
					return nil, fmt.Errorf("--path %q: %w", s, err)
				}
				v[i] = uint16(n.v)
			}
			p.paths = append(p.paths, v)
		}
	case "io":
		if c.outInst.v == 0 || c.inInst.v == 0 {
			return nil, errors.New("--output and --input are required")
		}
		b, err := hex.DecodeString(c.outData)
		if err != nil || (len(b) != 0 && len(b) != c.outSize) {
			return nil, errors.New("--output-data must be --output-size octets of hex")
		}
		p.outData = b
	case "read-tag":
		if c.tag == "" {
			return nil, errors.New("--tag is required")
		}
		if c.typ != "" {
			if _, ok := enip.AtomicTypeCode(c.typ); !ok {
				return nil, fmt.Errorf("--type %q", c.typ)
			}
		}
	case "read-tags":
		for _, t := range strings.Split(c.tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				p.tagList = append(p.tagList, t)
			}
		}
		if len(p.tagList) == 0 {
			return nil, errors.New("--tags is required")
		}
	case "write-tag":
		if c.tag == "" || c.values == "" {
			return nil, errors.New("--tag and --values are required")
		}
		code, ok := enip.AtomicTypeCode(c.typ)
		if !ok {
			return nil, fmt.Errorf("--type %q: a CIP elementary type (STRING and JSON are not supported)", c.typ)
		}
		p.typ = enip.Type{Code: code}
		for _, s := range strings.Split(c.values, ",") {
			b, err := parseValue(code, strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("--values: %w", err)
			}
			p.write = append(p.write, b...)
			p.n++
		}
	}
	return p, nil
}

// parseValue encodes one --values element of an elementary type, checking
// its range.
func parseValue(code uint16, s string) ([]byte, error) {
	le := binary.LittleEndian
	switch code {
	case enip.TypeREAL, enip.TypeLREAL:
		bits := 64
		if code == enip.TypeREAL {
			bits = 32
		}
		f, err := strconv.ParseFloat(s, bits)
		if err != nil {
			return nil, err
		}
		if code == enip.TypeREAL {
			return le.AppendUint32(nil, math.Float32bits(float32(f))), nil
		}
		return le.AppendUint64(nil, math.Float64bits(f)), nil
	case enip.TypeBOOL:
		switch strings.ToLower(s) {
		case "1", "true":
			return []byte{1}, nil
		case "0", "false":
			return []byte{0}, nil
		}
		return nil, fmt.Errorf("%q is not a BOOL", s)
	}
	size := map[uint16]int{enip.TypeSINT: 8, enip.TypeINT: 16, enip.TypeDINT: 32, enip.TypeLINT: 64,
		enip.TypeUSINT: 8, enip.TypeUINT: 16, enip.TypeUDINT: 32, enip.TypeULINT: 64}[code]
	if size == 0 {
		return nil, fmt.Errorf("type 0x%04X", code)
	}
	var u uint64
	if code >= enip.TypeUSINT && code <= enip.TypeULINT {
		v, err := strconv.ParseUint(s, 10, size)
		if err != nil {
			return nil, err
		}
		u = v
	} else {
		v, err := strconv.ParseInt(s, 10, size)
		if err != nil {
			return nil, err
		}
		u = uint64(v)
	}
	b := le.AppendUint64(nil, u)
	return b[:size/8], nil
}

func runClient(op string, args []string) int {
	c, err := parseClientFlags(op, args)
	if err != nil {
		return usage("%s: %v", op, err)
	}
	p, err := prepare(op, c)
	if err != nil {
		return usage("%s: %v", op, err)
	}
	res := result{"schemaVersion": "1.0", "adapter": adapterName, "operation": op, "ok": false, "error": nil}
	timeout := time.Duration(c.timeout) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 8*timeout+time.Duration(c.dur)*time.Millisecond+5*time.Second)
	defer cancel()
	code := execute(ctx, op, c, p, res, timeout)
	if code != 0 && res["error"] == nil {
		res["error"] = map[string]string{"code": "failed", "message": "the operation failed"}
	}
	emit(res)
	return code
}

// fail records err in res and returns exit 1.
func fail(res result, err error) int {
	code := "failed"
	var ne net.Error
	var oe *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		code = "timeout"
	case errors.As(err, &oe) && oe.Op == "dial":
		code = "connect-failed"
	}
	res["ok"], res["error"] = false, map[string]string{"code": code, "message": err.Error()}
	return 1
}

// status records a CIP reply's status (and its extended status).
func status(res result, r *enip.Response) {
	res["status"] = int(r.Status)
	if len(r.Ext) > 0 {
		ext := make([]int, len(r.Ext))
		for i, e := range r.Ext {
			ext[i] = int(e)
		}
		res["extendedStatus"] = ext
	}
}

func execute(ctx context.Context, op string, c *clientFlags, p *prepared, res result, timeout time.Duration) int {
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))
	if op == "list-identity" && c.transport == "udp" {
		ids, err := enip.Discover(ctx, addr, timeout)
		if err != nil {
			return fail(res, err)
		}
		if len(ids) == 0 {
			return fail(res, fmt.Errorf("no ListIdentity reply within %v: %w", timeout, context.DeadlineExceeded))
		}
		res["ok"], res["identity"] = true, identityDoc(ids[0])
		return 0
	}
	// The Logix operations go over a Class 3 connection, as enip's
	// Logix client is meant to, routed to slot 0 (logixRoute) unless
	// --route says otherwise; the others only with --connected.
	cl := &enip.Client{Addr: addr, Route: p.route, Timeout: timeout, Connected: c.connected || isLogix(op)}
	defer cl.Close()
	if cl.Connected {
		// Connect first: enip falls back to unconnected messaging when
		// the target refuses the Forward_Open, and says so in Mode.
		if _, err := cl.ListIdentity(ctx); err != nil {
			return fail(res, err)
		}
		mode, why := cl.Mode()
		res["connected"] = mode == "connected"
		if c.connected && mode != "connected" {
			return fail(res, fmt.Errorf("no Class 3 connection: %v", why))
		}
	}
	switch op {
	case "list-identity":
		id, err := cl.ListIdentity(ctx)
		if err != nil {
			return fail(res, err)
		}
		res["ok"], res["identity"] = true, identityDoc(*id)
	case "get-attribute", "set-attribute", "get-attributes-all":
		svc, attr := enip.SvcGetAttributeSingle, int(c.attr.v)
		switch op {
		case "set-attribute":
			svc = enip.SvcSetAttributeSingle
		case "get-attributes-all":
			svc, attr = enip.SvcGetAttributesAll, -1
		}
		r, err := cl.Do(ctx, svc, enip.Logical(uint16(c.class.v), uint16(c.instance.v), attr), p.data)
		if err != nil {
			return fail(res, err)
		}
		res["ok"] = true
		status(res, r)
		res["data"] = hex.EncodeToString(r.Data)
	case "multiple-get":
		var reqs [][]byte
		for _, x := range p.paths {
			reqs = append(reqs, enip.Request(enip.SvcGetAttributeSingle, enip.Logical(x[0], x[1], int(x[2])), nil))
		}
		r, err := cl.Do(ctx, enip.SvcMultipleService, enip.Logical(enip.ClassMessageRouter, 1, -1), enip.MultipleService(reqs))
		if err != nil {
			return fail(res, err)
		}
		res["ok"] = true
		status(res, r)
		if r.Status == enip.StatusSuccess || r.Status == enip.StatusEmbeddedError {
			parts, err := enip.SplitMultiple(r.Data)
			if err != nil {
				return fail(res, err)
			}
			replies := []map[string]any{}
			for _, b := range parts {
				er, err := enip.ParseResponse(b)
				if err != nil {
					return fail(res, err)
				}
				replies = append(replies, map[string]any{"status": int(er.Status), "data": hex.EncodeToString(er.Data)})
			}
			res["replies"] = replies
		}
	case "io":
		return runIO(ctx, cl, c, p, res)
	case "read-tag":
		t, data, err := cl.ReadTag(ctx, c.tag, c.count)
		if done, code := tagErr(res, err); done {
			return code
		}
		if c.typ != "" && t.Name() != c.typ {
			return fail(res, fmt.Errorf("%s is %s, not %s", c.tag, t.Name(), c.typ))
		}
		vals, err := decode(t, data, c.count)
		if err != nil {
			return fail(res, err)
		}
		res["ok"], res["status"], res["values"] = true, 0, vals
	case "read-tags":
		reads := make([]*enip.TagRead, len(p.tagList))
		for i, t := range p.tagList {
			reads[i] = &enip.TagRead{Tag: t, Elements: 1}
		}
		if err := cl.ReadTags(ctx, reads); err != nil {
			return fail(res, err)
		}
		results := []map[string]any{}
		for _, r := range reads {
			one := map[string]any{"tag": r.Tag}
			var ce *enip.Error
			switch {
			case errors.As(r.Err, &ce):
				one["status"] = int(ce.Status)
			case r.Err != nil:
				return fail(res, r.Err)
			default:
				vals, err := decode(r.Type, r.Data, 1)
				if err != nil {
					return fail(res, err)
				}
				one["status"], one["values"] = 0, vals
			}
			results = append(results, one)
		}
		res["ok"], res["results"] = true, results
	case "write-tag":
		err := cl.WriteTag(ctx, c.tag, p.typ, p.n, p.write)
		if done, code := tagErr(res, err); done {
			return code
		}
		res["ok"], res["status"] = true, 0
	case "tag-list":
		return tagList(ctx, cl, res)
	}
	return 0
}

// tagErr records a CIP status (ok, exit 0) or a failure; done is false when
// err is nil.
func tagErr(res result, err error) (done bool, code int) {
	if err == nil {
		return false, 0
	}
	var ce *enip.Error
	if errors.As(err, &ce) {
		res["ok"], res["status"] = true, int(ce.Status)
		if len(ce.Ext) > 0 {
			res["extendedStatus"] = []int{int(ce.Ext[0])}
		}
		return true, 0
	}
	return true, fail(res, err)
}

// decode turns elements of an elementary or STRING type into JSON values;
// integers exactly, BOOL as 0 or 1.
func decode(t enip.Type, data []byte, n int) ([]any, error) {
	if t.IsStruct() && t.Handle == enip.HandleSTRING {
		out := []any{}
		for i := 0; i < n; i++ {
			if len(data) < (i+1)*enip.StringSize {
				return nil, fmt.Errorf("STRING[%d]: %d bytes", n, len(data))
			}
			s, err := enip.DecodeString(data[i*enip.StringSize:])
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	}
	size := t.Size()
	if size == 0 || len(data) < n*size {
		return nil, fmt.Errorf("%s[%d] in %d bytes: not an elementary value", t.Name(), n, len(data))
	}
	le := binary.LittleEndian
	out := make([]any, n)
	for i := range out {
		b := data[i*size:]
		switch t.Code {
		case enip.TypeBOOL, enip.TypeUSINT, enip.TypeBYTE:
			out[i] = b[0]
		case enip.TypeSINT:
			out[i] = int8(b[0])
		case enip.TypeINT:
			out[i] = int16(le.Uint16(b))
		case enip.TypeUINT, enip.TypeWORD:
			out[i] = le.Uint16(b)
		case enip.TypeDINT:
			out[i] = int32(le.Uint32(b))
		case enip.TypeUDINT, enip.TypeDWORD:
			out[i] = le.Uint32(b)
		case enip.TypeLINT:
			out[i] = int64(le.Uint64(b))
		case enip.TypeULINT, enip.TypeLWORD:
			out[i] = le.Uint64(b)
		case enip.TypeREAL:
			out[i] = float64(math.Float32frombits(le.Uint32(b)))
		case enip.TypeLREAL:
			out[i] = math.Float64frombits(le.Uint64(b))
		}
	}
	return out, nil
}

// identityDoc is a ListIdentity reply as the contract reports it.
func identityDoc(id enip.Identity) map[string]any {
	// enip formats the revision "major.%03d" and the serial number as
	// hexadecimal text.
	rev := id.Revision
	if major, minor, ok := strings.Cut(rev, "."); ok {
		mi, _ := strconv.Atoi(minor)
		rev = major + "." + strconv.Itoa(mi)
	}
	serial, _ := strconv.ParseUint(id.Serial, 16, 32)
	return map[string]any{
		"vendorId": id.Vendor, "deviceType": id.DeviceType, "productCode": id.ProductCode, "revision": rev,
		"status": id.Status, "serialNumber": serial, "productName": id.ProductName,
		"socketAddress": net.JoinHostPort(id.IP, strconv.Itoa(int(id.Port))), "state": id.State,
	}
}

// tagList is the controller-scoped tag list (Symbol object), each
// structure's name read from its template (Template object), and the
// controller name.
func tagList(ctx context.Context, cl *enip.Client, res result) int {
	infos, err := cl.ListTags(ctx, "")
	if done, code := tagErr(res, err); done {
		return code
	}
	names := map[uint16]string{}
	tags := []map[string]any{}
	for _, ti := range infos {
		if ti.System {
			continue
		}
		typ := ti.Type.Name()
		if ti.Type.IsStruct() {
			n, ok := names[ti.Template]
			if !ok {
				tm, err := cl.ReadTemplate(ctx, ti.Template)
				if err != nil {
					return fail(res, fmt.Errorf("template %d of %s: %w", ti.Template, ti.Name, err))
				}
				n = tm.Name
				if tm.Handle == enip.HandleSTRING {
					n = "STRING"
				}
				names[ti.Template] = n
			}
			typ = n
		}
		dims := []int{}
		for _, d := range ti.Dims {
			if d > 0 {
				dims = append(dims, d)
			}
		}
		tags = append(tags, map[string]any{"name": ti.Name, "type": typ, "dims": dims})
	}
	res["tags"] = tags
	// The controller name: Get_Attributes_All of class 0x64 instance 1,
	// as pycomm3 reads it.
	if r, err := cl.Do(ctx, enip.SvcGetAttributesAll, enip.Logical(0x64, 1, -1), nil); err == nil && r.Status == 0 && len(r.Data) >= 2 {
		n := int(binary.LittleEndian.Uint16(r.Data))
		if len(r.Data) >= 2+n {
			res["programName"] = string(r.Data[2 : 2+n])
		}
	}
	res["ok"] = true
	return 0
}

// runIO opens a Class 1 connection with enip's scanner (OpenIO), runs it
// for --duration-ms in run mode and closes it.
func runIO(ctx context.Context, cl *enip.Client, c *clientFlags, p *prepared, res result) int {
	cfg := enip.IOConfig{ConfigInstance: uint16(c.cfgInst.v), OutputInstance: uint16(c.outInst.v),
		InputInstance: uint16(c.inInst.v), OutputSize: c.outSize, InputSize: c.inSize,
		RPI: time.Duration(c.rpi) * time.Millisecond}
	before := udpCounters()
	conn, err := cl.OpenIO(ctx, cfg)
	if err != nil {
		var ce *enip.Error
		if !errors.As(err, &ce) {
			return fail(res, err)
		}
		fo := map[string]any{"status": int(ce.Status)}
		ext := []int{}
		for _, e := range ce.Ext {
			ext = append(ext, int(e))
		}
		fo["extendedStatus"] = ext
		res["ok"], res["forwardOpen"] = true, fo
		return 0
	}
	if len(p.outData) > 0 {
		conn.SetOutput(p.outData)
	}
	conn.SetRun(true)
	var first []byte
	got := false
	end := time.After(time.Duration(c.dur) * time.Millisecond)
loop:
	for {
		wctx, cancel := context.WithTimeout(ctx, time.Duration(c.dur)*time.Millisecond)
		in, err := conn.WaitInput(wctx)
		cancel()
		if err == nil && !got {
			first, got = in, true
		}
		select {
		case <-end:
			break loop
		default:
		}
		if err != nil && conn.Err() != nil {
			<-end
			break loop
		}
	}
	last, _, _ := conn.Input()
	timedOut := conn.Err() != nil
	conn.Close()
	after := udpCounters()
	res["ok"] = true
	res["forwardOpen"] = map[string]any{"status": 0}
	res["sent"] = after.out - before.out
	res["received"] = after.in - before.in
	res["firstInput"] = hex.EncodeToString(first)
	res["lastInput"] = hex.EncodeToString(last)
	res["timedOut"] = timedOut
	// enip v0.2.0 sends no Forward_Close for I/O connections: IOConn.Close
	// stops sending and the target drops the connection on its timeout.
	res["forwardClose"] = map[string]any{"status": -1, "error": "enip's IOConn sends no Forward_Close"}
	return 0
}
