# Contributing to ethernetip-interop

## Ground rules

- **Nothing here may know about a library under test.** No imports, no build steps, no expectations, no names from `otfabric/go-ethernetip` or any other consumer in adapters, fixtures, schemas, scripts, the Makefile or `compose.yaml` (CI checks). Documentation may name a consumer as an example. See [REQUIREMENTS.md](REQUIREMENTS.md) R1.
- **The contract is the product.** Adapters differ inside; their commands, documents and exit codes are the same. A change to [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) or [docs/FIXTURES.md](docs/FIXTURES.md) lands in every adapter it concerns in the same change.
- **Keep result documents compatible.** Consumers read fields such as `ok`, `error`, `identity`, `status`, `data`, `replies`, `forwardOpen`, `sent`, `received`, `lastInput`, `timedOut`, `forwardClose`, `values`, `results`, `tags`, `programName` and `module`, and some compare whole objects: add fields only in a minor contract version, never rename or retype one within a major one.
- **Declare what the adapter does.** A feature is `true` when the adapter does it; exercise it in the self-test when there is a reference peer for it, and say in [docs/CAPABILITIES.md](docs/CAPABILITIES.md) when there is none.
- **Pin everything.** Upstreams by commit or exact version with a hash, base images by digest, all mirrored in [versions.yaml](versions.yaml).

## Setup

Docker (with buildx), `jq`, GNU make, bash and Python 3. Nothing else:
compilers and the Python packages run inside the image builds, and the
schema validation and shellcheck run in containers when they are not
installed.

```sh
make ci     # validate, lint, build, smoke, cross-stack self-test
```

Run `make` for all targets.

## Changing an adapter

1. Edit under `adapters/<adapter>/`.
2. `make image-<adapter> smoke-<adapter>`.
3. `make selftest`: every case that ran before must still pass; look at the documents in `reports/selftest/` for what changed. If two stacks disagree, find out which one is right by the specification before touching an expectation.
4. If behaviour visible to consumers changed, update the contract, the schemas and [docs/CAPABILITIES.md](docs/CAPABILITIES.md) in the same change.

Keep the `SPDX-License-Identifier` header of each adapter source: `MIT`, except `adapters/cpppo/`, which imports the GPL-3.0 cpppo and is `GPL-3.0-or-later`.

## Moving an upstream pin

1. Change the pin in the Dockerfile (`UPSTREAM_VERSION`, `UPSTREAM_SHA`, and `UPSTREAM_SHA256` of a GitHub archive) or in `requirements.txt` (version and `--hash`), and in `versions.yaml`; for OpENer also in `adapters/opener/capabilities.json`. `scripts/check-versions.sh` must pass.
2. `make ci`.
3. Record the new version, and anything it changed, in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md), [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and [RELEASE.md](RELEASE.md).

OpENer and EIPScanner have had no release in years: they are pinned to a
commit of their default branch and fetched as GitHub archives verified by
SHA-256. cpppo and pycomm3 are pinned to their PyPI releases by hash.

## Registries and retries

CI does not pull from Docker Hub: its anonymous rate limit and its token service fail too often on shared runners. The workflows set `BASE_REGISTRY=mirror.gcr.io/library` (base images, which the Dockerfiles pin by digest, so the mirror cannot change what is built) and `TOOLS_REGISTRY=mirror.gcr.io` (BuildKit, QEMU handlers, shellcheck, Python). Locally both default to Docker Hub.

Every command that talks to a registry runs through `scripts/retry.sh`, which repeats it with a growing delay when the output shows a rate limit, a 5xx or a timeout, and returns any other failure at once.

Both architectures are built and self-tested natively, on an amd64 and an arm64 runner; nothing in CI runs under emulation. `make buildx-setup` and `make buildx` remain for checking the other architecture from one machine.

## Adding a client operation or a server behaviour

1. Specify it first: the operation, its flags, its result fields and when it succeeds in the contract; a fixture field and its meaning in [docs/FIXTURES.md](docs/FIXTURES.md) and the fixture schema.
2. Implement it in every adapter whose upstream can do it; the others leave it out of `clientOperations` or declare the feature false, and treat the operation or flag as a usage error (exit 2).
3. Add a case to `scripts/selftest.sh` (`CASES` and `run_case`) with the operations and features it needs, and its check against the fixture.
4. Add the feature to the capability documents, to `schemas/` and to [docs/CAPABILITIES.md](docs/CAPABILITIES.md).

Additive changes bump the contract's minor version; anything a consumer could break on bumps the major version.

## Adding a fixture

See [docs/FIXTURES.md](docs/FIXTURES.md#adding-a-fixture).

## Adding an adapter

1. Evaluate the upstream: does it build from a pinned source on both architectures, which roles does it provide, what is its licence and does it allow publishing an image, is its protocol engine its own? Write the answers into [PLAN.md](PLAN.md) before writing code.
2. Create `adapters/<name>/` with a Dockerfile built from the repository root, following the existing ones: pinned build stage, minimal runtime stage, uid 10001, `/fixtures`, `/licenses`, `THIRD_PARTY_NOTICES.md`, the readiness file and health check for a server, `ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]`, `ADAPTER_VERSION`, `UPSTREAM_VERSION` and `UPSTREAM_SHA` build arguments.
3. Implement the contract. A role the upstream does not have exits 2 with a message and `roles` says `false`. Never fake a behaviour the library does not have.
4. `scripts/smoke.sh <name>` must pass, then `scripts/selftest.sh opener eipscanner cpppo pycomm3 <name>`.
5. Add it to `ADAPTERS` and `PUBLISHED_ADAPTERS` (if its licence permits publishing) in the Makefile, to the release workflow, `versions.yaml`, `scripts/check-versions.sh`, the notices and the documentation tables.

Do not build a shared adapter framework. Adapters in different languages share a contract, not code.

## Style

- Shell: bash, `set -uo pipefail` (or `-euo`), shellcheck-clean at warning level (`make lint`).
- Python: standard library only besides the upstream package; runs with `python3 -I`.
- C++: C++20, `-Wall -Wextra` clean.
- Documentation: plain, specific, no claims that the self-tests do not show.

## Commits and pull requests

One change per pull request, with the commands you ran (`make ci`, or which
parts) and their outcome. Never commit build output, `reports/` or upstream
sources.
