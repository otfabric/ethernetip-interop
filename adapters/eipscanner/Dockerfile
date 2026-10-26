# ethernetip-interop adapter for EIPScanner (client role only: an EtherNet/IP
# scanner/originator of explicit messages and Class 1 I/O connections).
#
# Build from the repository root:
#   docker build -f adapters/eipscanner/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-eipscanner:dev .
#
# EIPScanner is MIT (copied to /licenses/eipscanner/). It is built from the
# source archive of the pinned commit, verified by its SHA-256.

ARG BASE_REGISTRY=docker.io/library
ARG DEBIAN_DIGEST=sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST} AS build
ARG UPSTREAM_SHA=12c89a57ec2e7449efb1348be52af10be9ef9701
ARG UPSTREAM_SHA256=3efbb95bff91501328b5899dd635cfee61ec11f59f3ad7609812fb65211ddc63
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential cmake ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src/eipscanner
# The pins of this file, to tell the release build from a candidate build.
COPY adapters/eipscanner/Dockerfile /tmp/pins
# The release build checks the archive of the pinned commit against its
# SHA-256. A candidate build ("make candidate-eipscanner") overrides UPSTREAM_SHA
# with another commit, whose archive has no recorded hash: it says so on
# stderr and leaves /tmp/candidate behind for the steps below.
RUN set -eu; \
    pinned="$(sed -nE 's/^ARG UPSTREAM_SHA=(.+)$/\1/p' /tmp/pins | head -1)"; \
    test -n "${pinned}"; \
    if [ "${UPSTREAM_SHA}" != "${pinned}" ]; then \
        printf '%s' "${UPSTREAM_SHA}" | grep -qE '^[0-9a-f]{40}$' || { echo "UPSTREAM_SHA must be a full commit hash" >&2; exit 1; }; \
        echo "candidate build: EIPScanner ${UPSTREAM_SHA} instead of the pin ${pinned}, no archive hash to check" >&2; \
        touch /tmp/candidate; \
    fi; \
    curl -fsSL -o eipscanner.tar.gz "https://github.com/nimbuscontrols/EIPScanner/archive/${UPSTREAM_SHA}.tar.gz"; \
    if [ ! -e /tmp/candidate ]; then echo "${UPSTREAM_SHA256}  eipscanner.tar.gz" | sha256sum -c -; fi; \
    tar xzf eipscanner.tar.gz --strip-components=1; \
    rm eipscanner.tar.gz
COPY adapters/eipscanner/CMakeLists.txt /src/adapter/CMakeLists.txt
COPY adapters/eipscanner/src /src/adapter/src
WORKDIR /src/build
RUN cmake -DCMAKE_BUILD_TYPE=Release -DCMAKE_POLICY_VERSION_MINIMUM=3.5 -DEIPSCANNER_SOURCE=/src/eipscanner /src/adapter \
    && make -j"$(nproc)" ethernetip-interop \
    && install -m 755 ethernetip-interop /usr/local/bin/ethernetip-interop

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST}
ARG UPSTREAM_SHA=12c89a57ec2e7449efb1348be52af10be9ef9701
ARG UPSTREAM_VERSION=1.3.0-33
ARG ADAPTER_VERSION=dev
ENV ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
COPY --from=build /usr/local/bin/ethernetip-interop /usr/local/bin/ethernetip-interop
COPY --from=build /src/eipscanner/LICENSE /licenses/eipscanner/LICENSE
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop
USER 10001
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["print-capabilities"]
LABEL org.opencontainers.image.title="ethernetip-interop-eipscanner" \
      org.opencontainers.image.description="EtherNet/IP reference scanner built on EIPScanner" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
