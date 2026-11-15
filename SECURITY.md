# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `ethernetip-interop`, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/ethernetip-interop/security) tab → **Advisories** → **Report a vulnerability**), with a description, steps to reproduce and the affected image tag or digest.

Vulnerabilities in the bundled implementations themselves (OpENer, EIPScanner, cpppo, pycomm3, libplctag, gologix, pylogix, etherip, EEIP.NET, cB-Sigma enip, rseip (eip-rs), node-ethernet-ip and go-ethernet-ip (loki-os); [versions.yaml](versions.yaml) names the repository of each) belong upstream; report them there, and tell us so that the pin can be moved.

## Supported Versions

| Version | Supported |
|---------|-----------|
| v0.8.x (latest) | Yes |
| < v0.8 | No |

## Security Considerations

### These are unauthenticated industrial protocol simulators

EtherNet/IP has no authentication in the encapsulation or CIP layers these
images use. The servers are **EtherNet/IP targets that accept, from anyone who
can reach them**, attribute writes (Set_Attribute_Single on assemblies),
Class 1 I/O connections that drive their output assemblies, Class 3
connections and Logix tag writes; they answer ListIdentity broadcasts. The
clients send whatever operation they are given to whatever address they are
given. That is their purpose.

- **Use them on loopback or private Docker networks only.** `compose.yaml` and the `run-*` Makefile targets publish 44818/tcp on `127.0.0.1` only; the self-tests use per-run private Docker networks and publish nothing. Keep it that way
- **Never attach a server to a plant, control or other production network**, and never publish its ports on a routable address: a scanner on that network could connect to it, and it answers discovery
- **Never point a client at a real device**: `set-attribute`, `write-tag` and `io` change what they are sent to; `io` drives a device's outputs for the duration given
- The images contain no credentials, keys or certificates; EtherNet/IP over TLS (CIP Security) is not part of the contract

### Image hardening

- Processes run as a non-root user (uid 10001)
- Base images are pinned by digest; upstream sources by commit and SHA-256 or by exact version and hash ([versions.yaml](versions.yaml))
- The runtime images contain the adapter, its runtime libraries and the fixtures; build tools stay in the build stages (OpENer, EIPScanner), and the Python images have pip removed after installing the pinned packages
- The images are rebuilt, not patched: a base image or upstream fix arrives with a new release

### Supply chain

- Upstream sources are fetched during the image build at pinned versions, each verified by the kind of its source ([versions.yaml](versions.yaml) records the hash):
  - OpENer, EIPScanner, libplctag, etherip, EEIP.NET and rseip (eip-rs): the GitHub archive of a commit, verified against its SHA-256 (`source_sha256`). etherip is built with Maven, whose build plugins come from Maven Central at the versions Maven 3.9.16 and etherip's pom name
  - cpppo, greenery, pycomm3 and pylogix: PyPI, with hash-pinned `requirements.txt` (`pip --require-hashes`; `wheel_sha256`)
  - gologix, cB-Sigma enip and go-ethernet-ip (loki-os): the Go module proxy, verified against the adapter's `go.sum` (`module_sum`)
  - node-ethernet-ip: npm, with `npm ci` against `package-lock.json`, which carries each tarball's integrity hash (`npm_integrity`)
  - the crates rseip depends on: crates.io, verified against the checksums of `adapters/eiprs/Cargo.lock`
- `scripts/check-versions.sh` (CI and the release workflow) fails when `versions.yaml` disagrees with the Dockerfiles, the `requirements.txt` files, the `go.mod` and `go.sum` files or the tool images
- Base images are pinned by digest. CI and the release workflow pull them from `mirror.gcr.io/library`, a mirror of Docker Hub's official images, because anonymous Docker Hub pulls are rate limited; the digest makes the source irrelevant to the content (`BASE_REGISTRY`, default `docker.io/library`). The tooling images (BuildKit, QEMU handlers, shellcheck, Python) come from the same mirror (`TOOLS_REGISTRY`), pinned by version and digest (`versions.yaml` `tooling`); the Python packages of the validators are hash-pinned in `scripts/requirements.txt`, and govulncheck is pinned by version
- CI runs gofmt, `go vet`, the race tests and govulncheck on the Go adapters and on the adapter of OT Fabric's own stack, in the golang image the adapters are built with (`make test-go vulncheck`, and the same targets of `own/go-ethernetip/Makefile`)
- Consumers should pin the published images by digest, taken from the release notes or `manifest.json` of a release
