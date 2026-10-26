# ethernetip-interop adapter for etherip (client role only: the EtherNet/IP
# explicit-messaging client of EPICS, for ControlLogix/CompactLogix tags and
# generic CIP reads).
#
# Build from the repository root:
#   docker build -f adapters/etherip/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-etherip:dev .
#
# etherip is EPL-1.0 (copied to /licenses/etherip/). It is built with Maven
# from the source archive of the pinned commit, verified by its SHA-256; it
# has no runtime dependencies. The adapter (adapters/etherip/src, MIT) is
# compiled against the built jar and runs on a Temurin JRE.

ARG BASE_REGISTRY=docker.io/library
ARG MAVEN_DIGEST=sha256:6fdc855a6ed81d288ca7ca37ac6ff5e9308b612485c0801d70b25a858c83d237
ARG JRE_DIGEST=sha256:20e7f7288e1c18eebe8f06a442c9f7183342d9b022d3b9a9677cae2b558ddddd

FROM ${BASE_REGISTRY}/maven:3.9.11-eclipse-temurin-21-noble@${MAVEN_DIGEST} AS build
ARG UPSTREAM_SHA=ce0688ae51be5652ec200fbe389667b36a638323
ARG UPSTREAM_SHA256=35ad2cbb710be5bef6cae0b2441a9397534249ddb33e9ac4e1e377b2e6546c7e
WORKDIR /src/etherip
# The pins of this file, to tell the release build from a candidate build.
COPY adapters/etherip/Dockerfile /tmp/pins
# The release build checks the archive of the pinned commit against its
# SHA-256. A candidate build ("make candidate-etherip") overrides UPSTREAM_SHA
# with another commit, whose archive has no recorded hash: it says so on
# stderr and leaves /tmp/candidate behind for the steps below.
RUN set -eu; \
    pinned="$(sed -nE 's/^ARG UPSTREAM_SHA=(.+)$/\1/p' /tmp/pins | head -1)"; \
    test -n "${pinned}"; \
    if [ "${UPSTREAM_SHA}" != "${pinned}" ]; then \
        printf '%s' "${UPSTREAM_SHA}" | grep -qE '^[0-9a-f]{40}$' || { echo "UPSTREAM_SHA must be a full commit hash" >&2; exit 1; }; \
        echo "candidate build: etherip ${UPSTREAM_SHA} instead of the pin ${pinned}, no archive hash to check" >&2; \
        touch /tmp/candidate; \
    fi; \
    curl -fsSL -o etherip.tar.gz "https://github.com/ornl-epics/etherip/archive/${UPSTREAM_SHA}.tar.gz"; \
    if [ ! -e /tmp/candidate ]; then echo "${UPSTREAM_SHA256}  etherip.tar.gz" | sha256sum -c -; fi; \
    tar xzf etherip.tar.gz --strip-components=1; \
    rm etherip.tar.gz
# The tests need a PLC: skipped (and not compiled, so JUnit is not fetched).
# The plugins are Maven 3.9.11's default bindings plus the versions the pom pins.
RUN mvn -B -q -Dmaven.test.skip=true package \
    && if [ -e /tmp/candidate ]; then cp target/etherip-*.jar /src/etherip.jar; \
       else cp target/etherip-1.0.0.jar /src/etherip.jar; fi
COPY adapters/etherip/src /src/adapter/src
RUN mkdir -p /src/adapter/classes \
    && javac --release 21 -Xlint:-options -cp /src/etherip.jar -d /src/adapter/classes \
        /src/adapter/src/interop/*.java \
    && jar --create --file /src/adapter.jar -C /src/adapter/classes .

FROM ${BASE_REGISTRY}/eclipse-temurin:21.0.8_9-jre-noble@${JRE_DIGEST}
ARG UPSTREAM_SHA=ce0688ae51be5652ec200fbe389667b36a638323
ARG UPSTREAM_VERSION=1.0-67
ARG ADAPTER_VERSION=dev
ENV ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
COPY --from=build /src/etherip.jar /opt/etherip/etherip.jar
COPY --from=build /src/adapter.jar /opt/adapter/adapter.jar
COPY --from=build /src/etherip/LICENSE /licenses/etherip/LICENSE
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
# A short-lived client: no JIT tiers beyond C1, the serial collector.
RUN printf '#!/bin/sh\nexec java -XX:+UseSerialGC -XX:TieredStopAtLevel=1 -Xss512k -cp /opt/adapter/adapter.jar:/opt/etherip/etherip.jar interop.EtheripInterop "$@"\n' \
        > /usr/local/bin/ethernetip-interop \
    && chmod 755 /usr/local/bin/ethernetip-interop \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop
USER 10001
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["print-capabilities"]
LABEL org.opencontainers.image.title="ethernetip-interop-etherip" \
      org.opencontainers.image.description="EtherNet/IP and Logix reference client built on etherip (EPICS)" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="EPL-1.0 AND MIT" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
