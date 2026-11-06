// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/cB-Sigma/enip"
	"github.com/cB-Sigma/enip/server"
)

// fixture is the part of a fixture document (docs/FIXTURES.md) the server
// serves.
type fixture struct {
	Name     string `json:"name"`
	Identity struct {
		VendorID     uint16 `json:"vendorId"`
		DeviceType   uint16 `json:"deviceType"`
		ProductCode  uint16 `json:"productCode"`
		Revision     string `json:"revision"`
		SerialNumber uint32 `json:"serialNumber"`
		ProductName  string `json:"productName"`
	} `json:"identity"`
	Assemblies []struct {
		Instance uint16 `json:"instance"`
		Kind     string `json:"kind"`
		Size     int    `json:"size"`
	} `json:"assemblies"`
	EchoOutputToInput bool       `json:"echoOutputToInput"`
	Slot              int        `json:"slot"`
	Templates         []template `json:"templates"`
	Tags              []fxTag    `json:"tags"`
}

type template struct {
	Name    string `json:"name"`
	ID      int    `json:"id"`
	Handle  uint16 `json:"handle"`
	Size    int    `json:"size"`
	Members []struct {
		Name   string `json:"name"`
		Type   string `json:"type"`
		Offset int    `json:"offset"`
		Count  int    `json:"count"`
		Bit    *int   `json:"bit"`
	} `json:"members"`
}

type fxTag struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Count   int       `json:"count"`
	Values  []float64 `json:"values"`
	Pattern *struct {
		Multiplier float64 `json:"multiplier"`
		Offset     float64 `json:"offset"`
	} `json:"pattern"`
	Strings []string         `json:"strings"`
	Fields  []map[string]any `json:"fields"`
}

func (t fxTag) value(i int) float64 {
	if t.Pattern != nil {
		return float64(i)*t.Pattern.Multiplier + t.Pattern.Offset
	}
	if i < len(t.Values) {
		return t.Values[i]
	}
	return 0
}

// controller is the fixture turned into enip's server.
type controller struct {
	s       *server.Server
	structs map[string]server.StructDef // by template name
}

// newController builds the target of a fixture, not yet listening. An error
// is a fixture the adapter cannot serve (exit 4).
func newController(f *fixture) (*controller, error) {
	if !slices.Contains(serverFixtures, f.Name) {
		return nil, fmt.Errorf("fixture %q: this adapter serves %s", f.Name, strings.Join(serverFixtures, ", "))
	}
	if f.Slot < 0 || f.Slot > 255 {
		return nil, fmt.Errorf("slot %d", f.Slot)
	}
	s := server.New()
	// The Logix controller name (class 0x64), which pycomm3 reads on open
	// and the tag list reports as the program name.
	s.ControllerName = f.Name
	s.Slot = byte(f.Slot)
	// enip's identity object has no revision: it serves 33.11 whatever
	// the fixture says (docs/CAPABILITIES.md, cbsigma).
	s.SetIdentity(enip.Identity{Protocol: 1, Vendor: f.Identity.VendorID, DeviceType: f.Identity.DeviceType,
		ProductCode: f.Identity.ProductCode, ProductName: f.Identity.ProductName, State: 3}, f.Identity.SerialNumber)
	for _, a := range f.Assemblies {
		switch a.Kind {
		case "input", "output":
		default:
			return nil, fmt.Errorf("assembly %d: kind %q (enip's adapter has input and output assemblies only)", a.Instance, a.Kind)
		}
		// Class 1 data, and the Assembly object's attribute 3 for explicit
		// messaging: two separate stores in enip (docs/CAPABILITIES.md).
		s.AddAssembly(a.Instance, a.Size)
		s.SetAttr(server.AttrKey{Class: 0x04, Instance: a.Instance, Attribute: 3}, server.Attr{Data: make([]byte, a.Size), Settable: true})
	}
	if f.EchoOutputToInput {
		return nil, errors.New("echoOutputToInput: enip's adapter has no hook on consumed data")
	}
	c := &controller{s: s, structs: map[string]server.StructDef{}}
	for _, t := range f.Templates {
		d := server.StructDef{Name: t.Name, Handle: t.Handle, Size: t.Size}
		for _, m := range t.Members {
			typ, ok := c.typeOf(m.Type)
			if !ok {
				return nil, fmt.Errorf("template %s: member %s: type %q", t.Name, m.Name, m.Type)
			}
			fl := server.Field{Name: m.Name, Type: typ, Offset: m.Offset, Array: m.Count,
				Hidden: strings.HasPrefix(m.Name, "ZZZZZZZZZZ") || strings.HasPrefix(m.Name, "__")}
			if m.Bit != nil {
				fl.Bit = *m.Bit
			}
			d.Fields = append(d.Fields, fl)
		}
		if err := s.CheckStruct(d); err != nil {
			return nil, err
		}
		s.DefineStruct(d)
		c.structs[t.Name] = d
	}
	for _, t := range f.Tags {
		if err := c.addTag(t); err != nil {
			return nil, fmt.Errorf("tag %s: %w", t.Name, err)
		}
	}
	return c, nil
}

// typeOf is the enip type of an elementary type name, STRING or a template
// defined earlier.
func (c *controller) typeOf(name string) (enip.Type, bool) {
	if name == "STRING" {
		return enip.Type{Code: enip.TypeStruct, Handle: enip.HandleSTRING}, true
	}
	if code, ok := enip.AtomicTypeCode(name); ok {
		return enip.Type{Code: code}, true
	}
	if d, ok := c.structs[name]; ok {
		return enip.Type{Code: enip.TypeStruct, Handle: d.Handle}, true
	}
	return enip.Type{}, false
}

func (c *controller) addTag(t fxTag) error {
	n := max(t.Count, 1)
	typ, ok := c.typeOf(t.Type)
	if !ok {
		return fmt.Errorf("type %q", t.Type)
	}
	switch {
	case t.Type == "BOOL" && t.Count > 0:
		// Logix holds a BOOL array as DWORDs, bit i in bit i%32 of DWORD
		// i/32.
		words := make([]byte, 4*((n+31)/32))
		for i := 0; i < n; i++ {
			if t.value(i) != 0 {
				words[i/32*4+i%32/8] |= 1 << (i % 8)
			}
		}
		c.s.SetTag(t.Name, enip.Type{Code: enip.TypeDWORD}, len(words)/4, words)
	case t.Type == "STRING":
		var data []byte
		for i := 0; i < n; i++ {
			s := ""
			if i < len(t.Strings) {
				s = t.Strings[i]
			}
			b, err := enip.EncodeString(s)
			if err != nil {
				return err
			}
			data = append(data, b...)
		}
		c.s.SetTag(t.Name, typ, n, data)
	case typ.IsStruct():
		d := c.structs[t.Type]
		data := make([]byte, n*d.Size)
		for i := 0; i < n && i < len(t.Fields); i++ {
			if err := c.encodeStruct(d, t.Fields[i], data[i*d.Size:(i+1)*d.Size]); err != nil {
				return err
			}
		}
		c.s.SetTag(t.Name, typ, n, data)
	default:
		var data []byte
		for i := 0; i < n; i++ {
			b, err := encodeAtomic(typ.Code, t.value(i))
			if err != nil {
				return err
			}
			data = append(data, b...)
		}
		c.s.SetTag(t.Name, typ, n, data)
	}
	return nil
}

// encodeStruct writes the members named in v into b, one element of d.
func (c *controller) encodeStruct(d server.StructDef, v map[string]any, b []byte) error {
	for _, f := range d.Fields {
		x, ok := v[f.Name]
		if !ok {
			continue
		}
		if f.Type.Code == enip.TypeBOOL && f.Array == 0 {
			n, ok := x.(float64)
			if !ok {
				return fmt.Errorf("%s.%s: %v", d.Name, f.Name, x)
			}
			if n != 0 {
				b[f.Offset] |= 1 << f.Bit
			}
			continue
		}
		elems := []any{x}
		if f.Array > 0 {
			l, ok := x.([]any)
			if !ok {
				return fmt.Errorf("%s.%s: want a list", d.Name, f.Name)
			}
			elems = l
		}
		size := f.Type.Size()
		if f.Type.IsStruct() && f.Type.Handle != enip.HandleSTRING {
			for _, sd := range c.structs {
				if sd.Handle == f.Type.Handle {
					size = sd.Size
				}
			}
		}
		for i, e := range elems {
			off := f.Offset + i*size
			var out []byte
			var err error
			switch {
			case f.Type.Handle == enip.HandleSTRING && f.Type.IsStruct():
				s, _ := e.(string)
				out, err = enip.EncodeString(s)
			case f.Type.IsStruct():
				m, _ := e.(map[string]any)
				for _, sd := range c.structs {
					if sd.Handle == f.Type.Handle {
						err = c.encodeStruct(sd, m, b[off:off+size])
					}
				}
			default:
				n, _ := e.(float64)
				out, err = encodeAtomic(f.Type.Code, n)
			}
			if err != nil {
				return fmt.Errorf("%s.%s: %w", d.Name, f.Name, err)
			}
			copy(b[off:], out)
		}
	}
	return nil
}

// encodeAtomic is v as an elementary value, little-endian.
func encodeAtomic(code uint16, v float64) ([]byte, error) {
	le := binary.LittleEndian
	switch code {
	case enip.TypeREAL:
		return le.AppendUint32(nil, math.Float32bits(float32(v))), nil
	case enip.TypeLREAL:
		return le.AppendUint64(nil, math.Float64bits(v)), nil
	case enip.TypeBOOL:
		if v != 0 {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	}
	i := int64(math.Round(v))
	switch code {
	case enip.TypeSINT, enip.TypeUSINT:
		return []byte{byte(i)}, nil
	case enip.TypeINT, enip.TypeUINT:
		return le.AppendUint16(nil, uint16(i)), nil
	case enip.TypeDINT, enip.TypeUDINT, enip.TypeDWORD:
		return le.AppendUint32(nil, uint32(i)), nil
	case enip.TypeLINT:
		return le.AppendUint64(nil, uint64(i)), nil
	case enip.TypeULINT:
		return le.AppendUint64(nil, uint64(v)), nil
	}
	return nil, fmt.Errorf("type 0x%04X is not elementary", code)
}

func serve(args []string) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("fixture", "/fixtures/"+serverFixtures[0]+"/fixture.json", "fixture to serve")
	port := fs.Int("port", 44818, "TCP and UDP port of the encapsulation layer")
	ready := fs.String("ready-file", "/run/ethernetip-interop/ready", "readiness file; empty: none")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		return usage("unexpected argument %q", fs.Arg(0))
	}
	if *port < 1 || *port > math.MaxUint16 {
		return usage("--port %d", *port)
	}
	b, err := os.ReadFile(*path)
	if err != nil {
		logf("fixture: %v", err)
		return 4
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		logf("fixture %s: %v", *path, err)
		return 4
	}
	c, err := newController(&f)
	if err != nil {
		logf("fixture %s: %v", *path, err)
		return 4
	}
	if err := c.s.Start(net.JoinHostPort("0.0.0.0", strconv.Itoa(*port))); err != nil {
		logf("%v", err)
		return 1
	}
	if *ready != "" {
		if err := os.WriteFile(*ready, nil, 0o600); err != nil {
			logf("readiness file: %v", err)
			c.s.Stop()
			return 1
		}
	}
	emit(map[string]any{"event": "ready", "adapter": adapterName, "address": c.s.Addr(), "fixture": f.Name})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	<-sig
	if *ready != "" {
		_ = os.Remove(*ready)
	}
	c.s.Stop()
	emit(map[string]any{"event": "stopped", "adapter": adapterName})
	return 0
}
