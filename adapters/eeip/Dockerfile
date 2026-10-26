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
ARG DOTNET_SDK_DIGEST=sha256:de4fe90a98845d75910b9769662465c84de1a934665c431279d1382b2bddc02b
# runtime:10.0 (Microsoft.NETCore.App 10.0.14, Ubuntu 24.04)
ARG DOTNET_RUNTIME_DIGEST=sha256:a3f9f81954b9c810af2ed18849716c8dc74a4707bd0907fdbd69be81554c0f30

FROM --platform=$BUILDPLATFORM ${DOTNET_REGISTRY}/sdk:10.0@${DOTNET_SDK_DIGEST} AS build
ARG UPSTREAM_SHA=58b91a0c8e30227b1b97a637635d147a2a9655dc
ARG UPSTREAM_SHA256=8ad4f3e498b6d02b1c65b8fae498100a9f9f90d6f691423e5c80559c4eceff5b
ENV DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1
WORKDIR /src/eeip
RUN curl -fsSL -o eeip.tar.gz "https://github.com/rossmann-engineering/EEIP.NET/archive/${UPSTREAM_SHA}.tar.gz" \
    && echo "${UPSTREAM_SHA256}  eeip.tar.gz" | sha256sum -c - \
    && tar xzf eeip.tar.gz --strip-components=1 \
    && rm eeip.tar.gz
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
