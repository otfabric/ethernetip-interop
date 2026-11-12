# ethernetip-interop-otfabric

[go-ethernetip](https://github.com/otfabric/go-ethernetip), OT Fabric's own
EtherNet/IP stack, behind the [container contract](../../docs/CONTAINER_CONTRACT.md),
so that other stacks can test against it. Anything that uses an
`ethernetip-interop-<adapter>` image can use this one in its place. Unlike those,
it is not a reference: its tags are go-ethernetip versions, and no harness
release or `manifest.json` contains it.

**For testing only.** It runs an unauthenticated EtherNet/IP target that accepts
writes to its assemblies and tags. Do not expose it to a network you do not
control. Old images stay available, so check
[go-ethernetip advisories](https://github.com/otfabric/go-ethernetip/security/advisories)
for the version you pull.

**Not an independent implementation.** The image declares
`upstream.independentEngine: false`, and `results.json` records
`independent: false`. A result against it says nothing independent about
go-ethernetip when OT Fabric produces it. A result you publish, naming the
implementation and version, is independent evidence, and we would like to hear
about it.

## Use

```sh
docker network create eip
docker run -d --name go-ethernetip --network eip ghcr.io/otfabric/ethernetip-interop-otfabric:<tag> server
until docker exec go-ethernetip test -f /run/ethernetip-interop/ready; do sleep 0.1; done
```

Class 1 I/O needs your originator on the same Docker network, without NAT
([Networking](../../docs/CONTAINER_CONTRACT.md#networking)); explicit messaging
also works through `-p 127.0.0.1:44818:44818`.

`<tag>` is a go-ethernetip version such as `v0.5.2`, or `latest`, the newest one
published. A rebuild of a version replaces its tag, so pin the digest if you need
an exact build. The digest is in the summary of the
[workflow run](https://github.com/otfabric/ethernetip-interop/actions/workflows/own-go-ethernetip.yml),
and `docker run --rm <image> version` prints the versions.

| | |
|---|---|
| Upstream | go-ethernetip, the latest release when the image was built |
| Licence | MIT; every module's licence is in the image under `/licenses` |
| Adapter name | `otfabric` (the name in documents, as the schema allows) |
| Roles | server (target) and client (originator) |
| Fixtures served | `logix` (the default), `adapter`, `logix-structures` |
| Language | Go |
| Protocol engine | OT Fabric's own |

## Why it is apart from the other adapters

`REQUIREMENTS.md` R1: nothing under `adapters/`, `scripts/`, `tests/` or the
top-level `Makefile` knows a library under test, and CI enforces that. This
directory and `.github/workflows/own-go-ethernetip.yml` are the one exception. The
scripts take the image through `ADAPTER_IMAGE_otfabric`.

No release loop can form (`otfabric/engineering` ADR-0012). This image is built
by hand, from the latest go-ethernetip release, and nothing is committed: this
repository has no pin of go-ethernetip to bump. No release or required check of
go-ethernetip needs the image, and go-ethernetip commits no pin of it. It is not
in a harness release or `manifest.json`.

## Publishing

Run the **Own stack (go-ethernetip)** workflow by hand, from `main`. It finds the
latest go-ethernetip release and the latest harness release, and prints them. It
builds the image, runs the smoke test and the cross-stack self-test against the
thirteen reference images of that harness release on both architectures, then
publishes `:<go-ethernetip version>` and `:latest`. Run from a branch, it stops
before publishing. Nothing runs it automatically, so after a go-ethernetip
release the image stays on the old version until someone does.

```sh
make -f own/go-ethernetip/Makefile image smoke interop    # from the repository root
```

`VERSION=dev` tests against the reference images `make images` built locally.

## How it behaves

- The Logix operations (`read-tag`, `read-tags`, `write-tag`, `tag-list`) go
  through Unconnected_Send to `1/0` (backplane, slot 0) unless `--route` names
  another route; the other operations are sent directly unless `--route` is
  given. With `--connected`, the route is in the Forward_Open path.
- `read-tags` reads the tags one Read Tag at a time, so that a target that
  serves Multiple Service Packets only over Class 3 answers it too.
- `identity` is go-ethernetip's `Client.Identity`: Get_Attributes_All of the
  Identity object `@1/1`, decoded.
- `list-identity --transport udp` sends one unicast datagram to `--host`;
  `list-services` is TCP only (`--transport udp` is a usage error).
- With `echoOutputToInput`, what is written to the output assembly (by
  Set_Attribute_Single or by O->T data) becomes the input assembly's data.

## What it cannot do

Declared `false`, so that a test skips them:

| Feature | Role | Why |
|---------|------|-----|
| `readModifyWrite` | client | go-ethernetip has Read-Modify-Write (`logix.Client.WriteBit`), but the contract's `write-tag --type BOOL` on a bit of an integer or of a BOOL array is not mapped to it by this adapter |
| `templates` | client | go-ethernetip reads templates and decodes structures, but the contract's `write-tag --type STRING`/`JSON` and structure reads are not mapped to it by this adapter |
| `errorsCloseSession` | server | go-ethernetip answers errors with a CIP status and keeps the session |

The `discover` operation (pycomm3's `discover()`) is not offered; use
`list-identity --transport udp`.
