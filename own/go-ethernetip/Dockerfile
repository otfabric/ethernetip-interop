# SPDX-License-Identifier: MIT
#
# ethernetip-interop-otfabric: OT Fabric's own EtherNet/IP stack behind the container contract.
# Not an independent implementation: see README.md.
#
# Build from the repository root:
#   make -f own/go-ethernetip/Makefile image
#
# The build takes the go-ethernetip release as UPSTREAM_VERSION (the released git tag,
# which is the Go module version) and its commit as UPSTREAM_SHA. There is no pin
# in this repository: the workflow passes the latest release. A build never uses
# main. go.mod and go.sum here are only a starting point for local work.

ARG BASE_REGISTRY=docker.io/library
# golang:1.27.4-trixie and debian:trixie-slim, as versions.yaml base_images, multi-arch indexes
# (linux/amd64 and linux/arm64).
ARG GOLANG_DIGEST=sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9
ARG DEBIAN_DIGEST=sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2

FROM ${BASE_REGISTRY}/golang:1.27.4-trixie@${GOLANG_DIGEST} AS build

ARG UPSTREAM_VERSION
ARG UPSTREAM_SHA
ARG ADAPTER_VERSION=dev
ARG GOPROXY=https://proxy.golang.org

ENV CGO_ENABLED=0 GOPROXY=${GOPROXY} GOSUMDB=sum.golang.org GOTOOLCHAIN=local GOWORK=off
WORKDIR /src
COPY own/go-ethernetip/go.mod own/go-ethernetip/go.sum own/go-ethernetip/*.go ./

RUN set -eu; \
    test -n "${UPSTREAM_VERSION}" || { echo "UPSTREAM_VERSION is required" >&2; exit 1; }; \
    echo "${UPSTREAM_SHA}" | grep -Eq '^[0-9a-f]{40}$'; \
    go get "github.com/otfabric/go-ethernetip@${UPSTREAM_VERSION}"; \
    go mod tidy; \
    built="$(go list -m -f '{{.Version}}' github.com/otfabric/go-ethernetip)"; \
    test "${built}" = "${UPSTREAM_VERSION}" || { echo "go.mod has go-ethernetip ${built}, want ${UPSTREAM_VERSION}" >&2; exit 1; }; \
    go mod download && go mod verify; \
    origin="$(go mod download -json "github.com/otfabric/go-ethernetip@${UPSTREAM_VERSION}" \
        | sed -nE 's/^[[:space:]]*"Hash": "([0-9a-f]{40})".*/\1/p')"; \
    if [ -n "${origin}" ] && [ "${origin}" != "${UPSTREAM_SHA}" ]; then \
        echo "go-ethernetip ${UPSTREAM_VERSION} is commit ${origin}, not ${UPSTREAM_SHA}" >&2; exit 1; \
    fi

RUN set -eu; \
    test -z "$(gofmt -l .)"; \
    go vet ./...; \
    go test ./...; \
    go build -trimpath -ldflags "-s -w \
        -X main.adapterVersion=${ADAPTER_VERSION} \
        -X main.upstreamVersion=${UPSTREAM_VERSION} \
        -X main.upstreamRevision=${UPSTREAM_SHA}" \
        -o /out/ethernetip-interop .; \
    mkdir -p /out/licenses/go; \
    cp "$(go env GOROOT)/LICENSE" /out/licenses/go/LICENSE; \
    go version -m /out/ethernetip-interop | awk '$1 == "dep" { print $2, $3 }' > /out/licenses/BUNDLED-MODULES.txt; \
    test -s /out/licenses/BUNDLED-MODULES.txt; \
    while read -r module modversion; do \
        dir="$(go list -m -f '{{.Dir}}' "${module}")"; \
        mkdir -p "/out/licenses/${module}"; \
        for file in "${dir}"/LICENSE* "${dir}"/LICENCE* "${dir}"/COPYING* "${dir}"/NOTICE*; do \
            if [ -f "${file}" ]; then cp "${file}" "/out/licenses/${module}/"; fi; \
        done; \
        test -n "$(ls "/out/licenses/${module}")" || { echo "no licence file in ${module} ${modversion}" >&2; exit 1; }; \
    done < /out/licenses/BUNDLED-MODULES.txt

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST} AS runtime

ARG ADAPTER_VERSION=dev
ARG UPSTREAM_VERSION

RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/ethernetip-interop \
    && chown interop /run/ethernetip-interop

COPY --from=build /out/ethernetip-interop /usr/local/bin/ethernetip-interop
COPY --from=build /out/licenses /licenses
COPY LICENSE /licenses/ethernetip-interop/LICENSE
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY fixtures /fixtures

LABEL org.opencontainers.image.title="ethernetip-interop-otfabric" \
      org.opencontainers.image.description="OT Fabric go-ethernetip behind the ethernetip-interop container contract, for testing. Not an independent implementation." \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${UPSTREAM_VERSION}"

USER interop
EXPOSE 44818/tcp 44818/udp 2222/udp

HEALTHCHECK --interval=2s --timeout=2s --start-period=10s --retries=15 \
  CMD test -f /run/ethernetip-interop/ready

ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["server"]
