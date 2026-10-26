# ethernetip-interop adapter for rseip (Joylei/eip-rs; client role only: a
# generic CIP and Rockwell Logix originator of explicit messages, unconnected
# and Class 3; no Class 1 I/O).
#
# Build from the repository root:
#   docker build -f adapters/eiprs/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-eiprs:dev .
#
# rseip is MIT (copied to /licenses/eip-rs/). Its crates are built from the
# source archive of the pinned commit, verified by its SHA-256; the other
# crates come from crates.io at the versions and checksums of Cargo.lock
# (MIT and/or Apache-2.0; their licence files are copied to /licenses/crates/).

ARG BASE_REGISTRY=docker.io/library
ARG DEBIAN_DIGEST=sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f
ARG RUST_DIGEST=sha256:4cd829461bd5c4d511c32e269da9cb8929223b666519d8004e35fc8d1d771ab7

FROM ${BASE_REGISTRY}/rust:1.98.1-slim-trixie@${RUST_DIGEST} AS build
ARG UPSTREAM_SHA=16e2236afb65acc9c82ebdcfbdcbba72e93b3887
ARG UPSTREAM_SHA256=92cccb84822056e23ab93a2d200f11ebf7a3c6813648eaf081d653d2961da77b
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src/eip-rs
RUN curl -fsSL -o eip-rs.tar.gz "https://github.com/Joylei/eip-rs/archive/${UPSTREAM_SHA}.tar.gz" \
    && echo "${UPSTREAM_SHA256}  eip-rs.tar.gz" | sha256sum -c - \
    && tar xzf eip-rs.tar.gz --strip-components=1 \
    && rm eip-rs.tar.gz
WORKDIR /src/adapter
COPY adapters/eiprs/Cargo.toml adapters/eiprs/Cargo.lock ./
RUN cargo fetch --locked \
    && for d in /usr/local/cargo/registry/src/*/*/; do \
         n="$(basename "$d")"; mkdir -p "/licenses/crates/$n"; \
         cp "$d"LICENSE* "$d"COPYING* "$d"UNLICENSE* "/licenses/crates/$n/" 2>/dev/null || true; \
         rmdir --ignore-fail-on-non-empty "/licenses/crates/$n"; \
       done
COPY adapters/eiprs/src ./src
RUN cargo build --locked --release --offline \
    && install -m 755 target/release/ethernetip-interop /usr/local/bin/ethernetip-interop

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST}
ARG UPSTREAM_SHA=16e2236afb65acc9c82ebdcfbdcbba72e93b3887
ARG UPSTREAM_VERSION=0.3.1-2
ARG ADAPTER_VERSION=dev
ENV ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
COPY --from=build /usr/local/bin/ethernetip-interop /usr/local/bin/ethernetip-interop
COPY --from=build /src/eip-rs/LICENSE /licenses/eip-rs/LICENSE
COPY --from=build /licenses/crates /licenses/crates
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop
USER 10001
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["print-capabilities"]
LABEL org.opencontainers.image.title="ethernetip-interop-eiprs" \
      org.opencontainers.image.description="EtherNet/IP reference originator built on rseip (eip-rs)" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
