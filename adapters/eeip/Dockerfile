# ethernetip-interop adapter for EEIP.NET (client role only: an EtherNet/IP
# scanner/originator of explicit messages and Class 1 I/O connections).
#
# Build from the repository root:
#   docker build -f adapters/eeip/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-eeip:dev .
#
# EEIP.NET is MIT (copied to /licenses/eeip/). Its library is built from the
# source archive of the pinned commit, verified by its SHA-256, unchanged:
# adapters/eeip/EEIP/EEIP.csproj compiles upstream's library sources for
# .NET 10 (upstream's own project targets .NET Framework 4.5.2).
#
# The .NET images come from Microsoft's registry (DOTNET_REGISTRY), pinned by
# the digest of their multi-platform index (linux/amd64 and linux/arm64).
# The managed build is platform-neutral and runs on the build platform.

ARG BASE_REGISTRY=docker.io/library
ARG DOTNET_REGISTRY=mcr.microsoft.com/dotnet
# sdk:10.0 (10.0.403, Ubuntu 24.04)
ARG DOTNET_SDK_DIGEST=sha256:fe0fd4e93bd2fbb47c883b7a6369e5c8d3db8bf01cdc5981f6ee35fbdf3ff4fb
# runtime:10.0 (Microsoft.NETCore.App 10.0.14, Ubuntu 24.04)
ARG DOTNET_RUNTIME_DIGEST=sha256:80a51954ac70925e5783cb1ed35129a4243dcb1cddef20066b67346e17db284d

FROM --platform=$BUILDPLATFORM ${DOTNET_REGISTRY}/sdk:10.0@${DOTNET_SDK_DIGEST} AS build
ARG UPSTREAM_SHA=58b91a0c8e30227b1b97a637635d147a2a9655dc
ARG UPSTREAM_SHA256=8ad4f3e498b6d02b1c65b8fae498100a9f9f90d6f691423e5c80559c4eceff5b
ENV DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1
WORKDIR /src/eeip
# The pins of this file, to tell the release build from a candidate build.
COPY adapters/eeip/Dockerfile /tmp/pins
# The release build checks the archive of the pinned commit against its
# SHA-256. A candidate build ("make candidate-eeip") overrides UPSTREAM_SHA
# with another commit, whose archive has no recorded hash: it says so on
# stderr and leaves /tmp/candidate behind for the steps below.
RUN set -eu; \
    pinned="$(sed -nE 's/^ARG UPSTREAM_SHA=(.+)$/\1/p' /tmp/pins | head -1)"; \
    test -n "${pinned}"; \
    if [ "${UPSTREAM_SHA}" != "${pinned}" ]; then \
        printf '%s' "${UPSTREAM_SHA}" | grep -qE '^[0-9a-f]{40}$' || { echo "UPSTREAM_SHA must be a full commit hash" >&2; exit 1; }; \
        echo "candidate build: EEIP.NET ${UPSTREAM_SHA} instead of the pin ${pinned}, no archive hash to check" >&2; \
        touch /tmp/candidate; \
    fi; \
    curl -fsSL -o eeip.tar.gz "https://github.com/rossmann-engineering/EEIP.NET/archive/${UPSTREAM_SHA}.tar.gz"; \
    if [ ! -e /tmp/candidate ]; then echo "${UPSTREAM_SHA256}  eeip.tar.gz" | sha256sum -c -; fi; \
    tar xzf eeip.tar.gz --strip-components=1; \
    rm eeip.tar.gz
COPY adapters/eeip/EEIP /src/adapter/EEIP
COPY adapters/eeip/src /src/adapter/src
RUN dotnet publish /src/adapter/src/Adapter.csproj -c Release -o /out -p:EeipSource=/src/eeip \
    && rm -f /out/*.pdb

FROM ${DOTNET_REGISTRY}/runtime:10.0@${DOTNET_RUNTIME_DIGEST}
ARG UPSTREAM_SHA=58b91a0c8e30227b1b97a637635d147a2a9655dc
ARG UPSTREAM_VERSION=1.6.0
ARG ADAPTER_VERSION=dev
ENV ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION} \
    DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_EnableDiagnostics=0
COPY --from=build /out /opt/ethernetip-interop
COPY --from=build /src/eeip/LICENSE /licenses/eeip/LICENSE
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
RUN printf '#!/bin/sh\nexec /usr/bin/dotnet /opt/ethernetip-interop/ethernetip-interop.dll "$@"\n' > /usr/local/bin/ethernetip-interop \
    && chmod 755 /usr/local/bin/ethernetip-interop \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop
USER 10001
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["print-capabilities"]
LABEL org.opencontainers.image.title="ethernetip-interop-eeip" \
      org.opencontainers.image.description="EtherNet/IP reference scanner built on EEIP.NET" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
