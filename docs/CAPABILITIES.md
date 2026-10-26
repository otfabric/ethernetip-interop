# Capabilities

`print-capabilities` tells a consumer what an adapter image can do, so that
a test suite can skip what a stack does not support instead of failing on
it. Skip, do not fail, a scenario that needs a role, an operation or a
feature an image does not declare.

A feature is declared `true` when the adapter does it. The ones the
cross-stack self-test can exercise with the four references are exercised
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
| `upstream` | The stack: name, pinned version and commit, licence, language; `independentEngine`: its protocol engine is its own (true for all four) |
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
| `getAttributeSingle`, `setAttributeSingle`, `getAttributesAll` | Sends the service | Serves it on the Identity and Assembly objects |
| `getAttributeList` | | Serves Get_Attribute_List (0x03) |
| `multipleService` | Its `multiple-get` sends a Multiple Service Packet | Serves Multiple Service Packet (0x0A) |
| `unconnectedSend` | Routes requests through Unconnected_Send (0x52 to `@6/1`) | Serves Unconnected_Send |
| `class3` | `--connected`: a Class 3 connection (SendUnitData) | Accepts Class 3 connections |
| `class1` | `io`: Class 1 I/O as originator | Class 1 I/O as target |
| `largeForwardOpen` | Sends Large_Forward_Open (`io --large`; pycomm3: first, for Class 3) | Accepts it |
| `multicast` | `io --multicast` | T->O multicast |
| `inputOnly`, `listenOnly` | Opens input-only (heartbeat O->T) / listen-only connections | Has such connection points |
| `logixTags` | Logix Read Tag / Write Tag (`read-tag`, `read-tags`, `write-tag`) | Serves them on the fixture's tags |
| `readTagFragmented`, `writeTagFragmented` | Uses the fragmented services for arrays | Serves them |
| `readModifyWrite` | Writes bits of integers and of BOOL arrays with Read-Modify-Write (0x4E) (`write-tag --type BOOL`) | Serves Read-Modify-Write (0x4E) |
| `tagList` | Reads the tag list (`tag-list`); a client with it (pycomm3) needs it from the server before any tag service | Serves the Symbol object's instance attribute list (0x55) |
| `templates` | Uploads structure templates and reads and writes structure and STRING tags | Serves Template objects (0x6C) |
| `configData` | Sends configuration data in the Forward_Open (`io --config-data`) | Applies it to the configuration assembly |
| `tcpipObject`, `ethernetLinkObject` | | Has the TCP/IP Interface (0xF5) and Ethernet Link (0xF6) objects |
| `errorsCloseSession` | | Answers some errors by closing the session (encapsulation status) instead of a CIP status |

## Adapters

| | opener | eipscanner | cpppo | pycomm3 |
|---|:-:|:-:|:-:|:-:|
| Upstream | OpENer 2.3-588 (C) | EIPScanner 1.3.0-33 (C++) | cpppo 5.2.5 (Python) | pycomm3 1.2.16 (Python) |
| Server (target) | yes | no | yes | no |
| Client (originator) | no | yes | yes | yes |
| Fixtures served | `opener-sample` | | `logix` | |

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

Features (client / server):

| Feature | opener (server) | eipscanner (client) | cpppo (client / server) | pycomm3 (client) |
|---------|:-:|:-:|:-:|:-:|
| `listIdentityTcp` | yes | no | yes / yes | yes |
| `listIdentityUdp` | yes | yes | yes / yes | yes |
| `listServices` | yes | no | yes / yes | no |
| `listInterfaces` | yes | | – / yes | |
| `getAttributeSingle`, `setAttributeSingle`, `getAttributesAll` | yes | yes | yes / yes | yes |
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
| `readModifyWrite`, `templates` | no | no | – / no | yes |
| `configData` | yes | yes | no / no | no |
| `tagList` | no | no | no / no | yes |
| `tcpipObject`, `ethernetLinkObject` | yes | | – / no | |
| `errorsCloseSession` | no | | – / yes | |

Not exercised by the self-test, for want of a peer among the references:
pycomm3's Logix features (`logixTags`, `readTagFragmented`, `tagList`: its
`LogixDriver` needs a tag list, which cpppo does not serve), OpENer's
`multicast` and `listenOnly` (EIPScanner has neither), `getAttributeList`,
`listInterfaces`, `writeTagFragmented`, `tcpipObject` and
`ethernetLinkObject`; pycomm3's `readModifyWrite` and `templates` (no
reference server has them). A library under test serving the `logix` and
`logix-structures` fixtures exercises them (go-ethernetip's interop tests
do). They are declared from the stacks' sources
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

## Where these come from

The declarations follow the stacks' sources at the pinned versions, read for
these adapters and summarized per stack in the research notes of the first
consumer (EtherNet/IP stack notes: what each stack sends and accepts), and
are checked against each other by the self-test.
