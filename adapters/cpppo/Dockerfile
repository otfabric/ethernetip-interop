# ethernetip-interop adapter for cpppo (server and client roles: a Logix-style
# EtherNet/IP simulator and originator; no Class 1 I/O).
#
# Build from the repository root:
#   docker build -f adapters/cpppo/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-cpppo:dev .
#
# cpppo is GPL-3.0 (LICENSE and COPYING copied to /licenses/cpppo/), and so is
# this adapter (GPL-3.0-or-later); the image is GPL-3.0 as a whole. It is
# installed from PyPI at the versions and hashes in requirements.txt.

ARG BASE_REGISTRY=docker.io/library
ARG PYTHON_DIGEST=sha256:a2b82f3c48559aa0a8446d9af49826b6e2b2016f4cd2afabfe6013ec53729170

FROM ${BASE_REGISTRY}/python:3.14.8-slim-trixie@${PYTHON_DIGEST}
ARG UPSTREAM_SHA=f7c224bbb2389bd26d0088eeb1a93db3a5c4858f
ARG UPSTREAM_VERSION=5.2.5
ARG ADAPTER_VERSION=dev
ENV PYTHONDONTWRITEBYTECODE=1 PIP_DISABLE_PIP_VERSION_CHECK=1 PIP_NO_CACHE_DIR=1 \
    ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
COPY adapters/cpppo/requirements.txt /opt/adapter/requirements.txt
RUN pip install --require-hashes --no-deps -r /opt/adapter/requirements.txt \
    && mkdir -p /licenses/cpppo /licenses/greenery \
    && python -c 'import importlib.metadata as m; d=m.distribution("cpppo"); [open("/licenses/cpppo/" + f.name, "w").write(f.read_text()) for f in d.files if f.name in ("LICENSE", "COPYING")]' \
    && test -s /licenses/cpppo/LICENSE && test -s /licenses/cpppo/COPYING \
    && printf '%s\n' "greenery 2.1 (https://github.com/qntm/greenery), by Sam Hughes (qntm), is under the MIT License." \
        "Its source distribution ships no licence file; the licence is stated in its package metadata (License: MIT License)." \
        > /licenses/greenery/NOTICE \
    && (python -m pip uninstall --yes --quiet pip setuptools wheel || true)
COPY adapters/cpppo/cpppo_interop.py /opt/adapter/cpppo_interop.py
COPY licenses/GPL-3.0.txt /licenses/GPL-3.0.txt
RUN python -I -c "import sys; sys.path.insert(0, '/opt/adapter'); import cpppo_interop" \
    && printf '#!/bin/sh\nexec python3 -I /opt/adapter/cpppo_interop.py "$@"\n' > /usr/local/bin/ethernetip-interop \
    && chmod 755 /usr/local/bin/ethernetip-interop \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/ethernetip-interop && chown interop /run/ethernetip-interop
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
USER 10001
EXPOSE 44818/tcp 44818/udp
HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD test -f /run/ethernetip-interop/ready
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["server"]
LABEL org.opencontainers.image.title="ethernetip-interop-cpppo" \
      org.opencontainers.image.description="EtherNet/IP and Logix reference simulator and originator built on cpppo" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="GPL-3.0-or-later" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
