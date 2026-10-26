# ethernetip-interop adapter for gologix (server and client roles: a Logix-style
# tag server with Class 1 I/O, and a Class 3 explicit-messaging and Logix
# originator).
#
# Build from the repository root:
#   docker build -f adapters/gologix/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-gologix:dev .
#
# gologix is MIT (license.MD copied to /licenses/gologix/LICENSE); its one
# dependency, github.com/npat-efault/crc16, is BSD-2-Clause (copied to
# /licenses/crc16/LICENSE). Both are Go modules pinned by go.mod and verified
# by the hashes in go.sum (the Go checksum database's), mirrored in
# versions.yaml with the commit of the gologix tag.

ARG BASE_REGISTRY=docker.io/library
ARG GOLANG_DIGEST=sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4
ARG DEBIAN_DIGEST=sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

FROM ${BASE_REGISTRY}/golang:1.27.3-trixie@${GOLANG_DIGEST} AS build
ARG UPSTREAM_VERSION=0.44.0-beta
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local GOWORK=off
WORKDIR /src/adapter
COPY adapters/gologix/go.mod adapters/gologix/go.sum ./
RUN go mod download && go mod verify \
    && test "$(go list -m -f '{{.Version}}' github.com/danomagnum/gologix)" = "v${UPSTREAM_VERSION}"
COPY adapters/gologix/*.go ./
RUN go vet ./... \
    && go build -trimpath -ldflags='-s -w' -o /out/ethernetip-interop . \
    && mkdir -p /out/licenses/gologix /out/licenses/crc16 \
    && cp "$(go list -m -f '{{.Dir}}' github.com/danomagnum/gologix)/license.MD" /out/licenses/gologix/LICENSE \
    && cp "$(go list -m -f '{{.Dir}}' github.com/npat-efault/crc16)/LICENSE.txt" /out/licenses/crc16/LICENSE

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST}
ARG UPSTREAM_SHA=647a0b87b756d42e37ac0f65c5762dc89036152c
ARG UPSTREAM_VERSION=0.44.0-beta
ARG ADAPTER_VERSION=dev
ENV ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
COPY --from=build /out/ethernetip-interop /usr/local/bin/ethernetip-interop
COPY --from=build /out/licenses/ /licenses/
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/ethernetip-interop && chown interop /run/ethernetip-interop
USER 10001
EXPOSE 44818/tcp 2222/udp
HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD test -f /run/ethernetip-interop/ready
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["server"]
LABEL org.opencontainers.image.title="ethernetip-interop-gologix" \
      org.opencontainers.image.description="EtherNet/IP and Logix tag server and originator built on gologix" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
