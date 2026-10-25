#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Fails when versions.yaml disagrees with what the builds actually pin: the
# Dockerfiles, adapters/opener/capabilities.json and the requirements.txt of
# the Python adapters.
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

for a in opener eipscanner cpppo pycomm3; do
    f="adapters/${a}/Dockerfile"
    need "${a} version"  "version: \"$(arg "${f}" UPSTREAM_VERSION)\""
    need "${a} revision" "revision: \"$(arg "${f}" UPSTREAM_SHA)\""
    consistent "${f}" UPSTREAM_SHA
done

# Source archives fetched from GitHub, verified by SHA-256 in the build.
for a in opener eipscanner; do
    f="adapters/${a}/Dockerfile"
    need "${a} source archive" "source_sha256: \"$(arg "${f}" UPSTREAM_SHA256)\""
    need "${a} debian digest"  "$(arg "${f}" DEBIAN_DIGEST)"
done

# OpENer's capability document is static: it must name the same pin.
caps=adapters/opener/capabilities.json
same "opener capabilities version"  "$(sed -nE 's/^ *"version": "([^"]+)".*/\1/p' "${caps}")" "$(arg adapters/opener/Dockerfile UPSTREAM_VERSION)"
same "opener capabilities revision" "$(sed -nE 's/^ *"revision": "([^"]+)".*/\1/p' "${caps}")" "$(arg adapters/opener/Dockerfile UPSTREAM_SHA)"

# Python adapters: the Dockerfile and requirements.txt name the same version,
# and versions.yaml records the hash pip verifies.
req_version() { sed -nE "s/^$2==([^ ]+).*$/\1/p" "$1"; }
req_hash() { sed -nE "s/^$2==[^ ]+ --hash=sha256:([0-9a-f]+).*$/\1/p" "$1"; }
for a in cpppo pycomm3; do
    f="adapters/${a}/Dockerfile"
    r="adapters/${a}/requirements.txt"
    same "${a} in Dockerfile and requirements.txt" "$(arg "${f}" UPSTREAM_VERSION)" "$(req_version "${r}" "${a}")"
    need "${a} wheel hash" "wheel_sha256: \"$(req_hash "${r}" "${a}")\""
    need "${a} python digest" "$(arg "${f}" PYTHON_DIGEST)"
done
need "greenery version" "greenery: \"$(req_version adapters/cpppo/requirements.txt greenery) sdist"
need "greenery hash"    "sha256 $(req_hash adapters/cpppo/requirements.txt greenery)"

exit "${status}"
