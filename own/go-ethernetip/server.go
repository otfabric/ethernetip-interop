// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/otfabric/go-ethernetip/cip"
	"github.com/otfabric/go-ethernetip/logix"
	"github.com/otfabric/go-ethernetip/server"
)

// Fixture is the fixture document (docs/FIXTURES.md).
type Fixture struct {
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
		Instance uint32 `json:"instance"`
		Kind     string `json:"kind"`
		Size     int    `json:"size"`
	} `json:"assemblies"`
	EchoOutputToInput bool              `json:"echoOutputToInput"`
	Slot              int               `json:"slot"`
	Templates         []FixtureTemplate `json:"templates"`
	Tags              []FixtureTag      `json:"tags"`
}

// FixtureTag is a tag of a Logix fixture.
type FixtureTag struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Count   int       `json:"count"`
	Values  []float64 `json:"values"`
	Pattern *struct {
		Multiplier float64 `json:"multiplier"`
		Offset     float64 `json:"offset"`
	} `json:"pattern"`
	// Strings and Fields are the initial values of a STRING and of a
	// structure tag.
	Strings []string         `json:"strings"`
	Fields  []map[string]any `json:"fields"`
}

// Value returns element i of the tag's initial value.
func (t FixtureTag) Value(i int) float64 {
	if t.Pattern != nil {
		return float64(i)*t.Pattern.Multiplier + t.Pattern.Offset
	}
	if i < len(t.Values) {
		return t.Values[i]
	}
	return 0
}

// loadFixture reads the fixture at path through a root of its directory: a
// fixture file that is a symbolic link out of the directory is refused.
func loadFixture(path string) (*Fixture, error) {
	dir, name := filepath.Split(filepath.Clean(path))
	if dir == "" {
		dir = "."
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var f Fixture
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("fixture %s: %w", path, err)
	}
	if !slices.Contains(serverFixtures, f.Name) {
		return nil, fmt.Errorf("fixture %s: %q is not one this adapter serves (%s)", path, f.Name, strings.Join(serverFixtures, ", "))
	}
	return &f, nil
}

// revision parses a "major.minor" revision; each part is 0 to 255.
func revision(s string) (cip.Revision, error) {
	a, b, _ := strings.Cut(s, ".")
	ma, err := strconv.ParseUint(a, 10, 8)
	if err != nil {
		return cip.Revision{}, fmt.Errorf("revision %q: major: %w", s, err)
	}
	mi, err := strconv.ParseUint(b, 10, 8)
	if err != nil {
		return cip.Revision{}, fmt.Errorf("revision %q: minor: %w", s, err)
	}
	return cip.Revision{Major: uint8(ma), Minor: uint8(mi)}, nil
}

var assemblyKinds = map[string]server.AssemblyKind{
	"input": server.AssemblyInput, "output": server.AssemblyOutput, "config": server.AssemblyConfig,
	"input-only": server.AssemblyInputOnly, "listen-only": server.AssemblyListenOnly,
	// Explicit messaging only: written and read with Set/Get_Attribute_Single.
	"explicit": server.AssemblyOutput,
}

// newServer builds the target of a fixture, not yet listening.
func newServer(f *Fixture) (*server.Server, error) {
	rev, err := revision(f.Identity.Revision)
	if err != nil {
		return nil, err
	}
	id := cip.Identity{
		VendorID: f.Identity.VendorID, DeviceType: cip.DeviceType(f.Identity.DeviceType), ProductCode: f.Identity.ProductCode,
		Revision: rev, SerialNumber: f.Identity.SerialNumber, ProductName: f.Identity.ProductName, State: 3,
	}
	srv, err := server.New(server.WithIdentity(id), server.WithSlot(f.Slot),
		server.WithIOAddress(net.JoinHostPort("0.0.0.0", "2222")), server.WithMaxPacket(65535))
	if err != nil {
		return nil, err
	}
	var inputs, outputs []*server.Assembly
	for _, a := range f.Assemblies {
		k, ok := assemblyKinds[a.Kind]
		if !ok {
			return nil, fmt.Errorf("assembly %d: kind %q", a.Instance, a.Kind)
		}
		asm, err := srv.AddAssembly(a.Instance, k, a.Size)
		if err != nil {
			return nil, err
		}
		switch a.Kind {
		case "input":
			inputs = append(inputs, asm)
		case "output":
			outputs = append(outputs, asm)
		}
	}
	if f.EchoOutputToInput {
		// The fixture schema allows the echo with exactly one input and one
		// output of the same size.
		if len(inputs) != 1 || len(outputs) != 1 {
			return nil, fmt.Errorf("echoOutputToInput needs one input and one output assembly")
		}
		in := inputs[0]
		outputs[0].OnWrite(func(data []byte, _ bool) { _ = in.SetData(data) })
	}
	if len(f.Tags) > 0 {
		ts := logix.NewTagServer(logix.WithProgramName(f.Name))
		byName, err := addTemplates(ts, f.Templates)
		if err != nil {
			return nil, err
		}
		for _, t := range f.Tags {
			// STRING is a Logix structure here, not CIP's elementary STRING.
			if _, ok := cip.ParseDataType(t.Type); !ok || strings.EqualFold(t.Type, "STRING") {
				err = addStructTag(ts, byName, t)
			} else {
				err = addTag(ts, t)
			}
			if err != nil {
				return nil, err
			}
		}
		ts.Attach(srv)
	}
	return srv, nil
}

func serve(args []string) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fixture := fs.String("fixture", fixtureRoot+"/"+serverFixtures[0]+"/fixture.json", "fixture to serve")
	port := fs.Int("port", 44818, "TCP and UDP port of the encapsulation layer")
	ready := fs.String("ready-file", readyDefault, "readiness file; empty: none")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		return usage("unexpected argument %q", fs.Arg(0))
	}
	if *port < 1 || *port > math.MaxUint16 {
		return usage("--port %d", *port)
	}
	f, err := loadFixture(*fixture)
	if err != nil {
		logf("%v", err)
		return exitInvalidFixture
	}
	srv, err := newServer(f)
	if err != nil {
		logf("fixture %s: %v", *fixture, err)
		return exitInvalidFixture
	}
	if err := srv.Listen(net.JoinHostPort("0.0.0.0", strconv.Itoa(*port))); err != nil {
		logf("%v", err)
		return exitFailed
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	if *ready != "" {
		if err := os.WriteFile(*ready, nil, 0o600); err != nil {
			logf("readiness file: %v", err)
			_ = srv.Close()
			return exitFailed
		}
	}
	emit(map[string]any{"event": "ready", "adapter": adapterName, "address": srv.Addr().String(), "fixture": f.Name})
	select {
	case <-ctx.Done():
		if *ready != "" {
			_ = os.Remove(*ready)
		}
		_ = srv.Close()
		emit(map[string]any{"event": "stopped", "adapter": adapterName})
		return exitOK
	case err := <-done:
		logf("server ended: %v", err)
		return exitFailed
	}
}

func addTag(ts *logix.TagServer, t FixtureTag) error {
	typ, ok := cip.ParseDataType(t.Type)
	if !ok {
		return fmt.Errorf("tag %s: type %q", t.Name, t.Type)
	}
	n := t.Count
	var dims []int
	if n > 0 {
		dims = []int{n}
	} else {
		n = 1
	}
	if err := ts.AddTag(t.Name, typ, dims...); err != nil {
		return err
	}
	if typ == cip.TypeBOOL && len(dims) > 0 {
		// Logix stores BOOL arrays as DWORDs, bit i of the array in bit
		// i%32 of DWORD i/32.
		words := make([]uint64, (n+31)/32)
		for i := 0; i < n; i++ {
			if t.Value(i) != 0 {
				words[i/32] |= 1 << (i % 32)
			}
		}
		v, err := logix.Uints(cip.TypeDWORD, words...)
		if err != nil {
			return err
		}
		return ts.Set(t.Name, v)
	}
	var v logix.Value
	var err error
	if typ.IsFloat() {
		fs := make([]float64, n)
		for i := range fs {
			fs[i] = t.Value(i)
		}
		v, err = logix.Floats(typ, fs...)
	} else {
		is := make([]int64, n)
		for i := range is {
			is[i] = int64(math.Round(t.Value(i)))
		}
		v, err = logix.Ints(typ, is...)
	}
	if err != nil {
		return err
	}
	if typ == cip.TypeBOOL {
		// Logix BOOL tags hold 0 or 1.
		for i := range v.Data {
			if v.Data[i] != 0 {
				v.Data[i] = 1
			}
		}
	}
	return ts.Set(t.Name, v)
}
