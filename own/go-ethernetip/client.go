// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	ethernetip "github.com/otfabric/go-ethernetip"
	"github.com/otfabric/go-ethernetip/cip"
	"github.com/otfabric/go-ethernetip/client"
	"github.com/otfabric/go-ethernetip/logix"
	"github.com/otfabric/go-ethernetip/scanner"
)

// logixRoute is the route of the Logix operations when --route is not
// given: a Logix controller sits behind the backplane, in slot 0.
const logixRoute = "1/0"

type result map[string]any

type opError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func classify(err error) opError {
	switch {
	case errors.Is(err, ethernetip.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return opError{"timeout", err.Error()}
	case errors.Is(err, ethernetip.ErrConnectFailed):
		return opError{"connect-failed", err.Error()}
	case errors.Is(err, ethernetip.ErrConnectionLost):
		return opError{"connection-lost", err.Error()}
	case errors.Is(err, ethernetip.ErrProtocol):
		return opError{"protocol", err.Error()}
	default:
		return opError{"failed", err.Error()}
	}
}

// clientFlags are the flags of every operation (docs/CONTAINER_CONTRACT.md).
type clientFlags struct {
	host                      string
	port, timeout             int
	transport                 string
	class, attribute          u16Flag
	instance                  u32Flag
	data                      string
	connected                 bool
	route                     string
	routeSet                  bool
	paths                     multiFlag
	cfgInst, outInst, inInst  u32Flag
	outSize, inSize, rpi, dur int
	outData, cfgData          string
	large, multicast          bool
	tag, tags, typ, values    string
	count                     int
}

func parseClientFlags(op string, args []string) (*clientFlags, bool) {
	c := &clientFlags{}
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.host, "host", "", "target address")
	fs.IntVar(&c.port, "port", 44818, "encapsulation port of the target")
	fs.IntVar(&c.timeout, "timeout-ms", 5000, "response timeout of each request")
	fs.StringVar(&c.transport, "transport", "tcp", "tcp or udp")
	fs.Var(&c.class, "class", "class")
	fs.Var(&c.instance, "instance", "instance")
	fs.Var(&c.attribute, "attribute", "attribute")
	fs.StringVar(&c.data, "data", "", "hex data")
	fs.BoolVar(&c.connected, "connected", false, "over a Class 3 connection")
	fs.StringVar(&c.route, "route", "", "route path for Unconnected_Send, for example 1/0")
	fs.Var(&c.paths, "path", "@class/instance/attribute (repeatable)")
	fs.Var(&c.cfgInst, "config", "configuration assembly")
	fs.Var(&c.outInst, "output", "O->T connection point")
	fs.Var(&c.inInst, "input", "T->O connection point")
	fs.IntVar(&c.outSize, "output-size", 0, "O->T data size")
	fs.IntVar(&c.inSize, "input-size", 0, "T->O data size")
	fs.IntVar(&c.rpi, "rpi-ms", 10, "requested packet interval")
	fs.IntVar(&c.dur, "duration-ms", 1000, "exchange duration")
	fs.StringVar(&c.outData, "output-data", "", "hex O->T data")
	fs.StringVar(&c.cfgData, "config-data", "", "hex configuration data")
	fs.BoolVar(&c.large, "large", false, "Large_Forward_Open")
	fs.BoolVar(&c.multicast, "multicast", false, "T->O multicast")
	fs.StringVar(&c.tag, "tag", "", "tag name")
	fs.StringVar(&c.tags, "tags", "", "comma-separated tag names")
	fs.IntVar(&c.count, "count", 1, "elements")
	fs.StringVar(&c.typ, "type", "", "CIP elementary type")
	fs.StringVar(&c.values, "values", "", "comma-separated values")
	if err := fs.Parse(args); err != nil {
		return nil, false
	}
	fs.Visit(func(f *flag.Flag) { c.routeSet = c.routeSet || f.Name == "route" })
	switch {
	case fs.NArg() > 0:
		logf("unexpected argument %q", fs.Arg(0))
	case c.host == "":
		logf("--host is required")
	case c.transport != "tcp" && c.transport != "udp":
		logf("--transport %q: tcp or udp", c.transport)
	case c.port < 1 || c.port > math.MaxUint16, c.timeout < 1, c.rpi < 1, c.dur < 0, c.count < 1, c.outSize < 0, c.inSize < 0:
		logf("a numeric flag is out of range")
	default:
		return c, true
	}
	return nil, false
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
	route   cip.Path
	data    []byte
	paths   []cip.Path
	io      scanner.IOConfig
	typ     cip.DataType
	value   logix.Value
	tagList []string
}

// prepare checks the flags an operation needs: a usage error sends nothing.
func prepare(op string, c *clientFlags) (*prepared, error) {
	p := &prepared{}
	route := c.route
	if !c.routeSet && isLogix(op) {
		route = logixRoute
	}
	if route != "" {
		r, err := cip.ParseRoute(route)
		if err != nil {
			return nil, fmt.Errorf("--route: %w", err)
		}
		p.route = r
	}
	if c.transport == "udp" && op != "list-identity" {
		return nil, fmt.Errorf("--transport udp: only for list-identity")
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
			return nil, fmt.Errorf("--path is required")
		}
		for _, s := range c.paths {
			path, err := cip.ParsePath(s)
			if err != nil {
				return nil, fmt.Errorf("--path %s: %w", s, err)
			}
			p.paths = append(p.paths, path)
		}
	case "io":
		if c.outInst == 0 || c.inInst == 0 {
			return nil, fmt.Errorf("--output and --input are required")
		}
		out, err := hex.DecodeString(c.outData)
		if err != nil || (len(out) != 0 && len(out) != c.outSize) {
			return nil, fmt.Errorf("--output-data must be --output-size octets of hex")
		}
		if len(out) == 0 {
			out = nil // zeros until set: the scanner wants nil or --output-size octets
		}
		cd, err := hex.DecodeString(c.cfgData)
		if err != nil || len(cd)%2 != 0 {
			return nil, fmt.Errorf("--config-data must be an even number of octets of hex")
		}
		p.io = scanner.IOConfig{
			ConfigInstance: uint32(c.cfgInst), OutputInstance: uint32(c.outInst), InputInstance: uint32(c.inInst),
			OutputSize: c.outSize, InputSize: c.inSize, RPI: time.Duration(c.rpi) * time.Millisecond,
			Large: c.large, Multicast: c.multicast, ConfigData: cd, Output: out,
		}
	case "read-tag":
		if c.tag == "" {
			return nil, fmt.Errorf("--tag is required")
		}
		if c.typ != "" {
			if _, ok := cip.ParseDataType(c.typ); !ok {
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
			return nil, fmt.Errorf("--tags is required")
		}
	case "write-tag":
		if c.tag == "" || c.values == "" {
			return nil, fmt.Errorf("--tag and --values are required")
		}
		t, ok := cip.ParseDataType(c.typ)
		if !ok || strings.EqualFold(c.typ, "STRING") {
			return nil, fmt.Errorf("--type %q: a CIP elementary type (STRING and JSON are not supported)", c.typ)
		}
		v, err := parseValues(t, c.values)
		if err != nil {
			return nil, fmt.Errorf("--values: %w", err)
		}
		p.typ, p.value = t, v
	}
	return p, nil
}

func clientOp(op string, args []string) int {
	c, ok := parseClientFlags(op, args)
	if !ok {
		return exitUsage
	}
	p, err := prepare(op, c)
	if err != nil {
		return usage("%s: %v", op, err)
	}
	res := result{"schemaVersion": schemaVersion, "adapter": adapterName, "operation": op, "ok": false, "error": nil}
	timeout := time.Duration(c.timeout) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 4*timeout+time.Duration(c.dur)*time.Millisecond+5*time.Second)
	defer cancel()
	code := exec(ctx, op, c, p, res, timeout)
	if code == exitFailed && res["error"] == nil {
		res["error"] = opError{"failed", "the operation failed"}
	}
	emit(res)
	return code
}

// fail records err in res and returns exit 1.
func fail(res result, err error) int {
	res["ok"], res["error"] = false, classify(err)
	return exitFailed
}

func cipError(err error) (*ethernetip.CIPError, bool) {
	var ce *ethernetip.CIPError
	return ce, errors.As(err, &ce)
}

func extended(ext []uint16) []int {
	out := []int{}
	for _, x := range ext {
		out = append(out, int(x))
	}
	return out
}

func exec(ctx context.Context, op string, c *clientFlags, p *prepared, res result, timeout time.Duration) int {
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))
	if op == "list-identity" && c.transport == "udp" {
		uctx, ucancel := context.WithTimeout(ctx, timeout)
		defer ucancel()
		dev, err := client.ListIdentity(uctx, addr)
		if err != nil {
			return fail(res, err)
		}
		res["identity"] = identityDoc(dev.Identity.Identity, dev.Identity.Socket.AddrPort().String())
		res["ok"] = true
		return exitOK
	}
	opts := []client.Option{client.WithTimeout(timeout)}
	if p.route != nil {
		opts = append(opts, client.WithRoute(p.route))
	}
	cl, err := client.Dial(ctx, addr, opts...)
	if err != nil {
		return fail(res, err)
	}
	defer func() { _ = cl.Close() }()
	var d client.Doer = cl
	if c.connected {
		cn, err := cl.OpenConnection(ctx, client.ConnectionOptions{Large: c.large})
		if err != nil {
			return fail(res, err)
		}
		defer func() { _ = cn.Close(ctx) }()
		d = cn
	}
	switch op {
	case "list-identity":
		it, err := cl.ListIdentity(ctx)
		if err != nil {
			return fail(res, err)
		}
		res["identity"] = identityDoc(it.Identity, it.Socket.AddrPort().String())
	case "list-services":
		svcs, err := cl.ListServices(ctx)
		if err != nil {
			return fail(res, err)
		}
		out := []map[string]any{}
		for _, s := range svcs {
			out = append(out, map[string]any{"name": s.Name, "version": s.Version, "capabilities": s.Capabilities})
		}
		res["services"] = out
	case "get-attribute", "get-attributes-all", "set-attribute":
		req := cip.Request{Service: cip.ServiceGetAttributeSingle, Path: cip.AttributePath(cip.Class(c.class), uint32(c.instance), uint16(c.attribute))}
		switch op {
		case "get-attributes-all":
			req = cip.Request{Service: cip.ServiceGetAttributesAll, Path: cip.ObjectPath(cip.Class(c.class), uint32(c.instance))}
		case "set-attribute":
			req.Service, req.Data = cip.ServiceSetAttributeSingle, p.data
		}
		resp, err := d.Do(ctx, req)
		if _, isCIP := cipError(err); err != nil && !isCIP {
			return fail(res, err)
		}
		res["status"], res["extendedStatus"], res["data"] = int(resp.Status), extended(resp.Extended), hex.EncodeToString(resp.Data)
	case "multiple-get":
		reqs := make([]cip.Request, len(p.paths))
		for i, path := range p.paths {
			reqs[i] = cip.Request{Service: cip.ServiceGetAttributeSingle, Path: path}
		}
		type multi interface {
			MultipleService(context.Context, []cip.Request) ([]cip.Response, error)
		}
		resps, err := d.(multi).MultipleService(ctx, reqs)
		status := 0
		if err != nil {
			ce, isCIP := cipError(err)
			if !isCIP {
				return fail(res, err)
			}
			status = int(ce.Status)
		}
		out := []map[string]any{}
		for _, r := range resps {
			out = append(out, map[string]any{"status": int(r.Status), "data": hex.EncodeToString(r.Data)})
		}
		res["status"], res["replies"] = status, out
	case "identity":
		id, err := cl.Identity(ctx)
		if err != nil {
			if ce, isCIP := cipError(err); isCIP {
				res["status"], res["ok"] = int(ce.Status), true
				return exitOK
			}
			return fail(res, err)
		}
		// The address asked, as EIPScanner's and EEIP.NET's adapters report it:
		// Get_Attributes_All has no socket address.
		res["identity"] = identityDoc(id, c.host)
	case "io":
		return ioOp(ctx, cl, res, p.io, time.Duration(c.dur)*time.Millisecond)
	case "read-tag":
		lc := logix.NewClient(d, logix.WithMaxData(maxData(c.connected, c.large)))
		v, err := lc.Read(ctx, c.tag, c.count)
		if err != nil {
			ce, isCIP := cipError(err)
			if !isCIP {
				return fail(res, err)
			}
			res["status"], res["values"] = int(ce.Status), []any{}
			break
		}
		res["status"], res["type"], res["values"] = 0, v.Type.String(), valuesOf(v)
	case "read-tags":
		// In sequence, one Read Tag each, as cpppo's adapter does: a target
		// that serves Multiple Service Packets only over Class 3 still
		// answers.
		lc := logix.NewClient(d, logix.WithMaxData(maxData(c.connected, c.large)))
		out := []map[string]any{}
		for _, t := range p.tagList {
			r := map[string]any{"tag": t, "status": 0, "values": []any{}}
			v, err := lc.Read(ctx, t, 1)
			if err != nil {
				ce, isCIP := cipError(err)
				if !isCIP {
					return fail(res, err)
				}
				r["status"] = int(ce.Status)
			} else {
				r["values"] = valuesOf(v)
			}
			out = append(out, r)
		}
		res["results"] = out
	case "write-tag":
		lc := logix.NewClient(d, logix.WithMaxData(maxData(c.connected, c.large)))
		res["status"] = 0
		if err := lc.Write(ctx, c.tag, p.value); err != nil {
			ce, isCIP := cipError(err)
			if !isCIP {
				return fail(res, err)
			}
			res["status"] = int(ce.Status)
		}
	case "tag-list":
		lc := logix.NewClient(d)
		tags, err := lc.ListTags(ctx, "")
		if err != nil {
			return fail(res, err)
		}
		// The type is the element type: an elementary type's name, or for a
		// structure the name of its template, read from the Template object
		// (1756-PM020: the name before ';'); the dimensions are in dims.
		names := map[uint16]string{}
		out := []map[string]any{}
		for _, t := range tags {
			typ := t.Type.Atomic().String()
			if t.Type.IsStruct() {
				id := t.Type.Template()
				name, ok := names[id]
				if !ok {
					tm, err := lc.Template(ctx, id)
					if err != nil {
						return fail(res, fmt.Errorf("template %d of %s: %w", id, t.Name, err))
					}
					name, names[id] = tm.Name, tm.Name
				}
				typ = name
			}
			out = append(out, map[string]any{"name": t.Name, "type": typ, "dims": t.Dims[:t.Type.Dims()]})
		}
		res["tags"] = out
		name, err := lc.ProgramName(ctx)
		if err != nil {
			res["programName"] = nil
		} else {
			res["programName"] = name
		}
	}
	res["ok"] = true
	return exitOK
}

func maxData(connected, large bool) int {
	switch {
	case connected && large:
		return 3900
	case connected:
		return 450
	}
	return 480
}

// ioOp opens an I/O connection, exchanges data for dur and closes it.
func ioOp(ctx context.Context, cl *client.Client, res result, cfg scanner.IOConfig, dur time.Duration) int {
	s, err := scanner.New(cl, scanner.WithLocalAddr(":2222"))
	if err != nil {
		return fail(res, err)
	}
	defer func() { _ = s.Close(ctx) }()
	// OnInput sees each new input (a new sequence count); the packets,
	// repeats of unchanged data included, are in Stats.
	var (
		firstMu sync.Mutex
		first   []byte
	)
	cfg.OnInput = func(data []byte, _ bool) {
		firstMu.Lock()
		defer firstMu.Unlock()
		if first == nil {
			first = append([]byte{}, data...)
		}
	}
	conn, err := s.Open(ctx, cfg)
	fo := map[string]any{"status": 0}
	if err != nil {
		ce, isCIP := cipError(err)
		if !isCIP {
			return fail(res, err)
		}
		fo["status"], fo["extendedStatus"] = int(ce.Status), extended(ce.Extended)
		res["forwardOpen"], res["ok"] = fo, true
		return exitOK
	}
	res["forwardOpen"] = fo
	select {
	case <-time.After(dur):
	case <-conn.Done():
	case <-ctx.Done():
	}
	st := conn.Stats()
	last, _, _ := conn.Input()
	res["sent"], res["received"] = st.Sent, st.Received
	firstMu.Lock()
	res["firstInput"], res["lastInput"] = hex.EncodeToString(first), hex.EncodeToString(last)
	firstMu.Unlock()
	res["timedOut"] = conn.State() == ethernetip.StateTimedOut
	// The exchange is over: a Forward_Close that is not answered is
	// recorded with status -1, not as a failed operation.
	fc := map[string]any{"status": 0}
	if err := conn.Close(ctx); err != nil {
		if ce, isCIP := cipError(err); isCIP {
			fc["status"], fc["extendedStatus"] = int(ce.Status), extended(ce.Extended)
		} else {
			fc["status"], fc["error"] = -1, err.Error()
		}
	}
	res["forwardClose"], res["ok"] = fc, true
	return exitOK
}

func identityDoc(id cip.Identity, socket string) map[string]any {
	return map[string]any{
		"vendorId": id.VendorID, "deviceType": uint16(id.DeviceType), "productCode": id.ProductCode,
		"revision": id.Revision.String(), "status": uint16(id.Status), "serialNumber": id.SerialNumber,
		"productName": id.ProductName, "state": id.State, "socketAddress": socket,
	}
}

func valuesOf(v logix.Value) []any {
	out := []any{}
	for i := 0; i < v.Count; i++ {
		switch {
		case v.Type.IsFloat():
			f, _ := v.Float(i)
			out = append(out, f)
		case v.Type == cip.TypeBOOL:
			b, _ := v.Bool(i)
			out = append(out, b)
		default:
			n, err := v.Int(i)
			if err != nil {
				return out
			}
			out = append(out, n)
		}
	}
	return out
}

func parseValues(t cip.DataType, s string) (logix.Value, error) {
	parts := strings.Split(s, ",")
	if t.IsFloat() {
		fs := make([]float64, len(parts))
		for i, p := range parts {
			f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil {
				return logix.Value{}, err
			}
			fs[i] = f
		}
		return logix.Floats(t, fs...)
	}
	is := make([]int64, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case t == cip.TypeBOOL && strings.EqualFold(p, "true"):
			is[i] = 1
		case t == cip.TypeBOOL && strings.EqualFold(p, "false"):
			is[i] = 0
		default:
			n, err := strconv.ParseInt(p, 0, 64)
			if err != nil {
				return logix.Value{}, err
			}
			is[i] = n
		}
	}
	return logix.Ints(t, is...)
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(s string) error { *m = append(*m, s); return nil }

// u16Flag and u32Flag are unsigned flags of a CIP field's width: a value
// that does not fit is a usage error, not truncated.
type u16Flag uint16

func (f *u16Flag) String() string { return strconv.FormatUint(uint64(*f), 10) }

func (f *u16Flag) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, 16)
	if err != nil {
		return err
	}
	*f = u16Flag(v)
	return nil
}

type u32Flag uint32

func (f *u32Flag) String() string { return strconv.FormatUint(uint64(*f), 10) }

func (f *u32Flag) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, 32)
	if err != nil {
		return err
	}
	*f = u32Flag(v)
	return nil
}
