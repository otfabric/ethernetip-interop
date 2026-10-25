# Plan

Status as of 2026-10-26.

| Phase | Scope | Status |
|-------|-------|--------|
| 1 | Adapters for OpENer, EIPScanner, cpppo and pycomm3; first runs against each other and against a consumer | done |
| 2 | Contract 1.0, fixture schema and validation, capability and result schemas, version manifest, smoke test, cross-stack self-test, CI and release workflows, documentation | done (this branch) |
| 3 | First published images (v0.1.0), consumed by a library's tests | next |

## Done

- Four adapters, each its own image, building from pinned sources on
  linux/arm64 (locally) with the same contract; see
  [docs/CAPABILITIES.md](docs/CAPABILITIES.md).
- `make validate`, `make lint`, `make images`, `make smoke` and
  `make selftest` pass locally on linux/arm64; results in
  [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).
- Fixes found on the way: the cpppo server ignored the fixture's identity
  (`--no-config` disabled its `--config` too) and was killed rather than
  stopped on SIGTERM; cpppo 5.2.5 cannot answer ListIdentity for a vendor id
  above 32767, so it refuses such fixtures.

## Next

- Tag v0.1.0: the release workflow builds and self-tests on amd64 and arm64
  runners and publishes the four images; record the digests in
  [RELEASE.md](RELEASE.md). The amd64 build has not run yet (CI will be its
  first run).
- Self-test coverage that needs another peer: pycomm3's Logix driver against
  a target with a tag list (none of the references has one), OpENer's
  multicast and listen-only connections (EIPScanner has neither).
- An upstream-candidates workflow, as dnp3-interop has, once a stack makes a
  release worth tracking (OpENer and EIPScanner have had none in years).
- Vulnerability scans and SBOMs of the published images.
