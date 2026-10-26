# ethernetip-interop adapter for libplctag (client and server roles: a Logix
# tag and generic CIP originator built on libplctag, and libplctag's PLC
# simulator ab_server serving a fixture's tags).
#
# Build from the repository root:
#   docker build -f adapters/libplctag/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-libplctag:dev .
#
# libplctag is dual-licensed MPL-2.0 OR LGPL-2.0-or-later (LICENSE-1.MPL and
# LICENSE-2.LGPL copied to /licenses/libplctag/); this image uses it under
# MPL-2.0. It is built, unmodified, from the source archive of the pinned
# release commit, verified by its SHA-256: the static library the adapter
# links, and ab_server (src/tools/ab_server).

ARG BASE_REGISTRY=docker.io/library
ARG DEBIAN_DIGEST=sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST} AS build
ARG UPSTREAM_SHA=7712ed7f1ee05ba0ebfca641b3f5c43da52a66ed
ARG UPSTREAM_SHA256=ed1f7869fa266df1bc266b9d135cd09d53dc392e557edb9986b2e172c84eb752
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential cmake ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src/libplctag
RUN curl -fsSL -o libplctag.tar.gz "https://github.com/libplctag/libplctag/archive/${UPSTREAM_SHA}.tar.gz" \
    && echo "${UPSTREAM_SHA256}  libplctag.tar.gz" | sha256sum -c - \
    && tar xzf libplctag.tar.gz --strip-components=1 \
    && rm libplctag.tar.gz
WORKDIR /src/build
RUN cmake -DCMAKE_BUILD_TYPE=Release -DBUILD_EXAMPLES=0 -DBUILD_TESTS=0 /src/libplctag \
    && make -j"$(nproc)" plctag_static ab_server \
    && install -m 755 bin_dist/ab_server /usr/local/bin/ab_server
COPY adapters/libplctag/src /src/adapter
RUN gcc -std=gnu11 -O2 -Wall -Wextra -Werror -I/src/build/bin_dist -o /usr/local/bin/ethernetip-interop \
        /src/adapter/main.c /src/adapter/json.c /src/build/bin_dist/libplctag_static.a -lpthread -lm

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST}
ARG UPSTREAM_SHA=7712ed7f1ee05ba0ebfca641b3f5c43da52a66ed
ARG UPSTREAM_VERSION=2.7.2
ARG ADAPTER_VERSION=dev
ENV ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
COPY --from=build /usr/local/bin/ethernetip-interop /usr/local/bin/ethernetip-interop
COPY --from=build /usr/local/bin/ab_server /usr/local/bin/ab_server
COPY --from=build /src/libplctag/LICENSE-1.MPL /licenses/libplctag/LICENSE-1.MPL
COPY --from=build /src/libplctag/LICENSE-2.LGPL /licenses/libplctag/LICENSE-2.LGPL
COPY --from=build /src/libplctag/LICENSE-1.MPL /licenses/libplctag/LICENSE
COPY adapters/libplctag/capabilities.json /opt/adapter/capabilities.json
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/ethernetip-interop && chown interop /run/ethernetip-interop
USER 10001
EXPOSE 44818/tcp
HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD test -f /run/ethernetip-interop/ready
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["server"]
LABEL org.opencontainers.image.title="ethernetip-interop-libplctag" \
      org.opencontainers.image.description="EtherNet/IP Logix originator and tag simulator built on libplctag" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT AND (MPL-2.0 OR LGPL-2.0-or-later)" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
