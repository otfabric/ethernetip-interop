# ethernetip-interop adapter for node-ethernet-ip, the npm package
# ethernet-ip (client role only: generic CIP and Rockwell Logix originator).
#
# Build from the repository root:
#   docker build -f adapters/nodeeip/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-nodeeip:dev .
#
# node-ethernet-ip is MIT. It is installed from npm with "npm ci" at the
# version and integrity hash in package-lock.json, mirrored in versions.yaml.
# It has no runtime dependencies.

ARG BASE_REGISTRY=docker.io/library
ARG NODE_DIGEST=sha256:43b9ba1142b8dcf33fa8f922ca1be85714adc444e0586c7cf7726daaeabc1bea

FROM ${BASE_REGISTRY}/node:24.22.1-trixie-slim@${NODE_DIGEST}
ARG UPSTREAM_SHA=4743b077abd3cc8826d511acde8f890372826e16
ARG UPSTREAM_VERSION=2.0.0
ARG ADAPTER_VERSION=dev
ENV NODE_ENV=production NPM_CONFIG_UPDATE_NOTIFIER=false NPM_CONFIG_FUND=false NPM_CONFIG_AUDIT=false \
    ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
WORKDIR /opt/adapter
COPY adapters/nodeeip/package.json adapters/nodeeip/package-lock.json /opt/adapter/
RUN npm ci --omit=dev --ignore-scripts --no-audit --no-fund \
    && test "$(node -p 'require("ethernet-ip/package.json").version')" = "${UPSTREAM_VERSION}" \
    && for d in node_modules/*/; do \
         n=$(basename "$d"); f=$(ls "$d" | grep -i '^licen[cs]e' | head -1); \
         test -n "$f" && mkdir -p "/licenses/$n" && cp "$d$f" "/licenses/$n/LICENSE" || exit 1; \
       done \
    && mkdir -p /licenses/node-ethernet-ip && mv /licenses/ethernet-ip/LICENSE /licenses/node-ethernet-ip/LICENSE \
    && rmdir /licenses/ethernet-ip && test -s /licenses/node-ethernet-ip/LICENSE \
    && rm -rf /root/.npm /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/corepack \
              /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack /opt/yarn-* /usr/local/bin/yarn /usr/local/bin/yarnpkg
COPY adapters/nodeeip/nodeeip_interop.js /opt/adapter/nodeeip_interop.js
RUN node /opt/adapter/nodeeip_interop.js print-capabilities > /dev/null \
    && printf '#!/bin/sh\nexec node /opt/adapter/nodeeip_interop.js "$@"\n' > /usr/local/bin/ethernetip-interop \
    && chmod 755 /usr/local/bin/ethernetip-interop \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
USER 10001
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["print-capabilities"]
LABEL org.opencontainers.image.title="ethernetip-interop-nodeeip" \
      org.opencontainers.image.description="EtherNet/IP and Logix reference originator built on node-ethernet-ip" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
