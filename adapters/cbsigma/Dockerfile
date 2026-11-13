# ethernetip-interop adapter for cB-Sigma's enip (server and client roles: a
# Logix-style target with structures, tag browsing, Class 3 and Class 1 I/O,
# and an explicit-messaging, Logix and Class 1 originator).
#
# Build from the repository root:
#   docker build -f adapters/cbsigma/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-cbsigma:dev .
#
# enip (github.com/cB-Sigma/enip) is Apache-2.0, and so is its one
# dependency, github.com/cB-Sigma/lanbcast; both LICENSE and NOTICE files are
# copied to /licenses/<module>/. Both are Go modules pinned by go.mod and
# verified by the hashes in go.sum (the Go checksum database's), mirrored in
# versions.yaml with the commit of the enip tag. The server role is enip's
# server package, which is also the controller of cB-Sigma's enip-sim since
# its v0.2.0; enip-sim itself (a demo tag table and a command) is not linked.

ARG BASE_REGISTRY=docker.io/library
ARG GOLANG_DIGEST=sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9
ARG DEBIAN_DIGEST=sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2

FROM ${BASE_REGISTRY}/golang:1.27.4-trixie@${GOLANG_DIGEST} AS build
ARG UPSTREAM_VERSION=0.2.1
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local GOWORK=off
WORKDIR /src/adapter
COPY adapters/cbsigma/go.mod adapters/cbsigma/go.sum ./
# The release build downloads what go.sum pins and verifies it. A candidate
# build ("make candidate-cbsigma") overrides UPSTREAM_VERSION: go get moves
# go.mod and go.sum to that version (checked against the Go checksum
# database, as every module download is), which it says on stderr.
RUN pinned="$(sed -nE 's/^require github.com\/cB-Sigma\/enip v(.+)$/\1/p' go.mod)" \
    && test -n "${pinned}" \
    && if [ "${UPSTREAM_VERSION}" != "${pinned}" ]; then \
         echo "candidate build: enip ${UPSTREAM_VERSION} instead of the pin ${pinned}, go.sum updated by go get" >&2 \
         && GOFLAGS= go get "github.com/cB-Sigma/enip@v${UPSTREAM_VERSION}"; \
       fi \
    && go mod download && go mod verify \
    && test "$(go list -m -f '{{.Version}}' github.com/cB-Sigma/enip)" = "v${UPSTREAM_VERSION}"
COPY adapters/cbsigma/*.go ./
RUN test -z "$(gofmt -l .)" \
    && go vet ./... \
    && go build -trimpath -ldflags='-s -w' -o /out/ethernetip-interop . \
    && for m in enip lanbcast; do \
         dir="$(go list -m -f '{{.Dir}}' "github.com/cB-Sigma/$m")" \
         && mkdir -p "/out/licenses/$m" && cp "$dir/LICENSE" "$dir/NOTICE" "/out/licenses/$m/"; \
       done

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST}
ARG UPSTREAM_SHA=49d9fb246a82c8d3cd2d54e31a93b9b12ed642ef
ARG UPSTREAM_VERSION=0.2.1
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
EXPOSE 44818/tcp 44818/udp 2222/udp
HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD test -f /run/ethernetip-interop/ready
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["server"]
LABEL org.opencontainers.image.title="ethernetip-interop-cbsigma" \
      org.opencontainers.image.description="EtherNet/IP and Logix target and originator built on cB-Sigma's enip" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT AND Apache-2.0" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
