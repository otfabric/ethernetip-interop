# ethernetip-interop adapter for go-ethernet-ip (client role only: an
# explicit-messaging and Logix tag originator; the library has no target).
#
# Build from the repository root:
#   docker build -f adapters/lokieip/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-lokieip:dev .
#
# go-ethernet-ip (github.com/loki-os/go-ethernet-ip) is WTFPL (its LICENSE is
# copied to /licenses/go-ethernet-ip/LICENSE) and has no dependencies. It has
# no tags: the default branch head is pinned as a Go pseudo-version by
# go.mod and verified by the hash in go.sum (the Go checksum database's),
# mirrored in versions.yaml with the commit.
#
# The upstream repository also carries a copy of a Rockwell manual (a PDF),
# which the module download brings into the build stage. The image copies
# only the compiled binary and the LICENSE from that stage.

ARG BASE_REGISTRY=docker.io/library
ARG GOLANG_DIGEST=sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4
ARG DEBIAN_DIGEST=sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

FROM ${BASE_REGISTRY}/golang:1.27.3-trixie@${GOLANG_DIGEST} AS build
ARG UPSTREAM_SHA=535f79752f63a4122c37c7f0f9b95ece01a8f7b4
ARG UPSTREAM_VERSION=0.0.0-20221229063210-535f797
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local GOWORK=off
WORKDIR /src/adapter
COPY adapters/lokieip/go.mod adapters/lokieip/go.sum ./
# The release build downloads what go.sum pins and verifies it. A candidate
# build ("make candidate-lokieip") overrides UPSTREAM_SHA (and UPSTREAM_VERSION):
# go get moves go.mod and go.sum to that commit (checked against the Go
# checksum database, as every module download is), which it says on stderr.
# Either way the module's pseudo-version must be
# v<UPSTREAM_VERSION without its short hash>-<12 hex of UPSTREAM_SHA>.
RUN pinned="$(sed -nE 's/^require github.com\/loki-os\/go-ethernet-ip v.*-([0-9a-f]{12})$/\1/p' go.mod)" \
    && test -n "${pinned}" \
    && if [ "$(printf '%s' "${UPSTREAM_SHA}" | cut -c1-12)" != "${pinned}" ]; then \
         echo "candidate build: go-ethernet-ip ${UPSTREAM_SHA} instead of the pin ${pinned}, go.sum updated by go get" >&2 \
         && GOFLAGS= go get "github.com/loki-os/go-ethernet-ip@${UPSTREAM_SHA}"; \
       fi \
    && go mod download && go mod verify \
    && want="v${UPSTREAM_VERSION%-*}-$(printf '%s' "${UPSTREAM_SHA}" | cut -c1-12)" \
    && got="$(go list -m -f '{{.Version}}' github.com/loki-os/go-ethernet-ip)" \
    && echo "go-ethernet-ip ${got}" && test "${got}" = "${want}" \
    && case "${UPSTREAM_SHA}" in "${UPSTREAM_VERSION##*-}"*) ;; *) exit 1 ;; esac
COPY adapters/lokieip/*.go ./
RUN go vet ./... \
    && go build -trimpath -ldflags='-s -w' -o /out/ethernetip-interop . \
    && mkdir -p /out/licenses/go-ethernet-ip \
    && cp "$(go list -m -f '{{.Dir}}' github.com/loki-os/go-ethernet-ip)/LICENSE" /out/licenses/go-ethernet-ip/LICENSE

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST}
ARG UPSTREAM_SHA=535f79752f63a4122c37c7f0f9b95ece01a8f7b4
ARG UPSTREAM_VERSION=0.0.0-20221229063210-535f797
ARG ADAPTER_VERSION=dev
ENV ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
COPY --from=build /out/ethernetip-interop /usr/local/bin/ethernetip-interop
COPY --from=build /out/licenses/ /licenses/
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop
USER 10001
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["print-capabilities"]
LABEL org.opencontainers.image.title="ethernetip-interop-lokieip" \
      org.opencontainers.image.description="EtherNet/IP and Logix tag originator built on go-ethernet-ip (loki-os)" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT AND WTFPL" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
