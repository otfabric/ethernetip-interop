# Container contract

Version: **1.0** (`contract_version` in [versions.yaml](../versions.yaml))

Every adapter image exposes the same commands, flags, JSON documents and exit
codes. Consumers depend on this document, not on any adapter's source. A
change that breaks it bumps the major version; an addition bumps the minor
one. Documents carry `schemaVersion` `"1.0"`: it names the compatible line
and changes only with the major version.

| Version | Change |
|---------|--------|
| 1.0 | First release |

Not every adapter has every role, operation or flag. What an image has it
says in [`print-capabilities`](#print-capabilities); an operation it does not
list in `clientOperations` is a usage error (exit 2), and so is a flag that
needs a feature the image does not have (`--connected` or `--multicast` on
EIPScanner, `--transport tcp` for its ListIdentity).

What a *server* serves is defined by its fixture: see
[FIXTURES.md](FIXTURES.md).

Roles: `server` is an EtherNet/IP **target** (an adapter, in ODVA's words, or
a Logix-style controller); `client` an **originator** (a scanner, or an
explicit-messaging client).

## Contents

- [Image layout](#image-layout)
- [Commands](#commands)
- [`server`](#server)
- [`client`](#client)
- [Operations](#operations)
- [Result document](#result-document)
- [Networking](#networking)
- [`print-capabilities`](#print-capabilities)
- [`print-fixture`](#print-fixture)
- [`version`](#version)
- [Exit codes](#exit-codes)
- [Output rules](#output-rules)

## Image layout

| Item | Value |
|------|-------|
| Image | `ghcr.io/otfabric/ethernetip-interop-<adapter>`: `opener`, `eipscanner`, `cpppo`, `pycomm3`, `libplctag`, `gologix`, `pylogix`, `etherip`, `eiprs`, `nodeeip`, `eeip`, `lokieip` |
| Entrypoint | `ethernetip-interop` (`/usr/local/bin/ethernetip-interop`) |
| Default command | `server` for images with the server role (opener, cpppo, libplctag, gologix), `print-capabilities` for the others |
| Ports | 44818/tcp and 44818/udp (encapsulation: explicit messaging, ListIdentity, ListServices; libplctag's and gologix's servers: TCP only); 2222/udp (Class 1 I/O; the opener and gologix images) |
| User | non-root, uid 10001 |
| Fixtures | `/fixtures/<name>/fixture.json` (baked in: `adapter`, `logix`, `logix-structures`, `opener-sample`) |
| Readiness file | `/run/ethernetip-interop/ready` |
| Health check | built into the server images: `test -f /run/ethernetip-interop/ready` |
| Licences | `/licenses/` (see [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md)) |

## Commands

```text
ethernetip-interop server [--fixture PATH] [--port N] [--ready-file PATH]
ethernetip-interop client <operation> --host HOST [--port N] [--timeout-ms N] [operation flags]
ethernetip-interop print-capabilities
ethernetip-interop print-fixture [NAME]
ethernetip-interop version
```

Flags are `--name value`; switches (`--connected`, `--large`, `--multicast`)
are bare. An unknown flag is a usage error (exit 2), never ignored. Numbers
are decimal or `0x` hexadecimal (`--class 0x99`).

## `server`

Runs a target that serves one fixture until it receives SIGTERM or SIGINT.

| Flag | Default | Meaning |
|------|---------|---------|
| `--fixture PATH` | the adapter's first fixture (`print-capabilities` `.fixtures[0]`): `/fixtures/opener-sample/fixture.json` (opener), `/fixtures/logix/fixture.json` (cpppo) | Fixture to serve |
| `--port N` | 44818 | TCP and UDP port of the encapsulation layer. OpENer listens on 44818 only: any other value is a usage error there |
| `--ready-file PATH` | `/run/ethernetip-interop/ready` | Readiness file; empty: none |
| `--interface IF` | `eth0` | opener only: the interface OpENer binds to (it binds the interface's address, not 0.0.0.0) |

Lifecycle:

1. The fixture is loaded and checked. A missing or invalid fixture, or one
   the adapter cannot serve (opener: any but `opener-sample`; cpppo: one with
   `echoOutputToInput`, or a vendor id, device type or product code above
   32767, see [CAPABILITIES.md](CAPABILITIES.md#cpppo)), ends the process
   with exit 4 before anything listens.
2. The target is started. If it does not come up, the process exits 1.
3. Once the target accepts TCP connections (cpppo: and the fixture's initial
   tag values are written), the readiness file is written and the `ready`
   event is printed. **Readiness is the file (or the event), not the open
   port and not a delay.**
4. On SIGTERM/SIGINT the readiness file is removed, the target is stopped,
   the `stopped` event is printed and the process exits 0. A target that ends
   any other way exits 1 without `stopped`.

The target's state (assembly data, tag values) belongs to the process and
survives connections: restart the container to return to the fixture.

### Server events

One JSON object per line on stdout.

| `event` | Other fields | When |
|---------|--------------|------|
| `ready` | `adapter`, `address` (`ip:port` the target listens on), `fixture` (its `name`) | Once, when the target accepts connections |
| `stopped` | `adapter` | Once, after a clean stop |

```json
{"event":"ready","adapter":"opener","address":"172.18.0.2:44818","fixture":"opener-sample"}
```

## `client`

Runs one bounded operation as an originator and prints one
[result document](#result-document).

### Common flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--host HOST` | required | Target address. Use an IP address: EIPScanner does not resolve names, and the identity's `socketAddress` is reported as an address |
| `--port N` | 44818 | Encapsulation port of the target |
| `--timeout-ms N` | 5000 | Response timeout of each request (pycomm3: its socket timeout; its UDP discovery keeps its own) |

## Operations

`--class`, `--instance` and `--attribute` default to 0. `--data` and
`--output-data` are hexadecimal octets (`0a1b...`), and a value that is not is
a usage error.

| Operation | Flags | Sends | Adapters |
|-----------|-------|-------|----------|
| `list-identity` | `--transport tcp\|udp` (default `tcp`; EIPScanner: `udp`, and `tcp` is a usage error) | ListIdentity (0x63) over TCP, or one unicast UDP datagram to `--host:--port` | eipscanner, cpppo, pycomm3, gologix, pylogix, etherip, eiprs, nodeeip, eeip, lokieip |
| `discover` | | pycomm3's `discover()` to `--host`: ListIdentity over UDP to port 44818, the first reply | pycomm3, pylogix |
| `list-services` | `--transport tcp\|udp` | ListServices (0x04) | cpppo, gologix, etherip, eiprs, lokieip |
| `get-attribute` | `--class --instance --attribute`, `--connected` | Get_Attribute_Single (0x0E), unconnected (SendRRData) or, with `--connected`, over a Class 3 connection (SendUnitData) | eipscanner, cpppo, pycomm3, libplctag, gologix, pylogix, etherip, eiprs, nodeeip, eeip, lokieip |
| `set-attribute` | `--class --instance --attribute --data HEX`, `--connected` | Set_Attribute_Single (0x10) with the data | eipscanner, cpppo, pycomm3, libplctag, gologix, pylogix, eiprs, nodeeip, eeip, lokieip |
| `get-attributes-all` | `--class --instance`, `--connected` | Get_Attributes_All (0x01) | eipscanner, cpppo, pycomm3, libplctag, gologix, pylogix, etherip, eiprs, nodeeip, eeip, lokieip |
| `multiple-get` | `--path @CLASS/INSTANCE/ATTRIBUTE` (repeatable) | One Multiple Service Packet (0x0A to `@2/1`) of Get_Attribute_Single requests | eipscanner, cpppo, gologix, eiprs, nodeeip, lokieip |
| `identity` | | eipscanner: Get_Attributes_All of the Identity object `@1/1` (EIPScanner's `IdentityObject`). pycomm3: `get_module_info(0)`, the Identity object through Unconnected_Send to backplane slot 0 | eipscanner, pycomm3, libplctag, pylogix, etherip, nodeeip, eeip |
| `io` | see below | Forward_Open (or Large_Forward_Open), Class 1 I/O for a duration, Forward_Close | eipscanner, eeip |
| `read-tag` | `--tag NAME`, `--count N` (1), `--type T` (cpppo: element type of an array, for the fragment offsets), `--route` (cpppo) | Logix Read Tag (0x4C); an array (`--count` > 1) with Read Tag Fragmented (0x52) | cpppo, pycomm3, libplctag, gologix, pylogix, etherip, eiprs, nodeeip, lokieip |
| `read-tags` | `--tags A,B,C`, `--route` (cpppo) | Read Tag of each (cpppo: in sequence; pycomm3: in a Multiple Service Packet) | cpppo, pycomm3, libplctag, gologix, pylogix, etherip, eiprs, nodeeip, lokieip |
| `write-tag` | `--tag NAME --type T --values V1,V2,...`, `--route` (cpppo) | Logix Write Tag (0x4D) of as many elements as values, from element 0; pycomm3: Write Tag Fragmented (0x53) when it does not fit one request, and the types below | cpppo, pycomm3, libplctag, gologix, pylogix, etherip, eiprs, nodeeip, lokieip |
| `tag-list` | | pycomm3's `LogixDriver` open: the controller's tag list (Symbol object 0x6B, Get_Instance_Attribute_List 0x55) and program name | pycomm3, libplctag, gologix, pylogix, eiprs, nodeeip, lokieip |

`--type` names a CIP elementary type: `BOOL`, `SINT`, `INT`, `DINT`, `LINT`,
`USINT`, `UINT`, `UDINT`, `ULINT`, `REAL`, `LREAL`. pycomm3's `write-tag` also
takes (v0.2.0):

| `--type` | `--values` | Sends |
|----------|------------|-------|
| `BOOL` | `true`/`false` or `1`/`0` per element | for a bit of an integer (`Flags.3`) or an element of a BOOL array (`Bits[37]`): Read-Modify-Write Tag (0x4E) on the integer or on the DWORD holding the bit |
| `STRING` | one text (commas included) | Write Tag of a STRING structure |
| `JSON` | one JSON value | a structure as an object of its members (`{"Speed": 5, ...}`): Write Tag of the whole structure |

`--route` (cpppo, pylogix, etherip, eiprs, gologix, node-ethernet-ip): the
route path for Unconnected_Send, for example `1/0` (backplane, slot 0);
node-ethernet-ip takes only `1/<slot>`, which it puts in its Forward_Open
path. The adapters' quirks ([CAPABILITIES.md](CAPABILITIES.md)) say how each
routes by default. pycomm3's `LogixDriver` always routes to slot 0 itself;
its `identity` routes through Unconnected_Send.

`--connected` opens a Class 3 connection (pycomm3: Large_Forward_Open first,
Forward_Open if that is refused) and sends the request over it.

### `io`

| Flag | Default | Meaning |
|------|---------|---------|
| `--config N` | 0 (none) | Configuration assembly instance of the connection path |
| `--output N` | required | O->T connection point (the output assembly, or the input-only / listen-only connection point) |
| `--input N` | required | T->O connection point (the input assembly) |
| `--output-size N` | 0 | O->T application data size in octets; 0 is a heartbeat (input only) |
| `--input-size N` | 0 | T->O application data size in octets |
| `--rpi-ms N` | 10 | Requested packet interval, both directions |
| `--duration-ms N` | 1000 | How long data is exchanged before Forward_Close |
| `--output-data HEX` | zeros | O->T data; must be `--output-size` octets |
| `--large` | | Large_Forward_Open (0x5B) instead of Forward_Open (0x54) |
| `--multicast` | | T->O multicast. Usage error on EIPScanner, which has no multicast |
| `--config-data HEX` | none | Configuration data (v0.2.0): a simple data segment (0x80) at the end of the connection path, for the configuration assembly. An even number of octets, at most 510 (eipscanner) |

The connection is point-to-point (T->O unicast unless `--multicast`),
transport class 1, cyclic; O->T with the 32-bit run/idle header (run), T->O
without. The originator receives T->O at its own address, UDP port 2222: see
[Networking](#networking).

The operation succeeds (`ok: true`) when the Forward_Open was answered, also
when it was refused: the refusal is in `forwardOpen.status`.

## Result document

Exactly one JSON object on one line of stdout, for every invocation that got
past argument parsing, including failures ([schema](../schemas/client-result.schema.json)).

Common fields:

| Field | Type | Meaning |
|-------|------|---------|
| `schemaVersion` | string | `"1.0"` |
| `adapter` | string | Adapter that ran the operation |
| `operation` | string | As given on the command line |
| `ok` | boolean | The exchange completed: the target answered (whatever the CIP status) |
| `error` | object or null | `{ "code", "message" }` when `ok` is false, null otherwise |

**A CIP error is not a failed operation.** A target that answers with a
non-zero general status gives `ok: true` and `status` != 0. `ok: false` means
no usable answer: no connection, a timeout, a closed session, an
encapsulation error, a reply that could not be parsed.

| `error.code` | Meaning |
|--------------|---------|
| `connect-failed` | No TCP connection or session (EIPScanner) |
| `timeout` | No reply in time (ListIdentity, ListServices, discover) |
| `connection-lost`, `protocol` | Reserved for adapters that can tell these apart |
| `failed` | Anything else; see `message` (the library's exception) |

`message` is for people and not part of the contract.

Operation fields (when `ok` is true; a field that does not apply is absent):

| Operation | Fields |
|-----------|--------|
| `list-identity`, `discover` | `identity`: `vendorId`, `deviceType`, `productCode`, `revision` (`"major.minor"`), `status`, `serialNumber`, `productName`, `socketAddress`, and `state` (not EIPScanner). pycomm3 reports vendor and device type by name: `vendorName`, and `vendorId`/`deviceType` are recovered from pycomm3's tables or null; its `socketAddress` is the address without port |
| `list-services` | `services`: `[{ "name", "version", "capabilities" }]` |
| `get-attribute`, `set-attribute`, `get-attributes-all` | `status` (general status), `data` (reply data, hex; `""` for none). EIPScanner adds `extendedStatus` (array of additional status words); pycomm3 adds `errorText` (its message for a non-zero status, or null) |
| `multiple-get` | `status` (of the Multiple Service Packet: 0, or 0x1E when an embedded service failed), `replies`: `[{ "status", "data" }]`, one per `--path`, in order |
| `identity` | eipscanner: `identity` (as above, without `state`). pycomm3: `module`, pycomm3's dict as it returns it (`vendor`, `product_type`, `product_code`, `revision` `{major, minor}`, `serial` (hex string), `product_name`, ...) |
| `io` | `forwardOpen`: `{ "status" }` (0 when the connection opened); on a refusal the general status and `extendedStatus`, or (EIPScanner, which reports a refusal only in its log) `status` -1 and `refused: true`. When it opened: `sent`, `received` (packets), `firstInput`, `lastInput` (T->O data, hex), `timedOut` (the connection timed out during the exchange), `forwardClose`: `{ "status" }` |
| `read-tag` | `status`, `values` (array; numbers, booleans for BOOL with pycomm3), pycomm3: `type`, `errorText` |
| `read-tags` | `results`: `[{ "tag", "values", ... }]` in the order of `--tags`; cpppo adds `status`, pycomm3 `errorText` |
| `write-tag` | `status`; pycomm3: `errorText` |
| `tag-list` | `tags`: `[{ "name", "type", "dims" }]`, `programName` |

pycomm3 reports Logix results by message, not status: its `status` is 0 when
pycomm3 reports no error and **-1** when it does (`errorText` says which).
EIPScanner's I/O documents also carry `log`, its log lines, for diagnosis.

Example:

```json
{"schemaVersion":"1.0","adapter":"eipscanner","operation":"get-attribute","ok":true,"error":null,"status":0,"extendedStatus":[],"data":"094f70454e6572205043"}
```

## Networking

Explicit messaging is TCP to 44818 and works through anything that forwards
TCP, Docker's port publishing included.

**Class 1 I/O needs the originator and the target on the same Docker
network, without NAT.** The target sends T->O datagrams to the originator's
own IP address at UDP port 2222 (EIPScanner sends no T->O socket address
item; OpENer would otherwise use it), and checks that O->T datagrams come
from the address that opened the connection. Through Docker's NAT neither
holds. So:

- run the originator and the target as containers on one user-defined
  network, and address the target by its address on that network
  (`docker inspect`); the self-tests and [compose.yaml](../compose.yaml) do
  this;
- a library under test that originates I/O runs in a container on that
  network too, and binds UDP 2222 there;
- I/O from the host to a container through a published port does not work.

ListIdentity over UDP is answered to the sender's address and works through
NAT only for unicast requests to a published 44818/udp.

## `print-capabilities`

Prints one JSON object on one line and exits 0
([schema](../schemas/capabilities.schema.json)). See
[CAPABILITIES.md](CAPABILITIES.md) for the fields and each adapter's values.
Needs no network and no fixture.

## `print-fixture`

`print-fixture [NAME]` writes the fixture baked into the image to stdout,
byte for byte, and exits 0. The default is the adapter's own fixture
(opener: `opener-sample`, cpppo: `logix`, eipscanner and pycomm3: `adapter`).
An unknown name, or one that is empty or contains `/`, `\` or `..`, is a
usage error (exit 2).

A consumer reads the expected values from the image it tests against, so it
needs no checkout of this repository and cannot drift from the image.

## `version`

Prints one JSON object on one line and exits 0:

```json
{"adapter":"opener","adapterVersion":"v0.1.0","upstream":"OpENer","upstreamVersion":"2.3-588","upstreamRevision":"d8820ef41ce30dda06d213438ba81c81de33ffe2","contract":"1.0"}
```

`adapterVersion` is the release tag the image was built for (`dev` for a
local build).

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success: the operation completed (whatever the CIP status). `server`: clean stop after SIGTERM/SIGINT |
| 1 | The operation failed (`ok: false`, see `error.code`); `server`: the target could not start, or ended without SIGTERM/SIGINT |
| 2 | Usage error: unknown command, operation or flag, a missing or invalid value, a role the image does not have (`client` on opener, `server` on eipscanner and pycomm3), an operation or flag it does not declare, an unknown fixture name. Nothing was sent |
| 4 | `server`: the fixture is missing, invalid, or one the adapter cannot serve |

## Output rules

- **stdout carries JSON only**: server events as JSON Lines, one client
  result, one capability document, the version document. `print-fixture` is
  the exception: it prints the fixture file as it is.
- **stderr carries diagnostics** for people: the libraries' logs (OpENer,
  EIPScanner and cpppo log there). Nothing on stderr is part of the contract.
- Field names and exit codes are stable within a major contract version.
  Fields may be added in a minor version; consumers ignore fields they do not
  know.
- Every wait is bounded: by `--timeout-ms` or `--duration-ms`. No client
  operation runs indefinitely.
