# ethernetip-interop — EtherNet/IP Interoperability Testing with Docker

A Docker-based interoperability testing framework for EtherNet/IP (CIP over
TCP and UDP), providing independent target (adapter) and originator
(scanner) implementations, deterministic fixtures, and automated
cross-stack self-tests. Test EtherNet/IP libraries against established
implementations: OpENer, EIPScanner, cpppo, pycomm3, libplctag, gologix,
pylogix, etherip, eip-rs, node-ethernet-ip, EEIP.NET and loki-os's
go-ethernet-ip. Each reference is
containerized behind the same command-line contract, so a library can be
tested against real implementation diversity without integrating
third-party protocol stacks into its own code.

| Adapter | Implementation | Language | Server (target) | Client (originator) | Image |
|---------|----------------|----------|:------:|:------:|-------|
| `opener` | [OpENer](https://github.com/EIPStackGroup/OpENer) master `d8820ef` (v2.3-588) | C | yes ¹ | no | `ghcr.io/otfabric/ethernetip-interop-opener` |
| `eipscanner` | [EIPScanner](https://github.com/nimbuscontrols/EIPScanner) master `12c89a5` (1.3.0-33) | C++ | no | yes | `ghcr.io/otfabric/ethernetip-interop-eipscanner` |
| `cpppo` | [cpppo](https://github.com/pjkundert/cpppo) 5.2.5 | Python | yes ² | yes | `ghcr.io/otfabric/ethernetip-interop-cpppo` |
| `pycomm3` | [pycomm3](https://github.com/ottowayi/pycomm3) 1.2.16 | Python | no | yes | `ghcr.io/otfabric/ethernetip-interop-pycomm3` |
| `libplctag` | [libplctag](https://github.com/libplctag/libplctag) 2.7.2 | C | yes ³ | yes | `ghcr.io/otfabric/ethernetip-interop-libplctag` |
| `gologix` | [gologix](https://github.com/danomagnum/gologix) 0.44.0-beta | Go | yes ⁴ | yes | `ghcr.io/otfabric/ethernetip-interop-gologix` |
| `pylogix` | [pylogix](https://github.com/dmroeder/pylogix) 1.1.6 | Python | no | yes | `ghcr.io/otfabric/ethernetip-interop-pylogix` |
| `etherip` | [etherip](https://github.com/ornl-epics/etherip) main `ce0688a` (V1.0-67) | Java | no | yes | `ghcr.io/otfabric/ethernetip-interop-etherip` |
| `eiprs` | [eip-rs (rseip)](https://github.com/Joylei/eip-rs) main `16e2236` (0.3.1-2) | Rust | no | yes | `ghcr.io/otfabric/ethernetip-interop-eiprs` |
| `nodeeip` | [node-ethernet-ip](https://github.com/cmseaton42/node-ethernet-ip) 2.0.0 | TypeScript | no | yes | `ghcr.io/otfabric/ethernetip-interop-nodeeip` |
| `eeip` | [EEIP.NET](https://github.com/rossmann-engineering/EEIP.NET) master `58b91a0` (1.6.0) | C# | no | yes ⁵ | `ghcr.io/otfabric/ethernetip-interop-eeip` |
| `lokieip` | [go-ethernet-ip (loki-os)](https://github.com/loki-os/go-ethernet-ip) master `535f797` (2022-12-29) | Go | no | yes | `ghcr.io/otfabric/ethernetip-interop-lokieip` |

¹ OpENer's sample application, a target with Class 1 I/O; it serves the
`opener-sample` fixture (its compiled-in identity and assemblies).
² A Logix-style simulator serving the `logix` fixture; no Class 1 I/O.
³ libplctag's `ab_server` PLC simulator serving the `logix` fixture's tags;
no ListIdentity, no attributes, no tag list.
⁴ A Logix tag server with Class 1 I/O, serving the `logix` and `adapter`
fixtures.
⁵ A scanner with Class 1 I/O, T->O multicast and listen-only connections.

The stacks do not all do the same: OpENer has no Multiple Service Packet and
no Unconnected_Send, EIPScanner no Class 3 and no multicast, cpppo no Class 1
and no tag list, pycomm3's Logix driver needs a tag list from its target, and
the Logix clients differ in bit writes, BOOL arrays, structures and
fragmentation.
Each image says what it supports (`print-capabilities`); see
[docs/CAPABILITIES.md](docs/CAPABILITIES.md).

**Docs:** [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) (commands, JSON documents, exit codes, networking) · [docs/FIXTURES.md](docs/FIXTURES.md) (the simulated targets) · [docs/CAPABILITIES.md](docs/CAPABILITIES.md) · [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) (pins, cross-stack matrix) · [REQUIREMENTS.md](REQUIREMENTS.md) · [PLAN.md](PLAN.md) · [RELEASE.md](RELEASE.md)

## Table of contents

- [What this repository is, and is not](#what-this-repository-is-and-is-not)
- [Quick start](#quick-start)
- [Using the images from a library's tests](#using-the-images-from-a-librarys-tests)
- [Repository layout](#repository-layout)
- [Development](#development)
- [Security](#security)
- [License](#license)

## What this repository is, and is not

It owns the reference implementations, their adapters, the fixtures, the
container contract, the images and the self-tests that show the references
agree with each other.

It does not contain, import, build or know about any library under test.
[otfabric/go-ethernetip](https://github.com/otfabric/go-ethernetip) is a
consumer: it pulls the images and owns its own scenarios, assertions and
compatibility claims. A passing self-test here says the *references*
interoperate; it says nothing about a consumer.

The fixtures describe what a target is; the adapters make each
implementation behave that way; the consumer decides what to assert.

## Quick start

Needs Docker and `jq`.

```sh
make images      # build the four adapter images for this machine
make smoke       # container contract of each image
make selftest    # every client against every server
```

A reference target and a reference originator on one Docker network (images
built by `make images`, tagged `dev`; with a release tag instead of `dev`
they are pulled from GHCR):

```sh
docker network create eip
docker run -d --rm --name ref --network eip ghcr.io/otfabric/ethernetip-interop-opener:dev server
until docker exec ref test -f /run/ethernetip-interop/ready; do sleep 0.1; done
ip=$(docker inspect -f '{{(index .NetworkSettings.Networks "eip").IPAddress}}' ref)
docker run --rm --network eip ghcr.io/otfabric/ethernetip-interop-eipscanner:dev \
  client io --host "$ip" --config 151 --output 150 --input 100 \
  --output-size 32 --input-size 32 --rpi-ms 10 --duration-ms 1000 \
  --output-data "$(printf 'c3%.0s' $(seq 32))" | jq 'del(.log)'
docker stop ref; docker network rm eip
```

```json
{
  "schemaVersion": "1.0",
  "adapter": "eipscanner",
  "operation": "io",
  "ok": true,
  "error": null,
  "forwardOpen": { "status": 0 },
  "sent": 76,
  "received": 84,
  "firstInput": "0000…",
  "lastInput": "c3c3…",
  "timedOut": false,
  "forwardClose": { "status": 0 }
}
```

(abridged: OpENer copies output 150 to input 100, so the output comes back.)
`docker compose up -d --wait` starts OpENer and cpppo on the network
`ethernetip-interop`, with 44818/tcp on 127.0.0.1:44818 and :44819 for
explicit messaging from the host; `make run-opener` and `make run-cpppo` run
one of them the same way. Class 1 I/O does not work from the host through
Docker's NAT; see [Networking](docs/CONTAINER_CONTRACT.md#networking).

Every image has the same commands:

```text
server               a target serving a fixture (opener, cpppo)
client <operation>   an originator running one of: list-identity, discover,
                     list-services, get-attribute, set-attribute,
                     get-attributes-all, multiple-get, identity, io,
                     read-tag, read-tags, write-tag, tag-list
                     (see docs/CONTAINER_CONTRACT.md; not every image has all)
print-capabilities   what this adapter supports, as JSON
print-fixture        a fixture baked into the image
version              adapter and upstream versions, as JSON
```

## Using the images from a library's tests

A consumer needs Docker, the image references and its own test code. It does
not clone this repository. go-ethernetip's `interop` package is the example:
it runs the four images against its own adapter and originator, each way.

**Everything on one Docker network.** Create a network per test run and run
the references and the library on it, the library in a container too (a
statically linked test node mounted into a slim base image is enough). Class 1
I/O needs this: the target sends T->O datagrams to the originator's own
address at UDP 2222, which Docker's port publishing hides. Address the
containers by their address on the network.

**Library as originator, reference as target.** Start the server, wait for
its `ready` event or readiness file (never for a delay), then run the
library's originator against it:

```sh
docker run -d --rm --name ref --network "$net" ghcr.io/otfabric/ethernetip-interop-opener:<tag> server
until docker exec ref test -f /run/ethernetip-interop/ready; do sleep 0.1; done
```

**Library as target, reference as originator.** Serve a fixture with the
library, run a client operation, assert on the result document and exit code:

```sh
docker run --rm --network "$net" ghcr.io/otfabric/ethernetip-interop-pycomm3:<tag> \
  client get-attribute --host <library's address> --class 1 --instance 1 --attribute 7
```

**Expected values** come from the image: `print-fixture adapter` (or
`logix`, `logix-structures`, `opener-sample`) prints the fixture it was built with, so
expectations cannot drift from the image. A library serving the `adapter`
fixture gives the reference originators an identity and assemblies to check.

**What a stack supports** comes from the image too: `print-capabilities`.
Skip, do not fail, a scenario that needs a feature an image declares false.

**Pin by digest** in release qualification
(`ghcr.io/otfabric/ethernetip-interop-opener@sha256:...`); the digests of a
release are in its notes and its `manifest.json`. go-ethernetip reads
`ETHERNETIP_INTEROP_<ADAPTER>_IMAGE` (or `ETHERNETIP_INTEROP_VERSION=dev` for
the local builds of `make images`) to override its pins.

## Repository layout

```text
ethernetip-interop/
├── adapters/
│   ├── opener/          OpENer: Dockerfile, bash entrypoint, static capability document
│   ├── eipscanner/      C++ adapter: Dockerfile, CMakeLists.txt, src/main.cpp
│   ├── cpppo/           Python adapter: Dockerfile, requirements.txt, cpppo_interop.py (GPL-3.0-or-later)
│   ├── pycomm3/         Python adapter: Dockerfile, requirements.txt, pycomm3_interop.py
│   └── LICENSE          MIT, except adapters/cpppo/ (GPL-3.0-or-later)
├── fixtures/
│   ├── schema/          JSON Schema of the fixture format
│   ├── adapter/         the adapter a library under test serves
│   ├── logix/           a Logix-style controller (cpppo)
│   └── opener-sample/   what OpENer's sample application serves
├── schemas/             JSON Schemas of the capability and client result documents
├── scripts/             validate, smoke, cross-stack self-test, version check, latest upstream
├── docs/                contract, fixtures, capabilities, compatibility
├── licenses/            licence texts the adapters refer to
├── versions.yaml        every upstream pin and base image digest
├── compose.yaml         the reference targets on a private network
└── Makefile
```

## Development

```sh
make help        # all targets
make ci          # validate, lint, build, smoke, cross-stack self-test
make validate    # versions.yaml against the build files; fixtures against the schema and the cross-field rules
make lint        # shellcheck
make buildx      # check the other architecture locally, under emulation (CI builds both natively)
scripts/latest-upstream.py opener   # an adapter's pin against the latest upstream
make candidate-opener CANDIDATE_VERSION=<version> CANDIDATE_SHA=<commit>   # self-test an adapter on another upstream than its pin
```

`make smoke ADAPTERS=opener` and `make selftest ADAPTERS="opener eipscanner"`
narrow what is run; `VERBOSE=1` shows every check. The self-test keeps every
result document under `reports/selftest/` with `summary.md` and
`summary.tsv`. See [CONTRIBUTING.md](CONTRIBUTING.md) for adding an adapter,
a fixture or a client operation.

A weekly workflow, **Upstream candidates**, compares every pin with the
latest upstream (the newest release, or the head of the default branch for
the six stacks pinned to a commit), builds and self-tests a candidate image
where a pin is behind, and keeps an issue open with the decision table while
any pin is behind. Pins are never moved automatically; see
[docs/COMPATIBILITY.md](docs/COMPATIBILITY.md#upstream-candidates).

## Security

The servers are unauthenticated EtherNet/IP targets that accept attribute
writes, Class 1 connections and tag writes from anyone who can reach them.
They are test fixtures: `compose.yaml` and the `run-*` targets publish them on
loopback only, the self-tests use private Docker networks, and they must
never be attached to a plant network. See [SECURITY.md](SECURITY.md).

## License

The repository's own code and documentation, and the adapter sources, are
MIT-licensed, except `adapters/cpppo/`, which imports the GPL-3.0 cpppo and is
GPL-3.0-or-later. The images contain the upstream stacks under their own
licences: OpENer under its adapted BSD licence (products implementing
EtherNet/IP also need ODVA's Terms of Usage), EIPScanner and pycomm3 MIT, and
the cpppo image is GPL-3.0 as a whole. All four images are published. See
[LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
