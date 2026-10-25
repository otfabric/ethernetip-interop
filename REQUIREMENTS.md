# Requirements

What this repository must do and must not do. [PLAN.md](PLAN.md) says where
it stands; [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) and
[docs/FIXTURES.md](docs/FIXTURES.md) are the specifications that follow from
it.

## Purpose

Independent, reproducible interoperability validation for EtherNet/IP (CIP
over TCP and UDP) libraries: established third-party implementations,
packaged as containers that all behave the same way, so that a library can be
tested against real implementation diversity without owning any of it.

It is not another EtherNet/IP implementation and not a general
protocol-testing framework.

## R1 - Repository isolation

- R1.1 This repository does not import, build, link against or depend on any
  library under test, and contains no test logic or expectation specific to
  one. In particular it knows nothing about `otfabric/go-ethernetip` outside
  documentation examples (CI checks adapters, fixtures, schemas, scripts, the
  Makefile and `compose.yaml`).
- R1.2 A consumer depends on published images and the documented contract
  only. It does not need a checkout of this repository.
- R1.3 No third-party code from here is copied into a consumer, and the
  contract does not shape a consumer's public API.

## R2 - Reference implementations

- R2.1 In scope:

  | Priority | Implementation | Language | Licence | Roles |
  |----------|----------------|----------|---------|-------|
  | P0 | OpENer | C | adapted BSD (+ ODVA Terms of Usage for products) | target |
  | P0 | EIPScanner | C++ | MIT | originator |
  | P1 | cpppo | Python | GPL-3.0 | target (Logix-style), originator |
  | P1 | pycomm3 | Python | MIT | originator (CIP, Logix) |

- R2.2 Each is its own image. No image contains two implementations.
- R2.3 An adapter is released only when it builds from pinned sources and
  passes the smoke test and the cross-stack self-test for the roles it
  declares.

## R3 - Container contract

- R3.1 Every image has the same entry points: `server`, `client` (one
  operation), `print-capabilities`, `print-fixture`, `version`, with the same
  flags and semantics; a role or operation an image lacks is a usage error.
- R3.2 Servers signal readiness when they actually accept connections, by a
  file and an event. Nothing relies on delays.
- R3.3 Machine-readable output is JSON on stdout with stable field names;
  diagnostics go to stderr; exit codes have fixed meanings.
- R3.4 Every operation is bounded by a timeout or duration.
- R3.5 Servers stop cleanly on SIGTERM and exit non-zero when they cannot
  start.
- R3.6 Capabilities are machine-readable.
- R3.7 The contract states what Class 1 I/O needs from the network
  (originator and target reachable without NAT) and the self-tests meet it.

## R4 - Fixtures

- R4.1 Fixtures describe the simulated target: identity, assemblies, the
  echo of output to input, Logix tags and their initial values.
- R4.2 Fixtures contain no assertions and no names from any library under
  test.
- R4.3 Fixtures are validated against a schema and cross-field rules in CI;
  a server refuses one it cannot serve (exit 4) instead of serving something
  else.

## R5 - Reproducibility

- R5.1 Every upstream is pinned to a commit or exact version with a hash,
  every base image to a digest. `versions.yaml` records all of them and is
  checked against the build files.
- R5.2 Releases are immutable tags; images are published with their digests.
- R5.3 Images are built and self-tested for linux/amd64 and linux/arm64, and
  published for both.

## R6 - Self-tests

- R6.1 The reference stacks are tested against each other for every
  operation both sides declare; what does not apply is reported with the
  reason.
- R6.2 These results are about the references and are never presented as
  results for a consumer.
- R6.3 Malformed packets and fault injection are out of scope: reference
  implementations are not misused as fuzzers.

## R7 - Licensing and safety

- R7.1 Licence notices of all bundled software are preserved in the images;
  GPL source obligations are met by unmodified, pinned, recorded sources.
- R7.2 Third-party code stays in the adapter images and never reaches a
  consumer's code: a consumer talks to an image over the network.
- R7.3 Test services are exposed on loopback or private networks only.
- R7.4 The images exist for interoperability testing only.

## R8 - Scope discipline

- R8.1 Adapters are small and explicit. No generic interoperability
  framework.
- R8.2 A feature enters the contract when an adapter implements it; it is
  self-tested when a reference peer exists for it.
