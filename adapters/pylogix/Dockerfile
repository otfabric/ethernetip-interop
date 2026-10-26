# ethernetip-interop adapter for pylogix (client role only: generic CIP and
# Rockwell Logix originator).
#
# Build from the repository root:
#   docker build -f adapters/pylogix/Dockerfile -t ghcr.io/otfabric/ethernetip-interop-pylogix:dev .
#
# pylogix is Apache-2.0. It is installed from PyPI at the version and hash in
# requirements.txt, mirrored in versions.yaml.

ARG BASE_REGISTRY=docker.io/library
ARG PYTHON_DIGEST=sha256:a2b82f3c48559aa0a8446d9af49826b6e2b2016f4cd2afabfe6013ec53729170

FROM ${BASE_REGISTRY}/python:3.14.8-slim-trixie@${PYTHON_DIGEST}
ARG UPSTREAM_SHA=193b6c7b0c601619223ee53587abd821d3b344c1
ARG UPSTREAM_VERSION=1.1.6
ARG ADAPTER_VERSION=dev
ENV PYTHONDONTWRITEBYTECODE=1 PIP_DISABLE_PIP_VERSION_CHECK=1 PIP_NO_CACHE_DIR=1 \
    ETHERNETIP_INTEROP_ADAPTER_VERSION=${ADAPTER_VERSION} ETHERNETIP_INTEROP_UPSTREAM_REVISION=${UPSTREAM_SHA} \
    ETHERNETIP_INTEROP_UPSTREAM_VERSION=${UPSTREAM_VERSION}
COPY adapters/pylogix/requirements.txt /opt/adapter/requirements.txt
RUN pip install --require-hashes --only-binary=:all: -r /opt/adapter/requirements.txt \
    && mkdir -p /licenses/pylogix \
    && python -c 'import importlib.metadata as m; d=m.distribution("pylogix"); fs=[f for f in d.files if f.name.upper().startswith("LICENSE")]; open("/licenses/pylogix/LICENSE","w").write(fs[0].read_text()) if fs else None' \
    && test -s /licenses/pylogix/LICENSE \
    && (python -m pip uninstall --yes --quiet pip setuptools wheel || true)
COPY adapters/pylogix/pylogix_interop.py /opt/adapter/pylogix_interop.py
RUN python -I -c "import sys; sys.path.insert(0, '/opt/adapter'); import pylogix_interop" \
    && printf '#!/bin/sh\nexec python3 -I /opt/adapter/pylogix_interop.py "$@"\n' > /usr/local/bin/ethernetip-interop \
    && chmod 755 /usr/local/bin/ethernetip-interop \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY LICENSE /licenses/LICENSE
COPY fixtures /fixtures
USER 10001
ENTRYPOINT ["/usr/local/bin/ethernetip-interop"]
CMD ["print-capabilities"]
LABEL org.opencontainers.image.title="ethernetip-interop-pylogix" \
      org.opencontainers.image.description="EtherNet/IP and Logix reference originator built on pylogix" \
      org.opencontainers.image.source="https://github.com/otfabric/ethernetip-interop" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"
