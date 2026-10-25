# ethernetip-interop adapter for OpENer (server role only: an EtherNet/IP
# adapter running OpENer's POSIX sample application).
#
# Build from the repository root:
#   docker build -f adapters/opener/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-opener:dev .
#
# OpENer is under its adapted BSD licence (license.txt of the source, copied
# to /licenses/opener/). It is built here from the source archive of the
# pinned commit, verified by its SHA-256, mirrored in versions.yaml.

ARG BASE_REGISTRY=docker.io/library
ARG DEBIAN_DIGEST=sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST} AS build
ARG UPSTREAM_SHA=d8820ef41ce30dda06d213438ba81c81de33ffe2
ARG UPSTREAM_SHA256=6c34be6bfe294351a69544722079751f96cdc24ac1a43337dd3e22d843f5b95b
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential cmake libcap-dev ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN curl -fsSL -o opener.tar.gz "https://github.com/EIPStackGroup/OpENer/archive/${UPSTREAM_SHA}.tar.gz" \
    && echo "${UPSTREAM_SHA256}  opener.tar.gz" | sha256sum -c - \
    && tar xzf opener.tar.gz --strip-components=1 \
    && rm opener.tar.gz
WORKDIR /src/build
# The sample application, as OpENer's bin/posix/setup_posix.sh builds it.
RUN cmake -DCMAKE_C_COMPILER=gcc -DOpENer_PLATFORM=POSIX -DCMAKE_BUILD_TYPE=Release \
          -DBUILD_SHARED_LIBS=OFF ../source \
    && make -j"$(nproc)" \
    && install -m 755 src/ports/POSIX/OpENer /usr/local/bin/OpENer

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST}
ARG UPSTREAM_SHA=d8820ef41ce30dda06d213438ba81c81de33ffe2
ARG UPSTREAM_VERSION=2.3-588
ARG ADAPTER_VERSION=dev
ENV ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
RUN apt-get update \
    && apt-get install -y --no-install-recommends libcap2 iproute2 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /usr/local/bin/OpENer /usr/local/bin/OpENer
COPY --from=build /src/license.txt /licenses/opener/license.txt
COPY adapters/opener/ethernetip-interop.sh /usr/local/bin/ethernetip-interop
COPY adapters/opener/capabilities.json /opt/adapter/capabilities.json
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
RUN chmod 755 /usr/local/bin/ethernetip-interop \
    && useradd --uid 10001 --create-home --home-dir /var/lib/opener --shell /usr/sbin/nologin interop \
    && mkdir -p /run/ethernetip-interop \
    && chown interop /run/ethernetip-interop
USER 10001
WORKDIR /var/lib/opener
EXPOSE 44818/tcp 44818/udp 2222/udp
HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD test -f /run/ethernetip-interop/ready
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["server"]
LABEL org.opencontainers.image.title="ethernetip-interop-opener" \
      org.opencontainers.image.description="EtherNet/IP reference adapter built on OpENer" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT AND LicenseRef-OpENer" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
