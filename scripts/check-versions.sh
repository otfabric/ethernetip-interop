#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Fails when versions.yaml disagrees with what the builds actually pin: the
# Dockerfiles, the static capability documents (adapters/*/capabilities.json)
# and the requirements.txt of the Python adapters.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"
status=0

need() {  # need <description> <value that must appear in versions.yaml>
    if [[ -n "$2" ]] && grep -qF -- "$2" versions.yaml; then
        echo "ok   $1: $2"
    else
        echo "FAIL $1: '$2' is not in versions.yaml"
        status=1
    fi
}

same() {  # same <description> <a> <b>
    if [[ -n "$2" && "$2" == "$3" ]]; then
        echo "ok   $1: $2"
    else
        echo "FAIL $1: '$2' and '$3' disagree"
        status=1
    fi
}

arg() { sed -nE "s/^ARG $2=(.*)$/\1/p" "$1" | head -1; }

# Every stage of a Dockerfile must declare an ARG with the same value.
consistent() {  # consistent <Dockerfile> <ARG>
    local n
    n=$(sed -nE "s/^ARG $2=(.*)$/\1/p" "$1" | sort -u | wc -l | tr -d ' ')
    if [[ "${n}" == 1 ]]; then echo "ok   $1: every $2 is the same"; else echo "FAIL $1: $2 differs between stages"; status=1; fi
}

# Every adapter of the Makefile (ADAPTERS): its upstream version and
# revision, the same revision in every build stage, and, where the
# Dockerfile has them, the SHA-256 of the source archive and the digests of
# the base images.
read -r -a adapters <<<"$(make -s -C "${ROOT}" print-adapters)"
for a in "${adapters[@]}"; do
    f="adapters/${a}/Dockerfile"
    [[ -f "${f}" ]] || { echo "FAIL ${a}: no ${f}"; status=1; continue; }
    need "${a} version"  "version: \"$(arg "${f}" UPSTREAM_VERSION)\""
    need "${a} revision" "revision: \"$(arg "${f}" UPSTREAM_SHA)\""
    consistent "${f}" UPSTREAM_SHA
    if [[ -n "$(arg "${f}" UPSTREAM_SHA256)" ]]; then
        need "${a} source archive" "source_sha256: \"$(arg "${f}" UPSTREAM_SHA256)\""
    fi
    while read -r d; do
        need "${a} base image digest" "${d}"
    done < <(sed -nE 's/^ARG [A-Z_]+_DIGEST=(sha256:[0-9a-f]+)$/\1/p' "${f}" | sort -u)
done

# A static capability document (OpENer, libplctag) must name the same pin
# as its Dockerfile.
for a in "${adapters[@]}"; do
    caps="adapters/${a}/capabilities.json"
    [[ -f "${caps}" ]] || continue
    same "${a} capabilities version"  "$(sed -nE 's/^ *"version": "([^"]+)".*/\1/p' "${caps}" | head -1)" "$(arg "adapters/${a}/Dockerfile" UPSTREAM_VERSION)"
    same "${a} capabilities revision" "$(sed -nE 's/^ *"revision": "([^"]+)".*/\1/p' "${caps}" | head -1)" "$(arg "adapters/${a}/Dockerfile" UPSTREAM_SHA)"
done

# Python adapters: the Dockerfile and requirements.txt name the same version,
# and versions.yaml records the hash pip verifies.
req_version() { sed -nE "s/^$2==([^ ]+).*$/\1/p" "$1"; }
req_hash() { sed -nE "s/^$2==[^ ]+ --hash=sha256:([0-9a-f]+).*$/\1/p" "$1"; }
for a in "${adapters[@]}"; do
    r="adapters/${a}/requirements.txt"
    [[ -f "${r}" && -n "$(req_version "${r}" "${a}")" ]] || continue
    same "${a} in Dockerfile and requirements.txt" "$(arg "adapters/${a}/Dockerfile" UPSTREAM_VERSION)" "$(req_version "${r}" "${a}")"
    need "${a} wheel hash" "wheel_sha256: \"$(req_hash "${r}" "${a}")\""
done
need "greenery version" "greenery: \"$(req_version adapters/cpppo/requirements.txt greenery) sdist"
need "greenery hash"    "sha256 $(req_hash adapters/cpppo/requirements.txt greenery)"

exit "${status}"
