# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `ethernetip-interop`, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/ethernetip-interop/security) tab → **Advisories** → **Report a vulnerability**), with a description, steps to reproduce and the affected image tag or digest.

Vulnerabilities in the bundled implementations themselves (OpENer, EIPScanner, cpppo, pycomm3) belong upstream; report them there, and tell us so that the pin can be moved.

## Supported Versions

| Version | Supported |
|---------|-----------|
| v0.1.x (latest) | Yes |

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

- Upstream sources are fetched during the image build at pinned versions: OpENer and EIPScanner as GitHub archives of a commit, verified against their SHA-256; cpppo, greenery and pycomm3 from PyPI with hash-pinned `requirements.txt` (`pip --require-hashes`)
- Base images are pinned by digest. CI and the release workflow pull them from `mirror.gcr.io/library`, a mirror of Docker Hub's official images, because anonymous Docker Hub pulls are rate limited; the digest makes the source irrelevant to the content (`BASE_REGISTRY`, default `docker.io/library`). The build tooling images (BuildKit, QEMU handlers, shellcheck, Python) come from the same mirror by tag (`TOOLS_REGISTRY`)
- Consumers should pin the published images by digest, taken from the release notes or `manifest.json` of a release
