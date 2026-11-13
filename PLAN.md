# Plan

Status as of 2026-11-06 (v0.8.1).

| Phase | Scope | Status |
|-------|-------|--------|
| 1 | Adapters for OpENer, EIPScanner, cpppo and pycomm3; first runs against each other and against a consumer | done |
| 2 | Contract 1.0, fixture schema and validation, capability and result schemas, version manifest, smoke test, cross-stack self-test, CI and release workflows, documentation | done |
| 3 | Published images consumed by a library's tests (go-ethernetip) | done (v0.1.0 onwards) |
| 4 | More references: libplctag, gologix, pylogix, etherip, eip-rs, node-ethernet-ip, EEIP.NET, go-ethernet-ip (loki-os), cB-Sigma's enip | done (thirteen, v0.8.0) |
| 5 | OT Fabric's own stack as an image (`own/go-ethernetip`, ADR-0012) | done (v0.7.0) |

## Done

- Thirteen references, each its own image on linux/amd64 and linux/arm64,
  built from pinned sources behind contract 1.0; see
  [docs/CAPABILITIES.md](docs/CAPABILITIES.md). Releases and their notes:
  [RELEASE.md](RELEASE.md).
- `make validate`, `make lint`, `make images`, `make smoke` and
  `make interop` in CI on both architectures; the latest matrix in
  [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).
- The daily **Upstream** workflow compares every pin with its upstream and
  self-tests the latest as a candidate.
- Peers for what used to have none: cbsigma serves a tag list and templates
  (pycomm3's Logix driver and the `tag-list-structures` case), EEIP.NET opens
  OpENer's multicast and listen-only connections, cbsigma and EEIP.NET run
  Class 1 against each other and against OpENer.
- Known incompatibilities, each with the stack at fault and its source:
  [tests/known-differences.tsv](tests/known-differences.tsv); the bugs
  reported upstream are listed in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md#findings).

## Next

- Self-test coverage still without a peer: Read-Modify-Write and structure or
  STRING writes (no reference server has them), Get_Attribute_List,
  ListInterfaces and the TCP/IP and Ethernet Link objects (no reference client
  asks for them).
- Vulnerability scans and SBOMs of the published images.
