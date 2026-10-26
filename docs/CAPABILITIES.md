# Capabilities

`print-capabilities` tells a consumer what an adapter image can do, so that
a test suite can skip what a stack does not support instead of failing on
it. Skip, do not fail, a scenario that needs a role, an operation or a
feature an image does not declare.

A feature is declared `true` when the adapter does it. The ones the
cross-stack self-test can exercise with the eleven references are exercised
there ([COMPATIBILITY.md](COMPATIBILITY.md)); a few cannot be, for want of a
reference peer, and are marked so below.

## Document

One JSON object on one line ([schema](../schemas/capabilities.schema.json)):

```json
{
  "schemaVersion": "1.0",
  "adapter": "cpppo",
  "adapterVersion": "v0.1.0",
  "protocol": "ethernetip",
  "upstream": { "name": "cpppo", "version": "5.2.5", "revision": "f7c224bbb2389bd26d0088eeb1a93db3a5c4858f",
                "license": "GPL-3.0", "language": "Python", "independentEngine": true },
  "roles": { "server": true, "client": true },
  "fixtures": ["logix"],
  "clientOperations": ["list-identity", "list-services", "get-attribute", "..."],
  "clientFeatures": { "listIdentityTcp": true, "class1": false, "...": true },
  "serverFeatures": { "logixTags": true, "tagList": false, "...": true }
}
```

| Field | Meaning |
|-------|---------|
| `adapterVersion` | Release tag the image was built for, `dev` locally |
| `protocol` | `"ethernetip"` |
| `upstream` | The stack: name, pinned version and commit, licence, language; `independentEngine`: its protocol engine is its own (true for all eleven) |
| `roles` | `server` (target) and `client` (originator) |
| `fixtures` | The fixtures `server` serves, the default first; empty without the server role |
| `clientOperations` | The `client` operations; any other is a usage error |
| `clientFeatures`, `serverFeatures` | Feature flags per role (below); `{}` without the role. An absent feature is false |

## Features

| Feature | Client (originator) | Server (target) |
|---------|---------------------|-----------------|
| `listIdentityTcp`, `listIdentityUdp` | Sends ListIdentity over TCP / UDP | Answers it |
| `listServices` | Sends ListServices (`list-services`) | Answers it |
| `listInterfaces` | | Answers ListInterfaces |
| `getAttributeSingle`, `setAttributeSingle`, `getAttributesAll` | Sends the service | Serves it on the Identity and Assembly objects (gologix: Get_Attributes_All of the Identity object only) |
| `getAttributeList` | | Serves Get_Attribute_List (0x03) |
| `multipleService` | Its `multiple-get` sends a Multiple Service Packet | Serves Multiple Service Packet (0x0A) |
| `unconnectedSend` | Routes requests through Unconnected_Send (0x52 to `@6/1`) | Serves Unconnected_Send |
| `class3` | `--connected`, or its Logix operations: a Class 3 connection (SendUnitData) | Accepts Class 3 connections |
| `class1` | `io`: Class 1 I/O as originator | Class 1 I/O as target |
| `largeForwardOpen` | Sends Large_Forward_Open (`io --large`; pycomm3, libplctag, gologix, pylogix, node-ethernet-ip: first, for Class 3) | Accepts it |
| `multicast` | `io --multicast` | T->O multicast |
| `inputOnly`, `listenOnly` | Opens input-only (heartbeat O->T) / listen-only connections | Has such connection points |
| `logixTags` | Logix Read Tag / Write Tag (`read-tag`, `read-tags`, `write-tag`) | Serves them on the fixture's tags |
| `readTagFragmented`, `writeTagFragmented` | Uses the fragmented services for arrays (Write Tag Fragmented (0x53) for a write that does not fit one request) | Serves them |
| `readModifyWrite` | Writes bits of integers and of BOOL arrays with Read-Modify-Write (0x4E) (`write-tag --type BOOL`) | Serves Read-Modify-Write (0x4E) |
| `tagList` | Reads the tag list (`tag-list`); a client with it (pycomm3) needs it from the server before any tag service | Serves the Symbol object's instance attribute list (0x55) |
| `templates` | Uploads structure templates and reads and writes structure and STRING tags | Serves Template objects (0x6C) |
| `configData` | Sends configuration data in the Forward_Open (`io --config-data`) | Applies it to the configuration assembly |
| `tcpipObject`, `ethernetLinkObject` | | Has the TCP/IP Interface (0xF5) and Ethernet Link (0xF6) objects |
| `errorsCloseSession` | | Answers some errors by closing the session or connection (encapsulation status, or no reply) instead of a CIP status |

## Adapters

| | opener | eipscanner | cpppo | pycomm3 |
|---|:-:|:-:|:-:|:-:|
| Upstream | OpENer 2.3-588 (C) | EIPScanner 1.3.0-33 (C++) | cpppo 5.2.5 (Python) | pycomm3 1.2.16 (Python) |
| Server (target) | yes | no | yes | no |
| Client (originator) | no | yes | yes | yes |
| Fixtures served | `opener-sample` | | `logix` | |

| | libplctag | gologix | pylogix | etherip | eiprs | nodeeip | eeip |
|---|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| Upstream | libplctag 2.7.2 (C) | gologix 0.44.0-beta (Go) | pylogix 1.1.6 (Python) | etherip 1.0-67 (Java) | rseip 0.3.1-2 (Rust) | node-ethernet-ip 2.0.0 (TypeScript) | EEIP.NET 1.6.0 (C#) |
| Server (target) | yes (ab_server) | yes | no | no | no | no | no |
| Client (originator) | yes | yes | yes | yes | yes | yes | yes |
| Fixtures served | `logix` | `logix`, `adapter` | | | | | |

Client operations:

| Operation | eipscanner | cpppo | pycomm3 |
|-----------|:-:|:-:|:-:|
| `list-identity` | UDP only | TCP, UDP | TCP, UDP (`discover()`) |
| `discover` | | | yes |
| `list-services` | | yes | |
| `get-attribute`, `set-attribute`, `get-attributes-all` | yes | yes | yes (also `--connected`) |
| `multiple-get` | yes | yes | |
| `identity` | Get_Attributes_All `@1/1` | | `get_module_info(0)` (Unconnected_Send) |
| `io` | yes | | |
| `read-tag`, `read-tags`, `write-tag` | | yes | yes |
| `tag-list` | | | yes |

| Operation | libplctag | gologix | pylogix | etherip | eiprs | nodeeip | eeip |
|-----------|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| `list-identity` | | TCP only | UDP only (`Discover()`) | TCP, UDP | TCP, UDP | TCP, UDP | UDP only |
| `discover` | | | yes | | | | |
| `list-services` | | yes | | TCP, UDP | TCP only | | |
| `get-attribute`, `set-attribute`, `get-attributes-all` | yes (also `--connected`) | yes (always connected) | yes (unconnected only) | `get-attribute`, `get-attributes-all` only | yes (also `--connected`) | yes (also `--connected`) | yes |
| `multiple-get` | | yes | | | yes | yes | |
| `identity` | Get_Attributes_All `@1/1` (`@identity` tag) | | `GetModuleProperties` (Unconnected_Send) | etherip's identity (`--route 1/N`: of slot N) | | Get_Attributes_All `@1/1` | Get_Attributes_All `@1/1` |
| `io` | | | | | | | yes |
| `read-tag`, `read-tags`, `write-tag` | yes | yes | yes | yes | yes | yes | |
| `tag-list` | yes | yes | yes | | yes | yes | |

Features (client / server; an absent feature is blank, or "–" in a
client / server cell):

| Feature | opener (server) | eipscanner (client) | cpppo (client / server) | pycomm3 (client) |
|---------|:-:|:-:|:-:|:-:|
| `listIdentityTcp` | yes | no | yes / yes | yes |
| `listIdentityUdp` | yes | yes | yes / yes | yes |
| `listServices` | yes | no | yes / yes | no |
| `listInterfaces` | yes | | – / yes | |
| `getAttributeSingle` | yes | yes | yes / yes | yes |
| `setAttributeSingle` | yes | yes | yes / yes | yes |
| `getAttributesAll` | yes | yes | yes / yes | yes |
| `getAttributeList` | yes | | – / yes | |
| `multipleService` | **no** (0x08) | yes | yes / yes | no |
| `unconnectedSend` | **no** (0x08) | no | yes / yes | yes |
| `class3` | yes | no | no / yes | yes |
| `class1` | yes | yes | no / no | no |
| `largeForwardOpen` | yes | yes | no / yes | yes |
| `multicast` | yes | no | no / no | no |
| `inputOnly` | yes | yes | no / no | no |
| `listenOnly` | yes | no | no / no | no |
| `logixTags` | no | no | yes / yes | yes |
| `readTagFragmented` | no | no | yes / yes | yes |
| `writeTagFragmented` | no | | – / yes | |
| `readModifyWrite` | no | | – / no | yes |
| `templates` | no | | – / no | yes |
| `configData` | yes | yes | – / – | |
| `tagList` | no | no | no / no | yes |
| `tcpipObject`, `ethernetLinkObject` | yes | | – / no | |
| `errorsCloseSession` | no | | – / yes | |

| Feature | libplctag (client / server) | gologix (client / server) | pylogix (client) | etherip (client) | eiprs (client) | nodeeip (client) | eeip (client) |
|---------|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| `listIdentityTcp` | no / no | yes / yes | no | yes | yes | yes | no |
| `listIdentityUdp` | no / no | no / no | yes | yes | yes | yes | yes |
| `listServices` | no / no | yes / no | no | yes | yes | no | no |
| `listInterfaces` | – / no | – / no | | | | | |
| `getAttributeSingle` | yes / no | yes / no | yes | yes | yes | yes | yes |
| `setAttributeSingle` | yes / no | yes / no | yes | no | yes | yes | yes |
| `getAttributesAll` | yes / no | yes / yes | yes | yes | yes | yes | yes |
| `getAttributeList` | – / no | – / no | | | | | |
| `multipleService` | no / yes | yes / no | no | no | yes | yes | no |
| `unconnectedSend` | no / yes | no / yes | yes | yes | yes | no | no |
| `class3` | yes / yes | yes / yes | no | no | yes | yes | no |
| `class1` | no / no | no / yes | no | no | no | no | yes |
| `largeForwardOpen` | yes / yes | yes / yes | yes | no | no | yes | yes |
| `multicast` | no / no | no / no | no | no | no | no | yes |
| `inputOnly` | no / no | no / yes | no | no | no | no | yes |
| `listenOnly` | no / no | no / no | no | no | no | no | yes |
| `logixTags` | yes / yes | yes / yes | yes | yes | yes | yes | no |
| `readTagFragmented` | yes / yes | no / no | yes | no | yes | no | no |
| `writeTagFragmented` | no / yes | no / no | | no | yes | no | |
| `readModifyWrite` | yes / no | no / no | yes | no | yes | yes | |
| `templates` | no / no | no / no | no | no | yes | yes | |
| `configData` | no / no | no / no | | no | no | no | no |
| `tagList` | yes / no | yes / no | yes | no | yes | yes | no |
| `tcpipObject`, `ethernetLinkObject` | – / no | – / no | | | | | |
| `errorsCloseSession` | – / no | – / yes | | | | | |

With libplctag (ab_server) and gologix serving too, these declared features
have a peer among the references: `logixTags` (cpppo, ab_server and gologix
serve the `logix` fixture), `readTagFragmented` and `writeTagFragmented`
(cpppo and ab_server; rseip is the one client of Write Tag Fragmented),
`multipleService` (cpppo, ab_server), `unconnectedSend` (cpppo, ab_server,
gologix), `class3` and `largeForwardOpen` (all four servers), `class1` and
`inputOnly` (OpENer, gologix), and OpENer's `multicast` and `listenOnly`
(EEIP.NET opens both).

Not exercised by the self-test, for want of a peer among the references:
`readModifyWrite` (pycomm3, libplctag, pylogix, rseip, node-ethernet-ip),
`tagList` (pycomm3, libplctag, gologix, pylogix, rseip, node-ethernet-ip)
and `templates` (pycomm3, rseip, node-ethernet-ip): no reference server has
them (ab_server answers 0x55 with 0x08; gologix's server answers it with
instance id, name and type only, which pycomm3 and gologix's client cannot use). For want of
a tag list, pycomm3's Logix features (`logixTags`, `readTagFragmented`) are
not exercised either: its `LogixDriver` needs the tag list first. No
reference client declares OpENer's and cpppo's `getAttributeList`,
`listInterfaces`, or OpENer's `tcpipObject` and `ethernetLinkObject`. A
library under test serving the `logix` and `logix-structures` fixtures can
exercise them. They are declared from the stacks' sources
([research notes](#where-these-come-from)).

## Quirks by adapter

### opener

- Serves the `opener-sample` fixture only: OpENer's sample application has
  its identity and assemblies compiled in. Any other `--fixture` exits 4, any
  `--port` but 44818 exits 2.
- OpENer binds the address of `eth0` (`--interface`), not 0.0.0.0, and waits
  for it. Readiness is a TCP connect to that address.
- No Multiple Service Packet and no Unconnected_Send: both answer 0x08
  (service not supported). An unknown class or instance answers 0x05, an
  unknown attribute 0x14.
- Class 3: the connection path must be `20 02 24 01` (else 0x0315); the
  first request with sequence 0 counts as a duplicate.
- Class 1: exclusive owner `151/150/100`, input only `152`, listen only `153`
  (listen-only needs a multicast producer). Sizes must match exactly
  (32 + 2 + 4 O->T, 32 + 2 T->O): a wrong size is refused with 0x01 / 0x0127
  or 0x0128. At most one exclusive owner. O->T must come from the
  originator's address; T->O goes to the originator's address at 2222: same
  Docker network, no NAT.
- ListIdentity over UDP to a broadcast address is answered after a random
  delay of up to 2 s; unicast at once.
- Configuration data of the wrong size is refused with 0x01 / 0x0129
  (invalid configuration application path), not 0x0126.
- A listen-only connection names the same three points as its owner:
  configuration 151, listen-only 153, input 100; without 151 it is refused
  with 0x012F. Without a multicast owner of input 100 it is refused with
  0x0119.
- Closing a listen-only connection before its owner makes OpENer stop the
  shared multicast production and drop the owner: the owner's Forward_Close
  is then answered 0x01 / 0x0107 (connection not found). Confirmed by tcpdump
  (eeip).

### eipscanner

- Originator only. ListIdentity over UDP only (EIPScanner's
  `DiscoveryManager`, to the unicast address): `--transport tcp` exits 2.
- Packs 16-bit logical segments (`21 00 lo hi`) even for small ids: a target
  must accept both segment sizes.
- No Class 3 (`--connected` exits 2), no multicast (`--multicast` exits 2),
  no Unconnected_Send, no symbolic segments (no Logix tags).
- `multiple-get` packs the Multiple Service Packet in the adapter with
  EIPScanner's request encoder; EIPScanner has no helper for it.
- `io --config-data`: the adapter appends the data segment to EIPScanner's
  connection path (EIPScanner has no parameter for it).
- `io`: EIPScanner binds UDP 2222 itself and sends no T->O socket address
  item, so the target sends T->O to the originator's address. It reports a
  refused Forward_Open only in its log: the adapter reports `status` -1,
  `refused: true` and the log lines. The timeout multiplier is fixed at 1
  (EIPScanner overflows from 6 on).

### cpppo

- Serves the `logix` fixture: identity (from a generated cpppo
  configuration), tags, and assemblies as attributes `@4/<instance>/3`.
  Refuses with exit 4 a fixture with `echoOutputToInput` (cpppo's assemblies
  are plain attributes; nothing copies one to another) and one whose vendor
  id, device type or product code is above 32767: cpppo 5.2.5 packs these
  UINTs of its ListIdentity reply as signed 16-bit integers and drops the
  reply. That rules out the `adapter` fixture (vendor 65535).
- Tags start at zero in cpppo: the adapter writes the fixture's values
  before it signals readiness.
- An unknown tag, class or service is answered with an encapsulation status
  (0x08) and the session is closed (`errorsCloseSession`), not with a CIP
  status. An unknown attribute of a known object gives CIP status 0x08.
  Set_Attribute_Single needs exactly the attribute's size.
- No Class 1 I/O, no Read-Modify-Write, no tag list (0x55), no templates.
- As a client: no Class 3 in the adapter, Read Tag Fragmented continued by
  the adapter at byte offsets while the target answers 0x06 (`--type` gives
  the element size), Set_Attribute_Single sent as a typed operation (an
  untyped one would be Write Tag).
- ListIdentity reports the address of the generated configuration (the
  container's address) and port 44818.
- ListServices: its item carries a 15-octet service name where the
  specification has 16 octets; rseip and gologix cannot parse it.

### pycomm3

- Originator only, explicit messaging only (no implicit messaging: no `io`).
- `list-identity` is TCP; `--transport udp` and `discover` use pycomm3's
  `discover()`, which sends ListIdentity over UDP to port 44818 of
  `--host`.
- Identity documents carry names (`vendorName`) where the wire has codes;
  `vendorId`/`deviceType` are recovered from pycomm3's own tables and are
  null for a code it does not know.
- `identity` routes `get_module_info(0)` through Unconnected_Send: a target
  without Unconnected_Send (OpENer) cannot answer it.
- `LogixDriver` (`read-tag`, `read-tags`, `write-tag`, `tag-list`) opens with
  Unconnected_Send, a Class 3 connection, the program name (class 0x64) and
  the tag list (0x55 on 0x6B): a target without them (cpppo, OpENer) cannot
  serve its tag operations. pycomm3 1.2.16 sends Write Tag with the request
  repeated in one Connected Data item; Logix ignores the extra octets.
- Bits (`write-tag --type BOOL` of `Tag.N` or `Bits[N]`) are written with
  Read-Modify-Write. pycomm3 addresses a BOOL array by DWORD
  (`Bits[37]` is sent as element 1, bit 5), as Logix does, and expects the
  tag list to report BOOL arrays as DWORD arrays. pycomm3 1.2.16 sends the
  AND mask as 8 octets whatever the mask size.
- Structure templates: a BOOL member's type must be plain BOOL (0x00C1),
  its bit in the member's info word; pycomm3 reads any other type as a
  template id. Structure writes (`--type JSON`) need every member.
- Logix results carry pycomm3's error text, not the CIP status: `status` is
  0 or -1 ([CONTAINER_CONTRACT.md](CONTAINER_CONTRACT.md#result-document)).
- `generic_message` is sent without route path: pycomm3 would otherwise
  append the route to the request data.

### libplctag

- The server is libplctag's `ab_server` (`--plc=ControlLogix`, path
  `1,<slot>`), serving the `logix` fixture's tags only; the fixture's
  identity and assemblies are ignored. `logix-structures`, `adapter`,
  `opener-sample`, a fixture without tags and one with USINT, UINT, UDINT or
  ULINT tags exit 4. Types: BOOL (one octet), SINT, INT, DINT, LINT, REAL,
  LREAL, STRING; scalars are one-element arrays; tag names are
  case-sensitive.
- Tags start at zero in ab_server: the adapter writes the fixture's values
  through libplctag before it signals readiness, and stops ab_server with
  SIGINT.
- ab_server has no ListIdentity, ListServices or ListInterfaces (an unknown
  encapsulation command answers status 0x7D2) and no UDP; no Identity or
  Assembly object (Get/Set attribute answer 0x08), no tag list (0x55
  answers 0x08), no Read-Modify-Write. A RegisterSession with a non-zero
  sender context is refused: pycomm3 cannot connect.
- Client, unconnected generic requests (`@raw`): libplctag 2.7.2 copies the
  empty encoded name instead of the connection path, so the route `01 00` is
  missing and targets answer 0x13 (OpENer 0x08; cpppo tolerates it). Hence
  `unconnectedSend` false; use `--connected`, which runs libplctag's
  Micro800 mode: Forward_Open (0x54), path `20 02 24 01`.
- Logix operations always open Large_Forward_Open (0x5B) with path
  `01 00 20 02 24 01`, and Forward_Open only when that is not supported:
  OpENer refuses with 0x0315 (libplctag retries until the timeout); cpppo
  replies on the O->T connection id, libplctag drops the reply: timeout.
- Every tag is read once when it is created, to learn its type; reads always
  use Read Tag Fragmented (0x52) from offset 0; `read-tags` is packed in one
  Multiple Service Packet; `write-tag` writes in the type read, and the
  adapter refuses a `--type` that does not match. libplctag fragments a
  write only above about 4000 octets (`writeTagFragmented` false).
- Logix results: `status` 0 or -1 and `errorText` (libplctag's error names,
  no CIP status).
- Bits: `Flags.3` by Read-Modify-Write (0x4E); `Bits[37]` is not translated
  (out of bounds), libplctag's own form `Bits[1].5` works.
- Structures are read as raw hex with `type` null; member paths work; no
  structure writes. `tag-list` reports STRING as ASCIISTRING82, BOOL arrays
  as DWORD, `programName` null.
- `identity` reads libplctag's `@identity` tag (`plc=generic`): a direct
  Get_Attributes_All of `@1/1`; a CIP error gives `ok: false`. No
  `list-identity`, `list-services`, `multiple-get` or `io`.

### gologix

- Client: always connected: Large_Forward_Open (connection size 4000), then
  Forward_Open, and SendUnitData; `--connected` changes nothing.
  `list-identity` and `list-services` go over the TCP session after the
  Forward_Open; no UDP.
- Logix operations route to `1/0` by default (OpENer refuses it), generic
  requests have no route by default. The adapter packs `multiple-get`
  (gologix's `GenericCIPMessage`).
- Arrays are read with Read Tag (0x4C), Read Tag Fragmented only after 0x06.
  A structure read without `--type` is hex, type `STRUCT`. An unknown tag
  gives -1: gologix cannot parse the short error reply. In `read-tags` one
  failing tag fails all (0x1E or -1).
- `write-tag --type BOOL` only to BOOL tags: bits are refused (0x2107 /
  0x2105), no Read-Modify-Write; `--type JSON` exits 2; USINT / ULINT are
  sent as BYTE (0xD1) / LWORD (0xD4).
- `tag-list` probes the programs and uploads templates (STRING as
  ASCIISTRING82); `programName` null.
- Forward_Close waits for its reply; gologix's own server never answers it.
- Server: gologix's server runs as a child of the adapter; a panic exits 1.
  A Forward_Open with 16-bit segments panics it (EIPScanner crashes it); a
  Get_Attribute with 16-bit segments answers 0x04. Only 8-bit,
  three-segment paths.
- ListIdentity over TCP only, with status 0x3060 and state 0xFF.
  ListServices is answered with command 0x6F (`listServices` false).
- Get_Attributes_All works on `@1/1` and class 0x64. The Get_Attribute_Single
  reply is malformed (type code and reserved octet prepended; the name with
  a 4-octet length and 84 octets), its connected reply has service 0xD2.
  Set_Attribute only on class 0xC8. No Assembly object.
- Read / Write Tag over Class 3 and through Unconnected_Send, not as direct
  unconnected requests. Read Tag Fragmented through Unconnected_Send answers
  0x08, connected it ignores the offset. Replies are not limited to the
  connection size (1206 octets on 500). No Write Tag Fragmented, no
  Read-Modify-Write, no templates.
- Multiple Service Packet only connected and only with embedded Read / Write
  Tag (embedded Get_Attribute_Single replies are empty); unconnected 0x0A
  answers 0x08.
- The tag list (0x55) ignores the requested attributes and sends instance
  id, name and type only: pycomm3 and gologix's client fail on it
  (`tagList` false).
- Errors close the connection (0x28 then close, or no reply):
  `errorsCloseSession`. The adapter's tag provider keeps the fixture's types
  and sizes; USINT / ULINT cannot be served; names are case-insensitive.
- Class 1: the connection is resolved by the first two path octets (the
  adapter routes `20 04`), connection points are not checked, any sizes are
  accepted; input-only works; listen-only is accepted as an ordinary
  connection (`listenOnly` false). T->O goes to the originator's TCP address
  at 2222, from an ephemeral port. Configuration data is ignored, no
  multicast. A Large_Forward_Open for I/O is accepted but sends no T->O
  ("not yet supported"). Forward_Close is not answered. The output-to-input
  echo is the adapter's provider.
- A SendRRData with more than two common packet format items closes the
  connection (EEIP.NET's Forward_Open adds a socket address item).

### pylogix

- Originator only, explicit messaging only. `list-identity` is pylogix's
  `Discover()`: ListIdentity broadcast to 255.255.255.255, replies for
  0.5 s; the adapter keeps the one from `--host`. No TCP ListIdentity
  (`--transport tcp` exits 2); not through NAT.
- `identity` is `GetModuleProperties(slot)` through Unconnected_Send (OpENer
  answers 0x08: `ok: false`); no `state` (pylogix reads the last octet of
  the product name); `vendorName` "Unknown" for 65535.
- Generic messages are unconnected only (`--connected` exits 2), through
  Unconnected_Send only with `--route`; 8-bit attribute ids. An
  encapsulation error reply without CIP data crashes pylogix's parser:
  `ok: false`, `failed`.
- Logix operations open Large_Forward_Open (4002) then Forward_Open (504);
  Read Tag Fragmented only when a reply does not fit. A one-element Read Tag
  first learns the type; `read-tags` sends the scalars in one Multiple
  Service Packet, arrays alone. Write Tag Fragmented only above 3.9 kB
  (`writeTagFragmented` not declared).
- Template upload asks Get_Attribute_List of attributes 4, 3, 2, 1 and reads
  the reply at fixed offsets. Rockwell's 1756-PM020 documents attributes 1,
  2, 4 and 5 only, and the answer 0x0A (attribute 0x14) for one a target does
  not support; pylogix then misreads the member count. It reads ceil4(attribute 4 × 4 − 23) octets, and a read past
  the end answered with status 0 and no data loops forever (Read Tag to
  0x6C): the adapter's watchdog ends it as `timeout`.
- Structures are read as raw hex, type `STRUCT`; STRING is decoded (handle
  0x0FCE); members read as plain tags. `--type JSON` exits 2.
- `Flags.N` and `Bits[N]` by Read-Modify-Write on the DINT / DWORD; BOOL
  arrays are typed DWORD.
- `tag-list`: `dims` has the first dimension only (16-bit); names with
  `__`, `Routine:`, `Map:`, `Task:`, `UDI:` are filtered; program tags are
  `Program:X.Tag`.
- pylogix's `print()` goes to stderr; a dropped socket is `connection-lost`.

### etherip

- Originator only, explicit messaging only: no Set_Attribute (no
  `set-attribute`), no Class 3 (`--connected` is a usage error), no Class 1,
  no fragmented services, no tag list. `--route 1/N` sends through
  Unconnected_Send to backplane slot N.
- The adapter replaces etherip's transport with its own `Connection`
  subclass (by reflection) to honour `--port` and `--timeout-ms`: etherip
  fixes 44818, binds UDP local port 44818 and has no timeout. Encoding and
  decoding stay etherip's, with its 600-octet buffer.
- etherip's identity adds the octets of vendor id, device type and product
  code (65001 -> 486, 65535 -> 510); `socketAddress` and `state` are null.
- ListServices name "Communications" with the NULs shown as dots.
- One extended status word only.
- No LINT / LREAL (etherip throws: `ok: false`; one in `read-tags` fails
  all). BOOL is the raw signed octet (1 or -1, by target). STRING reads as
  type `STRUCT`; other structures give `values` [] and `errorText`.
- `read-tags` is one Multiple Service Packet through Unconnected_Send; any
  embedded failure throws on the outer status (0x1E, cpppo 0x05).
  `multiple-get` is not offered: etherip's reply decoder reads the whole
  remaining buffer and fails.
- The 600-octet buffer: a 300-DINT read gives 0x06 and a partial reply, a
  140-DINT write overflows the buffer.
- `Flags.3` is encoded as a member-id segment (0x04); `Bits[37]` as element
  37 (0x2105).
- cpppo answers an unknown tag or class with encapsulation status 0x08 and
  accepts INT written to a DINT; OpENer refuses Unconnected_Send with 0x08.
- Every request carries a transaction number in its sender context,
  RegisterSession included; libplctag's ab_server refuses a RegisterSession
  with a non-zero context (encapsulation status 0x0001).

### eiprs

- rseip wraps every unconnected request in Unconnected_Send, even with an
  empty route: without `--route` the adapter sends direct requests through
  `EipContext`, with `--route` through `EipClient` (Unconnected_Send). rseip
  rejects a refused Unconnected_Send (reply 0xD2) as an unexpected service;
  the adapter reports the status (OpENer 0x08).
- Logix operations always go through Unconnected_Send (an empty route
  suits cpppo; a real controller needs `--route 1/0`).
- Class 3 (`--connected`): plain Forward_Open, connection size 504,
  application trigger, path route + `20 02 24 01`; no Large_Forward_Open.
- rseip treats a non-zero status as an error and its decoder rejects more
  than one extended status word: generic replies use the adapter's own reply
  type; Logix statuses are recovered from rseip's error text.
- Multiple Service Packet: rseip's reply iterator cannot size variable
  replies and stops at the first embedded error; the adapter splits the raw
  reply.
- ListServices over TCP only; against cpppo it fails (rseip requires a
  16-octet name, cpppo sends 15). UDP ListIdentity (`EipDiscovery`) always
  goes to port 44818 and prints "cancel rx". rseip has no timeouts: the
  adapter bounds every call.
- `read-tag --count` uses `read_tag_fragmented`, continued by the adapter;
  types rseip's `TagType` lacks (LREAL, unsigned) fall back to one read.
  Writes above 400 octets are fragmented, element-aligned, rseip's
  `TagType`s only. Values are converted by the adapter, structures through
  the template (`find_template`, `read_template`).
- Bits: `Tag.N` by Read-Modify-Write; `Tag[N]` of a DWORD array by
  Read-Modify-Write on `Tag[N/32]`.
- `tag-list` asks attributes 1 and 2 only: `dims` [] (the dimension count
  only), `programName` null; names with `__` or `:` are filtered.
- cpppo answers `tag-list` and Read-Modify-Write with encapsulation status
  0x08; OpENer answers the Multiple Service Packet 0x08.

### nodeeip

- No Unconnected_Send: the slot goes only into the Forward_Open path
  `01 <slot> 20 02 24 01`. Large_Forward_Open (4002, RPI 60 ms, multiplier
  7) then Forward_Open (504); OpENer refuses with 0x01 / 0x0315, so
  `--connected` and the Logix operations fail against OpENer; a wrong slot
  gives 0x0311.
- Forward_Close is sent without connection path (some targets answer 0x13);
  Class 3 sequence numbers start at 0.
- A whole-structure write stores the template instance id (257) as the
  structure handle and is refused 0x2107 (library bug); member and STRING
  writes work.
- Template upload reads attribute 4 × 4 − 23 octets of the definition, as
  Rockwell's *Logix 5000 Controllers Data Access* (1756-PM020) specifies; a
  target that sizes attribute 4 for × 4 − 21 has the last member name
  truncated (`Label` -> `Labe`).
- A STRING in a batch read fails in the library (missing template 0x0FCE):
  the adapter runs its `discover()` and retries.
- BOOL arrays: `Bits[37]` is sent as element 37 (0x2105), `Bits[1].5` is a
  TypeError, the tag list reports BOOL arrays as BIT_STRING (0xD3).
  `Flags.N` is a Read-Modify-Write after a Read.
- No count reads: elements `Tag[i]` are read in Multiple Service Packets
  sized to the connection (about 220 / 4002); a target may refuse more
  than 200 embedded services (0x02), so the adapter sends 100 at a time. A
  multi-value write is one Write Tag per element; no fragmented services.
- Each tag is read once for its type before a write or a batch; `--type` is
  only parsed, the value is encoded in the tag's type (REAL 1.5 into a DINT
  writes 1). ULINT is not supported.
- `status` is the real CIP status with `extendedStatus`; a failing tag fails
  the whole `read-tags`; LINT above 2^53 as strings; `tag-list`
  `programName` null. Encapsulation error replies give a RangeError:
  `ok: false`.
- ListIdentity's `socketAddress` has no port; `identity`'s module lacks
  vendor, device type and product code. `multiple-get` with one path is sent
  bare, not in a Multiple Service Packet. OpENer answers the Multiple
  Service Packet 0x08.

### eeip

- Originator only, no Class 3 (`--connected` exits 2), no configuration
  data, no Multiple Service Packet, no Unconnected_Send.
- EEIP.NET's `ListIdentity()` fails on Linux (broadcast to every interface
  without SO_BROADCAST: EACCES, 1 s per address): the adapter sends one
  unicast datagram, built and parsed by EEIP's classes. No TCP ListIdentity.
- CIP errors come as text only: the adapter maps them back to the status
  through EEIP's table (by reflection); above 0x2B `status` is -1; no
  `extendedStatus`. A refused Forward_Open reports the status and the first
  extended word (0x01 / [0x0127]). EEIP.NET has no timeouts: the adapter
  bounds every call.
- The configuration segment is always sent (`--config 0` sends `24 00`,
  which OpENer refuses with 0x012F); 8-bit connection point segments (up to
  255).
- EEIP's heartbeat format announces an O->T size of 0 without sequence
  count, which targets refuse (0x0127): for input-only and listen-only the
  adapter uses Modeless with 0 octets (size 2 with the sequence count).
- Fixed: originator vendor 0x00FF, serial 0xFFFFFFFF, timeout multiplier 3,
  class 1 cyclic. EEIP's defaults (multicast T->O, redundant owner,
  variable size) are overridden by the adapter. It always sends a T->O
  socket address item of 0.0.0.0:2222; O->T leaves from an ephemeral port.
  With `--multicast` it joins the group from the Forward_Open reply
  (verified by tcpdump).
- `sent`, `received` and `timedOut` are the adapter's: sent from EEIP's
  sequence count (by reflection), received by polling the last T->O message
  every 1 ms, timed out after 32 RPIs without one.
- EEIP.NET parses the Forward_Open reply without checking that one arrived:
  a target that closes the connection instead leaves a zeroed buffer, read
  as success with connection IDs 0. The adapter reports that as
  `connection-lost` ("no Forward_Open reply").

## Where these come from

The declarations follow the stacks' sources at the pinned versions, read for
these adapters and summarized per stack in the research notes of the first
consumer (EtherNet/IP stack notes: what each stack sends and accepts), and
are checked against each other by the self-test.
