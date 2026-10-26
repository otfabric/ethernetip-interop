# ethernetip-interop Releases

## How a release is made

1. Write the notes of the version below and set `interop_version` in `versions.yaml` to it, in the same change. `make validate` (and CI) fails when RELEASE.md has no notes for `interop_version`.
2. `make ci` on the commit to release.
3. Tag `vX.Y.Z` and push the tag. The release workflow runs one job on an amd64 runner and one on an arm64 runner: each validates (including the notes of the tag), builds every image natively with `ADAPTER_VERSION` set to the tag, runs the smoke test and the cross-stack self-test on those images and pushes exactly those images to GHCR as `<image>:vX.Y.Z-amd64` and `-arm64`. A last job joins the two into the multi-arch `<image>:vX.Y.Z` and creates the GitHub release with the notes and the image digests in its body, `manifest.json` (upstream pins and the digests of the multi-arch images) and `checksums.txt`.
4. Consumers pin the digests.

The notes are the body of the GitHub release, followed by the image digests:
everything after the heading `## vX.Y.Z` up to the next line that is exactly
`---` (`scripts/release-notes.sh vX.Y.Z` prints it). This is the format of
every otfabric repository and the rule of the shared release workflow, so:
one section per version, newest first, each ending with a `---` line, and no
`---` line inside a section.

Tags are immutable. A fix is a new version.

| What changed | Version |
|--------------|---------|
| Contract or fixture format changed incompatibly | major |
| New operation, behaviour, fixture, adapter or document field | minor |
| Upstream pin, base image or bug fix with unchanged contract | patch |

---

## v0.3.0

**Date:** 2026-10-26
**Previous release:** v0.2.0

## Summary

Seven more reference stacks, in five more languages: libplctag (C, with its
ab_server simulator as a target), gologix (Go, client and a target with
Class 1 I/O), pylogix (Python), etherip (Java), eip-rs (Rust),
node-ethernet-ip (TypeScript) and EEIP.NET (C#, a scanner with Class 1 I/O,
multicast and listen-only). Eleven images, all behind container contract
1.0; the self-test now runs every client against four servers.

## Images

| Image | Upstream | Roles |
|-------|----------|-------|
| `ghcr.io/otfabric/ethernetip-interop-libplctag` | libplctag v2.7.2 (`7712ed7`), used under MPL-2.0 | server (ab_server, `logix` fixture) and client |
| `ghcr.io/otfabric/ethernetip-interop-gologix` | gologix v0.44.0-beta (`647a0b8`), MIT | server (`logix`, `adapter` fixtures; Class 1 I/O) and client |
| `ghcr.io/otfabric/ethernetip-interop-pylogix` | pylogix 1.1.6, Apache-2.0 | client |
| `ghcr.io/otfabric/ethernetip-interop-etherip` | etherip main `ce0688a` (V1.0-67), EPL-1.0 | client |
| `ghcr.io/otfabric/ethernetip-interop-eiprs` | eip-rs (rseip) main `16e2236` (0.3.1-2), MIT | client |
| `ghcr.io/otfabric/ethernetip-interop-nodeeip` | node-ethernet-ip 2.0.0, MIT | client |
| `ghcr.io/otfabric/ethernetip-interop-eeip` | EEIP.NET master `58b91a0` (1.6.0), MIT | client (scanner) |

## Changes

- **Adapters**: the seven above, each with its capability document and its
  quirks in [docs/CAPABILITIES.md](docs/CAPABILITIES.md).
- **pycomm3 adapter**: `read-tags` with a single tag works (pycomm3 returns
  a single result instead of a list).
- **Smoke test**: a server is probed with ListIdentity only when it declares
  it, otherwise with a Read Tag of its fixture (ab_server has no
  ListIdentity); a client whose unconnected request a server refuses is
  tried over Class 3 when it declares Class 3.
- **Tooling**: `check-versions.sh`, the self-test and the release workflow
  take the adapters from the Makefile instead of naming them; the release's
  `manifest.json` keys `upstream` and `images` by adapter name (it named
  OpENer, EIPScanner, cpppo and pycomm3 before).
- **CI**: two-hour job limits for the larger build.

---

## v0.2.0

**Date:** 2026-10-26
**Previous release:** v0.1.0

## Summary

More cases, no change to existing behaviour: configuration data for Class 1
connections, Logix bit writes, structures and STRING for pycomm3, and a
fourth fixture, `logix-structures`. Contract 1.0 and fixture schema 1.0
gain optional additions only.

## Changes

- **EIPScanner adapter**: `io --config-data HEX` appends a data segment to the connection path; feature `configData`.
- **pycomm3 adapter**: `write-tag --type BOOL` (bits of integers and BOOL array elements, by Read-Modify-Write), `--type STRING` and `--type JSON` (a structure); features `readModifyWrite` and `templates`.
- **OpENer**: declares `configData` (server).
- **Fixtures**: `logix-structures`, a controller with a structure template, structure, STRING, integer and BOOL array tags; the fixture format gains optional `templates`, and tags `strings` and `fields`, with schema and cross-field rules.
- **Capability schema**: feature `configData`.
- **Self-test**: case `io-config-data` (EIPScanner to OpENer passes).
- **Docs**: quirks found by the new cases ([docs/CAPABILITIES.md](docs/CAPABILITIES.md)): OpENer refuses wrong configuration data with 0x0129 and wants the configuration point in a listen-only path; pycomm3 addresses BOOL arrays by DWORD and needs plain BOOL member types in templates.

---

## v0.1.0

**Date:** 2026-10-26
**Previous release:** none

## Summary

First release: four EtherNet/IP reference stacks behind container contract
1.0, three fixtures, and a cross-stack self-test, on linux/amd64 and
linux/arm64.

## Images

| Image | Upstream | Roles |
|-------|----------|-------|
| `ghcr.io/otfabric/ethernetip-interop-opener` | OpENer master `d8820ef41ce30dda06d213438ba81c81de33ffe2` (v2.3-588) | server (target, with Class 1 I/O) |
| `ghcr.io/otfabric/ethernetip-interop-eipscanner` | EIPScanner master `12c89a57ec2e7449efb1348be52af10be9ef9701` (1.3.0-33) | client (scanner, with Class 1 I/O) |
| `ghcr.io/otfabric/ethernetip-interop-cpppo` | cpppo 5.2.5 (greenery 2.1) | server (Logix-style simulator) and client; GPL-3.0 |
| `ghcr.io/otfabric/ethernetip-interop-pycomm3` | pycomm3 1.2.16 | client (CIP and Logix) |

Base images: Debian trixie-slim and Python 3.14.8 slim-trixie, by digest
([versions.yaml](versions.yaml)).

## Contents

- **Container contract 1.0** ([docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md)): `server`, `client` with thirteen operations (`list-identity`, `discover`, `list-services`, `get-attribute`, `set-attribute`, `get-attributes-all`, `multiple-get`, `identity`, `io`, `read-tag`, `read-tags`, `write-tag`, `tag-list`), `print-capabilities`, `print-fixture`, `version`; the networking rule for Class 1 I/O (originator and target on one Docker network, no NAT).
- **Fixtures** ([docs/FIXTURES.md](docs/FIXTURES.md)): `opener-sample`, `adapter`, `logix`, with a JSON Schema and cross-field validation.
- **Schemas** of the capability and client result documents (`schemas/`).
- **Self-tests**: `make smoke` (the contract of each image) and `make selftest` (every client against every server for what both declare), with every document checked against the schemas. Results in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).
- **Adapters**: the cpppo server now loads the fixture's identity into cpppo (it ran with cpppo's default identity before), stops cleanly on SIGTERM, and refuses fixtures cpppo cannot serve (an output-to-input echo; vendor id, device type or product code above 32767); cpppo's client gained `list-services`. EIPScanner's adapter rejects unknown flags, `--connected`, `--multicast` and `--transport tcp` as usage errors. pycomm3's `list-identity --transport udp` uses `discover()`, and `--timeout-ms` sets its socket timeout. All four: a capability document with upstream, version and features per role; `upstreamVersion` in `version`; `print-fixture` refuses names with path separators.

---
