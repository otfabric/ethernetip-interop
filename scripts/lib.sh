#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Shared helpers for the self-test scripts. Source, do not execute.
#
# Servers and clients run as containers on a private Docker network and
# reach each other by address, without NAT, as Class 1 I/O needs: a target
# sends T->O datagrams to the originator's own address at UDP 2222. No host
# port is published, so parallel runs do not collide.

REGISTRY="${REGISTRY:-ghcr.io/otfabric}"
VERSION="${VERSION:-dev}"
RUN_ID="${RUN_ID:-$$}"
NETWORK="ethernetip-interop-${RUN_ID}"
READY_FILE="/run/ethernetip-interop/ready"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-60}"
TOOLS_REGISTRY="${TOOLS_REGISTRY:-docker.io}"

PASS=0
FAIL=0
NA=0

log()  { echo "[$(basename "$0" .sh)] $*" >&2; }
ok()   { PASS=$((PASS + 1)); [[ -n "${VERBOSE:-}" ]] && log "ok: $*"; return 0; }
fail() { FAIL=$((FAIL + 1)); log "FAIL: $*"; return 0; }

image_of() { echo "${REGISTRY}/ethernetip-interop-$1:${VERSION}"; }

require_tools() {
    local t
    for t in docker jq; do
        command -v "$t" >/dev/null || { log "missing required tool: $t"; exit 2; }
    done
}

network_up() { docker network create --label "ethernetip-interop.run=${RUN_ID}" "${NETWORK}" >/dev/null; }

cleanup() {
    docker ps -aq --filter "label=ethernetip-interop.run=${RUN_ID}" | xargs docker rm -f >/dev/null 2>&1
    docker network rm "${NETWORK}" >/dev/null 2>&1
    return 0
}

# start_server <adapter> [server args...] -> prints the container name.
start_server() {
    local adapter="$1"; shift
    local name="srv-${adapter}-${SERVER_TAG:-one}-${RUN_ID}"
    docker rm -f "${name}" >/dev/null 2>&1
    docker run -d --name "${name}" --network "${NETWORK}" --label "ethernetip-interop.run=${RUN_ID}" \
        "$(image_of "${adapter}")" server "$@" >/dev/null
    echo "${name}"
}

# wait_ready <container>: the ready file exists only once the target accepts
# connections. No fixed delays.
wait_ready() {
    local name="$1" waited=0 state
    while true; do
        state=$(docker inspect --format '{{.State.Status}}' "${name}" 2>/dev/null || echo absent)
        if [[ "${state}" != "running" ]]; then
            log "server ${name} is ${state}"; docker logs --tail 20 "${name}" >&2 2>/dev/null; return 1
        fi
        docker exec "${name}" test -f "${READY_FILE}" 2>/dev/null && return 0
        if (( waited >= WAIT_TIMEOUT * 5 )); then
            log "server ${name} not ready after ${WAIT_TIMEOUT}s"; docker logs --tail 20 "${name}" >&2; return 1
        fi
        sleep 0.2; waited=$((waited + 1))
    done
}

# ip_of <container>: its address on the run's network. EIPScanner does not
# resolve names, and the identity's socket address is compared with it.
ip_of() { docker inspect --format "{{(index .NetworkSettings.Networks \"${NETWORK}\").IPAddress}}" "$1"; }

stop_server() { docker rm -f "$1" >/dev/null 2>&1; return 0; }

# run_client <adapter> <operation> <args...>: prints the result document (the
# last line of stdout) and returns the exit code of the adapter. Capture both
# with
#     doc=$(run_client ...); CLIENT_EXIT=$?
run_client() {
    local adapter="$1"; shift
    local out code
    out=$(docker run --rm --network "${NETWORK}" --label "ethernetip-interop.run=${RUN_ID}" \
        "$(image_of "${adapter}")" client "$@" 2>/dev/null)
    code=$?
    printf '%s\n' "${out}" | tail -n 1
    return "${code}"
}

# capabilities <adapter>: the capability document, one line.
capabilities() { docker run --rm "$(image_of "$1")" print-capabilities 2>/dev/null; }

# validate_documents <dir>: schemas/ against the documents in dir, with the
# local jsonschema package or, without one, in a Python container.
validate_documents() {
    local dir="$1" root
    root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    if python3 -c 'import jsonschema' 2>/dev/null; then
        python3 "${root}/scripts/validate-documents.py" "${dir}"
    else
        local abs
        abs="$(cd "${dir}" && pwd)"
        "${root}/scripts/retry.sh" docker run --rm -v "${root}:/work:ro" -v "${abs}:/docs:ro" -w /work \
            "${TOOLS_REGISTRY}/library/python:3.12-slim" \
            sh -c 'pip install -q --root-user-action=ignore --disable-pip-version-check jsonschema && python scripts/validate-documents.py /docs'
    fi
}

finish() {
    echo "" >&2
    local extra=""
    [[ "${NA}" -gt 0 ]] && extra=", ${NA} not applicable"
    log "${PASS} passed, ${FAIL} failed${extra}"
    [[ "${FAIL}" -eq 0 ]]
}
