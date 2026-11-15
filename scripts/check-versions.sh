#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Fails when versions.yaml disagrees with what the builds actually pin: the
# Dockerfiles (of the adapters and of OT Fabric's own image under own/), the
# static capability documents (adapters/*/capabilities.json), the
# requirements.txt of the Python adapters, the go.mod and go.sum of the Go
# adapters, and the tool images of the Makefiles and scripts/lib.sh.
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

# Go adapters: go.mod requires the version the Dockerfile builds (as a Go
# pseudo-version for an untagged revision), and go.sum's hash of that version
# is the module_sum of versions.yaml.
impl() {  # impl <name> <field>: the field of implementations.<name> in versions.yaml
    awk -v n="$1" -v f="$2" '
        /^[a-z_]+:/ { sect = $1 }
        sect == "implementations:" && /^  [a-z0-9_]+:/ { cur = $1; sub(/:$/, "", cur) }
        sect == "implementations:" && cur == n && $1 == f ":" { v = $2; gsub(/"/, "", v); print v; exit }
    ' versions.yaml
}
go_version() {  # go_version <UPSTREAM_VERSION> <UPSTREAM_SHA>: the version as go.mod writes it
    if [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[0-9]{14}-[0-9a-f]+$ ]]; then echo "v${1%-*}-${2:0:12}"; else echo "v$1"; fi
}
for a in "${adapters[@]}"; do
    gm="adapters/${a}/go.mod"
    [[ -f "${gm}" ]] || continue
    mod="$(impl "${a}" module)"
    have="$(awk -v m="${mod}" '($1 == "require" && $2 == m) { print $3; exit } ($1 == m) { print $2; exit }' "${gm}")"
    same "${a} in Dockerfile and go.mod" \
        "$(go_version "$(arg "adapters/${a}/Dockerfile" UPSTREAM_VERSION)" "$(arg "adapters/${a}/Dockerfile" UPSTREAM_SHA)")" "${have}"
    same "${a} module sum in go.sum and versions.yaml" \
        "$(awk -v m="${mod}" -v v="${have}" '$1 == m && $2 == v { print $3; exit }' "adapters/${a}/go.sum")" "$(impl "${a}" module_sum)"
done

# OT Fabric's own image is built from the same base images (base_images).
for f in own/*/Dockerfile; do
    [[ -f "${f}" ]] || continue
    while read -r d; do
        need "${f} base image digest" "${d}"
    done < <(sed -nE 's/^ARG [A-Z_]+_DIGEST=(sha256:[0-9a-f]+)$/\1/p' "${f}" | sort -u)
done

# Tooling: every *_IMAGE of the Makefiles is pinned by digest, and versions.yaml
# records it: a TOOLS_REGISTRY image under tooling, the golang image the Go
# modules are tested in under base_images. The default PYTHON_IMAGE of
# scripts/lib.sh and the govulncheck version of the Makefiles likewise.
tools='$(TOOLS_REGISTRY)/' base='$(BASE_REGISTRY)/'
for m in Makefile own/*/Makefile; do
    [[ -f "${m}" ]] || continue
    while read -r var ref; do
        case "${ref}" in
            "${tools}"*@sha256:*) need "${m} ${var}" "\"${ref#"${tools}"}\"" ;;
            "${base}"*@sha256:*)  need "${m} ${var}" "/${ref#"${base}"}\"" ;;
            *) echo "FAIL ${m} ${var}: '${ref}' is not pinned by digest"; status=1 ;;
        esac
    done < <(sed -nE 's/^([A-Z0-9_]+_IMAGE)[[:space:]]*\?=[[:space:]]*(.*)$/\1 \2/p' "${m}")
    v="$(sed -nE 's/^GOVULNCHECK_VERSION[[:space:]]*\?=[[:space:]]*([^[:space:]]+).*$/\1/p' "${m}" | head -1)"
    if [[ -n "${v}" ]]; then need "${m} govulncheck" "govulncheck@${v}\""
    else echo "FAIL ${m}: no GOVULNCHECK_VERSION"; status=1; fi
done
v="$(sed -nE 's/^PYTHON_IMAGE="\$\{PYTHON_IMAGE:-\$\{TOOLS_REGISTRY\}\/([^}]+)\}"$/\1/p' scripts/lib.sh)"
case "${v}" in
    *@sha256:*) need "scripts/lib.sh PYTHON_IMAGE" "\"${v}\"" ;;
    *) echo "FAIL scripts/lib.sh PYTHON_IMAGE: '${v}' is not pinned by digest"; status=1 ;;
esac

exit "${status}"
