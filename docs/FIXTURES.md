# Fixtures

A fixture describes a simulated EtherNet/IP target: its identity, its
assemblies and, for a Logix-style controller, its tags. It contains no
assertions and no implementation detail. A server adapter serves it; a
consumer reads it (`print-fixture`) for the values to expect, and a library
under test can serve the same fixture to the reference originators.

- Format: JSON, [schema](../fixtures/schema/ethernetip-fixture.schema.json), `schemaVersion` `"1.0"` (`fixture_schema` in [versions.yaml](../versions.yaml))
- Location: `fixtures/<name>/fixture.json`, baked into every image under `/fixtures`
- Validation: `make validate` (the schema, then the [cross-field rules](#cross-field-rules) of `scripts/validate-fixtures.py`); a server adapter refuses, with exit 4, a fixture it cannot serve

## The fixtures

| Fixture | What it is | Served by |
|---------|------------|-----------|
| `opener-sample` | What OpENer's POSIX sample application serves: its identity (vendor 1, product code 65001, "OpENer PC") and assemblies 100 (input, 32 octets), 150 (output, 32), 151 (configuration, 10), 152 (input-only), 153 (listen-only), 154 (explicit, 32); output 150 is copied to input 100. It is compiled into OpENer: the fixture describes it and cannot change it | opener |
| `adapter` | The adapter a library under test serves to the reference originators: OpENer's sample layout (minus the explicit assembly) with an identity of its own (vendor 65535, product code 42, revision 3.7, serial 0x12345678, "OT Fabric interop adapter") and the echo of output to input | no reference server: cpppo cannot serve it (the echo, and vendor 65535, see [CAPABILITIES.md](CAPABILITIES.md#cpppo)); it exists for consumers |
| `logix` | A Logix-style controller: the identity of a 1756-L61 (vendor 1, device type 14, product code 54, revision 20.11) with ten controller-scoped tags of atomic types and arrays, and assemblies 100 and 150 as plain attributes (no echo) | cpppo |
| `logix-structures` | A Logix-style controller with a structure template `Motor` (BOOL members in a hidden host SINT, DINT, REAL, INT[4] and a nested STRING), structure tags `Pump` and `Fans[2]`, STRING tags `Message` and `Names[3]`, a DINT `Flags` and a `BOOL[64]` `Bits` for bit writes; no assemblies. Added in v0.2.0 | no reference server: cpppo has no structure templates; it exists for consumers (a library under test serves it to pycomm3) |

## Format

```json
{
  "schemaVersion": "1.0",
  "name": "logix",
  "description": "...",
  "identity": { "vendorId": 1, "deviceType": 14, "productCode": 54, "revision": "20.11",
                "serialNumber": 2882400001, "productName": "1756-L61/B LOGIX5561" },
  "assemblies": [ { "instance": 100, "kind": "input", "size": 32 } ],
  "echoOutputToInput": false,
  "slot": 0,
  "tags": [
    { "name": "Counter", "type": "DINT", "values": [42] },
    { "name": "Samples", "type": "DINT", "count": 300, "pattern": { "multiplier": 7, "offset": -1000 } }
  ]
}
```

| Field | Meaning |
|-------|---------|
| `schemaVersion` | `"1.0"` |
| `name` | The fixture's name, equal to its directory; reported in the server's `ready` event |
| `description` | For people |
| `identity` | The Identity object (class 1, instance 1), as ListIdentity and Get_Attributes_All report it |
| `assemblies` | The Assembly object's instances (class 4), see [Assemblies](#assemblies) |
| `echoOutputToInput` | Whether data written to the output assembly (by Set_Attribute_Single or Class 1 O->T data) appears in the input assembly |
| `slot` | The backplane slot the target answers as, for Unconnected_Send routes (`1/<slot>`) |
| `templates` | Optional (v0.2.0): structure templates, see [Templates](#templates) |
| `tags` | Controller-scoped Logix tags, see [Tags](#tags); empty for a plain adapter |

### Identity

| Field | Range | Meaning |
|-------|-------|---------|
| `vendorId` | 0..65535 | Attribute 1 |
| `deviceType` | 0..65535 | Attribute 2 (12: communications adapter, 14: programmable logic controller) |
| `productCode` | 0..65535 | Attribute 3 |
| `revision` | `"major.minor"`, each 0..255 | Attribute 4 |
| `serialNumber` | 0..4294967295 | Attribute 6 |
| `productName` | 1..32 printable ASCII characters | Attribute 7 (SHORT_STRING: on the wire a length octet and the characters) |

Status (attribute 5) and state are the target's own and not part of the
fixture. In hex, as the documents report them: `get-attribute @1/1/7` gives
the length octet and the characters (`"094f70454e6572205043"` for
"OpENer PC"); `get-attributes-all @1/1` starts with vendor, device type and
product code as little-endian UINTs and the two revision octets.

### Assemblies

| Field | Meaning |
|-------|---------|
| `instance` | 1..65535; unique |
| `kind` | `input` (T->O data, the target produces it), `output` (O->T data, the target consumes it), `config` (configuration data of a Forward_Open), `input-only` and `listen-only` (O->T connection points that carry no data: heartbeat), `explicit` (data for explicit messaging only) |
| `size` | Data size in octets: 0 for `input-only` and `listen-only`, at least 1 for the others |

The data of every assembly starts as zeros. Its attribute 3 (Data) is read
with Get_Attribute_Single and written with Set_Attribute_Single; a Class 1
connection's path names configuration, O->T and T->O instances.

**`echoOutputToInput`**: whatever is written to the output assembly, by
Set_Attribute_Single or by O->T data of a Class 1 connection, becomes the
input assembly's data, so an originator sees its own output come back in
T->O data. The fixture must have exactly one `input` and one `output`
assembly, of the same size.

### Tags

| Field | Meaning |
|-------|---------|
| `name` | Logix tag name: a letter or `_`, then letters, digits, `_`; at most 40 characters. Unique, ignoring case (Logix and cpppo match names without case) |
| `type` | CIP elementary type: `BOOL`, `SINT`, `INT`, `DINT`, `LINT`, `USINT`, `UINT`, `UDINT`, `ULINT`, `REAL`, `LREAL`; or `STRING` (the Logix STRING structure: a DINT length and 82 characters, 88 octets); or the name of a template |
| `count` | Elements of a one-dimensional array, at least 2. Absent: a scalar |
| `values` | The initial value of each element: one for a scalar, `count` for an array |
| `pattern` | Instead of `values`, for an array: `{ "multiplier", "offset" }` |
| `strings` | For a `STRING` tag, instead of `values`: the text of each element, at most 82 printable ASCII characters |
| `fields` | For a structure tag, instead of `values`: one object per element, member name to value (a number, a string for a STRING member, a list for an array member, an object for a structure member); members left out start as zeros |

A `BOOL` array is held as DWORDs, as in Logix: its size is rounded up to a
multiple of 32.

**The pattern generator.** Element *i* (0-based) of a tag with a `pattern`
starts as *i* × `multiplier` + `offset`, rounded to an integer for the
integer types. `Samples` (`DINT`, `count` 300, multiplier 7, offset -1000)
starts as -1000, -993, ..., 1093 (element 299: 299 × 7 - 1000); `Speeds`
(`INT`, `count` 100, multiplier 3, offset 0) as 0, 3, 6, 9, ... Large arrays
need no long value lists, and any element's value can be computed by a
consumer.

A `BOOL` tag holds 0 or 1 (pycomm3 reports `true`/`false`). Every value must
fit its type ([cross-field rules](#cross-field-rules)).

### Templates

```json
"templates": [
  { "name": "Motor", "id": 257, "handle": 23100, "size": 108, "members": [
      { "name": "ZZZZZZZZZZMotor0", "type": "SINT", "offset": 0 },
      { "name": "Enabled", "type": "BOOL", "bit": 0, "offset": 0 },
      { "name": "Speed", "type": "DINT", "offset": 4 },
      { "name": "Setpoints", "type": "INT", "count": 4, "offset": 12 },
      { "name": "Label", "type": "STRING", "offset": 20 } ] }
]
```

| Field | Meaning |
|-------|---------|
| `name` | The structure's name, as the Template object reports it; not an elementary type name or `STRING` |
| `id` | Template instance, 1..4095; unique |
| `handle` | Structure handle that Read Tag replies and Write Tag requests carry; unique |
| `size` | Structure size in octets; a multiple of 4, as Logix aligns structures |
| `members` | In order: `name`, `type` (elementary type, `STRING` or an earlier template), `offset` in octets, `count` for an array member, `bit` (0..7) for a BOOL member: its bit in the host octet at `offset` |

A member whose name starts with `ZZZZZZZZZZ` or `__` is hidden, as in
Logix: the host SINT of BOOL members is such a member, declared like any
other.

## Cross-field rules

`scripts/validate-fixtures.py` checks, beyond the schema:

- the fixture's `name` is its directory's name;
- revision major and minor are 0..255;
- assembly instances are unique;
- `input-only` and `listen-only` assemblies have size 0, the others at least 1;
- with `echoOutputToInput`, exactly one `input` and one `output` assembly, of the same size;
- tag names are unique, ignoring case;
- template names, ids and handles are unique; members are unique, of a known
  type, inside the structure's size; a scalar BOOL member has a `bit`, nothing
  else has; sizes are multiples of 4;
- `strings` has `count` texts of at most 82 characters, for `STRING` tags
  only; `fields` has `count` objects naming members of the template, whose
  values fit the members' types;
- tag types are among the CIP type names above;
- `values` has `count` entries (one for a scalar); `pattern` comes with `count`;
- every initial value, given or generated, fits its type: integers within the
  type's range (`BOOL` 0..1, `SINT` -128..127, ..., `ULINT` 0..2^64-1), `REAL`
  values exactly representable in single precision.

## Adding a fixture

1. Create `fixtures/<name>/fixture.json`; `make validate` must pass.
2. Describe it in the table above, with the adapters that serve it.
3. If a server adapter serves it, add it to that adapter's `fixtures` in its
   capability document, and make the adapter refuse (exit 4) what it cannot
   serve.
4. Every image bakes in every fixture: rebuild the images, and `make smoke`
   checks that `print-fixture` gives the file byte for byte.
