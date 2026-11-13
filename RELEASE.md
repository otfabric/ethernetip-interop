# ethernetip-interop Releases

## How a release is made

1. Write the notes of the version below and set `interop_version` in `versions.yaml` to it, in the same change. `make validate` (and CI) fails when RELEASE.md has no notes for `interop_version`.
2. `make ci` on the commit to release.
3. Run the `Release` workflow on `main` from the Actions tab and pick the bump (`patch` by default, `minor` or `major`) that gives `vX.Y.Z`, or push the tag `vX.Y.Z` yourself. The release workflow runs one job on an amd64 runner and one on an arm64 runner: each validates (including the notes of the tag), builds every image natively with `ADAPTER_VERSION` set to the tag, runs the smoke test and the cross-stack self-test on those images and pushes exactly those images to GHCR as `<image>:vX.Y.Z-amd64` and `-arm64`. A last job joins the two into the multi-arch `<image>:vX.Y.Z` and creates the GitHub release with the notes and the image digests in its body, `manifest.json` (upstream pins and the digests of the multi-arch images) and `checksums.txt`.
4. Consumers pin the digests.

The notes are the body of the GitHub release, followed by the image digests:
everything after the heading `## vX.Y.Z` up to the next line that is exactly
`---` (`scripts/release-notes.sh vX.Y.Z` prints it). This is the format of
every otfabric repository and the rule of the shared release workflow, so:
one section per version, newest first, each ending with a `---` line, and no
`---` line inside a section.

Tags are immutable. A fix is a new version.

The `ethernetip-interop-otfabric` image is not part of these releases. It has its own
manual workflow and no releases (`own/go-ethernetip/README.md`).

| What changed | Version |
|--------------|---------|
| Contract or fixture format changed incompatibly | major |
| New operation, behaviour, fixture, adapter or document field | minor |
| Upstream pin, base image or bug fix with unchanged contract | patch |

---

## v0.8.2

**Date:** 2026-11-13
**Previous release:** v0.8.1

### Changed

- `cbsigma`: cB-Sigma's enip v0.2.1 (was v0.2.0), which fixes the eight issues this harness reported ([cB-Sigma/enip#1](https://github.com/cB-Sigma/enip/issues/1)–[#8](https://github.com/cB-Sigma/enip/issues/8); tracking #18–#24, #27). The adapter now passes the fixture's identity revision to `SetIdentity`. The 43 entries that no longer reproduce are gone (five of them the own image's `otfabric -> cbsigma`, checked with go-ethernetip v0.6.0) from `tests/known-differences.tsv`. The remaining `cbsigma` pairings get the reason that now applies: libplctag's 1-word Unconnected_Send route (`get-attributes-all`), rseip's tag list without dimensions and pylogix reading the template past its end (`tag-list-structures`). The scanner still sends no Forward_Close for I/O connections.
- `eipscanner`: the adapter always puts the configuration instance segment in the Class 1 connection path, `24 00` for `--config 0`, as EEIP.NET, cB-Sigma's enip and go-ethernetip do. cB-Sigma's target refuses a path without it (0x0315). EIPScanner → cbsigma Class 1 I/O now passes.
- `opener`: OpENer master v2.3-592 (`fb90c99`, was v2.3-591), which drops the libcap dependency. libcap is no longer installed in the image.
- `pylogix`: 1.1.7 (was 1.1.6): a fix for UDT values in Multiple Service Packet reads.
- Base images: golang 1.27.4, Python 3.14.10 slim-trixie, Rust 1.100.0, Node.js 24.22.3, Maven 3.9.16 with Temurin 21, the Temurin 21.0.13 JRE, and new digests of Debian trixie-slim and .NET 10 SDK and runtime (recorded 2026-11-12). Closes #9.
- Cross-stack self-test: 369 passed, 0 failed, 46 known incompatibilities (was 328 passed, 86 known).

---

## v0.8.1

**Date:** 2026-11-06
**Previous release:** v0.8.0

### Changed

- The own image's adapter (`own/go-ethernetip`, built by hand, not in this release) reports a tag's type in `tag-list` as its element type, a structure's as its template's name, read from the Template object (`Motor`), as pycomm3, libplctag, gologix, node-ethernet-ip and cbsigma do. The two `tag-list-structures` waivers of the own image are gone: the **Own stack** workflow runs this release's self-tests, and v0.8.0's still listed them.
- The `otfabric -> cbsigma` Class 1 waivers cite [cB-Sigma/enip#8](https://github.com/cB-Sigma/enip/issues/8) (#27): with go-ethernetip v0.5.1 the scanner sends the configuration instance and the connection opens, but cB-Sigma resets its inactivity timer only on a new CIP sequence count and drops it after a few packets.
- Documentation: the cross-stack matrix of the thirteen references in `docs/COMPATIBILITY.md` (328 passed, 0 failed, 86 known), cB-Sigma/enip#8 in the findings and in `docs/CAPABILITIES.md`, `PLAN.md` brought up to date.

The reference images are unchanged in content (same pins); they are rebuilt and published as `v0.8.1`.

---

## v0.8.0

**Date:** 2026-11-06
**Previous release:** v0.7.0

### Added

- A thirteenth reference stack, `cbsigma`: cB-Sigma's [enip](https://github.com/cB-Sigma/enip) v0.2.0 (Apache-2.0, Go), both roles, image `ghcr.io/otfabric/ethernetip-interop-cbsigma`. The server is enip's `server` package (the controller of cB-Sigma's enip-sim since its v0.2.0; enip-sim is pinned in `versions.yaml` but not linked) serving the `logix` and `logix-structures` fixtures: tags, structures, the tag list and templates, Class 3, Unconnected_Send and Class 1 I/O. The client does explicit messaging, Logix tags over Class 3 with Read and Write Tag Fragmented, the tag list with template names, and Class 1 I/O as scanner.
- It is the first reference server with a tag list and templates: pycomm3, libplctag, gologix, node-ethernet-ip and cbsigma browse it, and pycomm3's Logix cases run against it for the first time. Class 1 I/O now runs between independent scanners and targets both ways (EEIP.NET to cbsigma, cbsigma to OpENer).
- Case `logix-write-fragmented`: a whole 300-element array written (Write Tag Fragmented where the client needs it), read back and restored.
- Case `tag-list-structures`: the tag list of the `logix-structures` fixture, each structure tag named after its template. `tests/cases.tsv` takes an optional sixth column, the fixture a case needs; `scripts/interop.sh` serves it in a second container of each server that has it, and records n/a for the others.

### Changed

- A client's Logix cases need `tagList` from the server only when the client uploads the tag list before any tag service (pycomm3, go-ethernet-ip (loki-os): `TAG_LIST_FIRST` in `scripts/interop.sh`). This opens the Logix cases of the other tag-list clients (libplctag, gologix, rseip, node-ethernet-ip, cbsigma) against cpppo, ab_server and gologix.
- After an I/O case that leaves its connection open (no successful Forward_Close), the server is restarted before the next case: OpENer keeps such a connection's exclusive owner and refuses the next one (0x0106).
- The smoke test compares the identity without the revision when `tests/known-differences.tsv` lists the pairing for `list-identity-tcp` (cbsigma's revision is fixed).

### Known differences

- cB-Sigma enip v0.2.0, reported upstream and tracked here: identity revision fixed at 33.11 ([cB-Sigma/enip#1](https://github.com/cB-Sigma/enip/issues/1), #18), 16-bit Message Router and Connection Manager paths ([#2](https://github.com/cB-Sigma/enip/issues/2), #19), Forward_Close of Class 1 connections ([#3](https://github.com/cB-Sigma/enip/issues/3), #20), case-sensitive tag names ([#4](https://github.com/cB-Sigma/enip/issues/4), #21), empty Unconnected_Send route ([#5](https://github.com/cB-Sigma/enip/issues/5), #22), socket address 0.0.0.0 ([#6](https://github.com/cB-Sigma/enip/issues/6), #23), Read Template at the end of a definition ([#7](https://github.com/cB-Sigma/enip/issues/7), #24). Its scanner sends no Forward_Close (documented upstream as not yet done).
- The pairings the newly opened Logix cases and the new cases fail, with the stack at fault and its source: `tests/known-differences.tsv`; the cB-Sigma findings in `docs/COMPATIBILITY.md`.

---

## v0.7.0

**Date:** 2026-11-06
**Previous release:** v0.6.1

### Added

- `own/go-ethernetip` and the manual workflow **Own stack (go-ethernetip)**: OT Fabric's go-ethernetip behind the container contract, published as `ghcr.io/otfabric/ethernetip-interop-otfabric` with go-ethernetip versions as tags. It is not an independent implementation and not part of this or any release, nor of `manifest.json`.
- The scripts take an image through `ADAPTER_IMAGE_<adapter>`, and `results.json` records whether an image is an independent implementation from what it says of itself.
- `make documents`: the smoke and cross-stack self-tests with every document validated.

### Changed

- The repository layout of the other OT Fabric harnesses: `scripts/selftest.sh` is `scripts/interop.sh` (`make interop`, documents in `reports/interop/`, result ids `interop.<case>`), its cases are in `tests/cases.tsv`, and `scripts/known-bugs.tsv` and `scripts/selftest-known.tsv` are `tests/known-bugs.tsv` and `tests/known-differences.tsv`. The contract is unchanged.
- libplctag's `read-tag` takes `--type`, as the contract lists it, and checks it: the cross-stack Logix Read Tag Fragmented and Write Tag cases ran it with `--type` (exit 2).
- lokieip no longer declares `write-tag`: go-ethernet-ip writes a tag only whole, by instance, and the contract's `write-tag` writes elements from element 0. Its write had never run in the cross-stack self-test (no reference server has a tag list, which it needs).
- `tests/known-differences.tsv`: the incompatibilities of the reference stacks with the own image (libplctag's Unconnected_Send without its route, etherip's ListServices name and its missing LINT and LREAL, cpppo's 15-octet ListServices name, and gologix's Class 1 target), which run only with that image.

---

## v0.6.1

**Date:** 2026-11-01
**Previous release:** v0.6.0

### Changed

- Policy 1.2: build output in `bin/` and `release/`, and shared CI at v3.3.0.

---

## v0.6.0

**Date:** 2026-10-31
**Previous release:** v0.5.0

### Added

- Adds `results.json` in the interop-result-v1 format.
- Policy 1.2: build output in `bin/` and `release/`, and shared CI at v3.3.0.

---

## v0.5.0

**Date:** 2026-10-26
**Previous release:** v0.4.0

## Summary

A weekly check of every pin against the latest upstream, with candidate
builds and self-tests of newer upstreams; OpENer moves to its master head.

## Changes

- **Upstream candidates** workflow (`.github/workflows/candidate.yml`, weekly
  and on demand): `scripts/latest-upstream.py` compares each adapter's pin
  with the newest release (libplctag, gologix, cpppo, pycomm3, pylogix,
  nodeeip) or with the head of the default branch (opener, eipscanner,
  etherip, eiprs, eeip, lokieip); where a pin is behind, the adapter is built
  on the latest upstream and smoke- and cross-stack-tested against the other
  pins. The decision table goes to the Job Summary and to an issue that stays
  open while any pin is behind. Pins are never moved automatically.
- **`make candidate-<adapter> CANDIDATE_VERSION=... CANDIDATE_SHA=...`**:
  every Dockerfile accepts another upstream through `UPSTREAM_VERSION` and
  `UPSTREAM_SHA`. At the pin the release build and its hash checks are
  unchanged; any other value takes a candidate path (no recorded hash, says
  `candidate build: ...` on stderr). The opener and libplctag images now
  write the built upstream into their capability document at build time.
- **`scripts/known-bugs.tsv`**: known bugs of specific upstream versions,
  recorded as `known` by the self-test for candidate builds.
- **OpENer** 2.3-588 (`d8820ef`) → 2.3-591 (`0e1ab7e`), found by the first
  candidate run: the three new upstream commits only update OpENer's own CI
  workflow; the candidate passed the smoke test and the cross-stack self-test
  unchanged (186 passed, 0 failed).

---

## v0.4.0

**Date:** 2026-10-26
**Previous release:** v0.3.0

## Summary

A twelfth reference stack: loki-os's go-ethernet-ip, a Go Logix-oriented
originator (image `ghcr.io/otfabric/ethernetip-interop-lokieip`, WTFPL).

## Changes

- **Adapter** `lokieip`: ListIdentity and ListServices over TCP, generic
  attribute services direct, routed (Unconnected_Send) or over Class 3,
  Multiple Service Packet, Logix read, scalar write, read-tags and tag list.
  Its quirks are in [docs/CAPABILITIES.md](docs/CAPABILITIES.md); two known
  incompatibilities with OpENer and cpppo are in
  `scripts/selftest-known.tsv`.
- **Image labels**: gologix's image declares `MIT AND BSD-2-Clause` (it
  contains crc16), lokieip's `MIT AND WTFPL`.

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
