#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Container-contract smoke test of one adapter image.
#
# Usage: smoke.sh <adapter>          e.g. smoke.sh opener
#
# Checks the parts of docs/CONTAINER_CONTRACT.md that one image can show:
# version, the capability document (against its schema), the baked
# fixtures, usage errors and exit codes; for a server, readiness, the ready
# and stopped events, an answer to ListIdentity from another adapter's client
# and a clean stop on SIGTERM; for a client, one operation against another
# adapter's server and a result document for a host that does not exist.
#
# Environment: REGISTRY, VERSION (image tag, default dev), VERBOSE=1,
#              REPORT_DIR (default reports; documents go to
#              REPORT_DIR/smoke/<adapter>/), SMOKE_CLIENT (the client that
#              asks a server: default cpppo, pycomm3 for cpppo itself),
#              SMOKE_SERVER (the server a client asks: default opener).

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=scripts/lib.sh
source "${SCRIPT_DIR}/lib.sh"

ADAPTER="${1:?usage: smoke.sh <adapter>}"
IMAGE="$(image_of "${ADAPTER}")"
require_tools
DOCS="${REPORT_DIR:-${ROOT}/reports}/smoke/${ADAPTER}"
rm -rf "${DOCS}" && mkdir -p "${DOCS}"
BAD=$(mktemp -d)
trap 'rm -rf "${BAD}"; cleanup' EXIT
network_up
log "${IMAGE}"

expect_exit() {  # expect_exit <code> <label> <docker run args...>
    local want="$1" label="$2"; shift 2
    docker run --rm --network "${NETWORK}" --label "ethernetip-interop.run=${RUN_ID}" "$@" >/dev/null 2>&1
    local got=$?
    if [[ "${got}" == "${want}" ]]; then ok "${label}: exit ${got}"; else fail "${label}: exit ${got}, want ${want}"; fi
}

# ---- version ----
version=$(docker run --rm "${IMAGE}" version 2>/dev/null)
if jq -e --arg a "${ADAPTER}" '.adapter == $a and .contract == "1.0" and (.adapterVersion | type == "string")
        and (.upstream | type == "string") and (.upstreamVersion | length > 0)
        and (.upstreamRevision | test("^[0-9a-f]{40}$"))' <<<"${version}" >/dev/null 2>&1
then ok "version"; else fail "version printed: ${version}"; fi

# ---- capabilities ----
caps=$(capabilities "${ADAPTER}")
printf '%s\n' "${caps}" > "${DOCS}/capabilities-${ADAPTER}.json"
if [[ $(wc -l <<<"${caps}") -eq 1 ]] && jq -e --arg a "${ADAPTER}" '.adapter == $a' <<<"${caps}" >/dev/null 2>&1
then ok "print-capabilities: one line, adapter ${ADAPTER}"; else fail "print-capabilities: ${caps}"; fi
serves=false; clients=false
jq -e '.roles.server' <<<"${caps}" >/dev/null 2>&1 && serves=true
jq -e '.roles.client' <<<"${caps}" >/dev/null 2>&1 && clients=true

# ---- the baked fixtures are the repository's ----
for f in "${ROOT}"/fixtures/*/fixture.json; do
    name=$(basename "$(dirname "${f}")")
    [[ "${name}" == schema ]] && continue
    if diff -q <(docker run --rm "${IMAGE}" print-fixture "${name}" 2>/dev/null) "${f}" >/dev/null
    then ok "print-fixture ${name}"; else fail "print-fixture ${name} differs from fixtures/${name}/fixture.json"; fi
done
if docker run --rm "${IMAGE}" print-fixture 2>/dev/null | jq -e '.schemaVersion == "1.0"' >/dev/null 2>&1
then ok "print-fixture without a name"; else fail "print-fixture without a name prints no fixture"; fi
expect_exit 2 "print-fixture of an unknown name" "${IMAGE}" print-fixture no-such-fixture
expect_exit 2 "print-fixture of a path" "${IMAGE}" print-fixture ../fixtures/logix

# ---- usage errors never touch the network ----
expect_exit 2 "unknown command" "${IMAGE}" frobnicate
if [[ "${clients}" == true ]]; then
    op=$(jq -r '.clientOperations[0]' <<<"${caps}")
    expect_exit 2 "client with an unknown operation" "${IMAGE}" client frobnicate --host nowhere
    expect_exit 2 "client without --host" "${IMAGE}" client "${op}"
    expect_exit 2 "client with an unknown flag" "${IMAGE}" client "${op}" --host nowhere --frobnicate 1
    expect_exit 2 "client with a bad --transport" "${IMAGE}" client list-identity --host nowhere --transport carrier-pigeon
    for o in list-identity discover list-services get-attribute set-attribute get-attributes-all multiple-get \
             identity io read-tag read-tags write-tag tag-list; do
        if ! jq -e --arg o "${o}" '.clientOperations | index($o)' <<<"${caps}" >/dev/null 2>&1; then
            expect_exit 2 "client ${o} (not declared)" "${IMAGE}" client "${o}" --host nowhere
        fi
    done
else
    expect_exit 2 "client of an adapter without the client role" "${IMAGE}" client list-identity --host nowhere
fi

if [[ "${serves}" == true ]]; then
    fixture=$(jq -r '.fixtures[0]' <<<"${caps}")
    expect_exit 2 "server with an unknown flag" "${IMAGE}" server --frobnicate 1
    expect_exit 4 "server with a missing fixture" "${IMAGE}" server --fixture /no/such/fixture.json
    # A fixture the adapter does not serve: one it does not list.
    other=$(cd "${ROOT}/fixtures" && for d in */; do d=${d%/}; [[ "${d}" == schema ]] && continue
        jq -e --arg d "${d}" '.fixtures | index($d)' <<<"${caps}" >/dev/null || { echo "${d}"; break; }; done)
    if [[ -n "${other}" ]]; then
        expect_exit 4 "server with a fixture it does not serve (${other})" "${IMAGE}" server --fixture "/fixtures/${other}/fixture.json"
    fi
    chmod 755 "${BAD}"
    printf '{"schemaVersion": "1.0", "name": "%s"' "${fixture}" > "${BAD}/fixture.json"   # not JSON
    chmod 644 "${BAD}/fixture.json"
    expect_exit 4 "server with an invalid fixture" -v "${BAD}:/bad:ro" "${IMAGE}" server --fixture /bad/fixture.json

    # ---- readiness, ready event, an answer to another adapter, clean stop ----
    name=$(start_server "${ADAPTER}")
    if wait_ready "${name}"; then ok "server becomes ready"; else fail "server did not become ready"; finish; exit 1; fi
    first=$(docker logs "${name}" 2>/dev/null | head -1)
    if jq -e --arg a "${ADAPTER}" --arg f "${fixture}" '.event == "ready" and .adapter == $a and .fixture == $f
            and (.address | test(":44818$"))' <<<"${first}" >/dev/null 2>&1
    then ok "ready event"; else fail "first stdout line is not the ready event: ${first}"; fi
    client="${SMOKE_CLIENT:-cpppo}"
    [[ "${client}" == "${ADAPTER}" ]] && client=pycomm3
    ip=$(ip_of "${name}")
    if jq -e '.serverFeatures.listIdentityTcp' <<<"${caps}" >/dev/null 2>&1; then
        doc=$(run_client "${client}" list-identity --host "${ip}"); code=$?
        printf '%s\n' "${doc}" > "${DOCS}/list-identity-from-${client}.json"
        want=$(jq -c '.identity | {productName, productCode, serialNumber, revision}' "${ROOT}/fixtures/${fixture}/fixture.json")
        if [[ "${code}" == 0 ]] && jq -e --argjson w "${want}" '.ok and (.identity | {productName, productCode, serialNumber, revision}) == $w' \
                <<<"${doc}" >/dev/null 2>&1
        then ok "${client} list-identity answered with the fixture's identity"; else fail "${client} list-identity: exit ${code}, ${doc}"; fi
    elif jq -e '.serverFeatures.logixTags' <<<"${caps}" >/dev/null 2>&1; then
        # A target without ListIdentity (libplctag's ab_server): one of the
        # fixture's scalar tags instead, read by cpppo through slot 0.
        tag=$(jq -c '[.tags[] | select((.count // 1) == 1 and .type != "BOOL" and .values)][0]' "${ROOT}/fixtures/${fixture}/fixture.json")
        doc=$(run_client cpppo read-tag --host "${ip}" --tag "$(jq -r '.name' <<<"${tag}")" --type "$(jq -r '.type' <<<"${tag}")" --route 1/0); code=$?
        printf '%s\n' "${doc}" > "${DOCS}/read-tag-from-cpppo.json"
        if [[ "${code}" == 0 ]] && jq -e --argjson t "${tag}" '.ok and .values == $t.values' <<<"${doc}" >/dev/null 2>&1
        then ok "cpppo read-tag answered with the fixture's value"; else fail "cpppo read-tag: exit ${code}, ${doc}"; fi
    else
        fail "the server declares neither listIdentityTcp nor logixTags: nothing to probe it with"
    fi
    docker stop -t 10 "${name}" >/dev/null
    code=$(docker inspect --format '{{.State.ExitCode}}' "${name}")
    last=$(docker logs "${name}" 2>/dev/null | tail -1)
    if [[ "${code}" == 0 ]]; then ok "SIGTERM: exit 0"; else fail "SIGTERM: exit ${code}"; fi
    if jq -e --arg a "${ADAPTER}" '.event == "stopped" and .adapter == $a' <<<"${last}" >/dev/null 2>&1
    then ok "stopped event"; else fail "last stdout line is not the stopped event: ${last}"; fi
    if docker logs "${name}" 2>/dev/null | grep -v '^{' | grep -q .; then fail "server stdout has lines that are not JSON"
    else ok "server stdout is JSON Lines only"; fi
    stop_server "${name}"
else
    expect_exit 2 "server of an adapter without the server role" "${IMAGE}" server
fi

if [[ "${clients}" == true ]]; then
    # ---- one operation against another adapter's server ----
    server="${SMOKE_SERVER:-opener}"
    SERVER_TAG=client
    name=$(start_server "${server}")
    if wait_ready "${name}"; then
        ip=$(ip_of "${name}")
        fixture=$(capabilities "${server}" | jq -r '.fixtures[0]')
        doc=$(run_client "${ADAPTER}" get-attribute --host "${ip}" --class 1 --instance 1 --attribute 7); code=$?
        # A client whose unconnected path the server cannot take (libplctag's
        # raw requests always go through Unconnected_Send, which OpENer
        # lacks) is tried once more over Class 3, when it declares Class 3.
        if ! jq -e '.ok and .status == 0' <<<"${doc}" >/dev/null 2>&1 && jq -e '.clientFeatures.class3' <<<"${caps}" >/dev/null 2>&1; then
            doc=$(run_client "${ADAPTER}" get-attribute --host "${ip}" --class 1 --instance 1 --attribute 7 --connected); code=$?
        fi
        printf '%s\n' "${doc}" > "${DOCS}/get-attribute-${server}.json"
        want=$(jq -r '.identity.productName' "${ROOT}/fixtures/${fixture}/fixture.json")
        want=$(printf '%02x' "${#want}")$(printf '%s' "${want}" | od -An -tx1 | tr -d ' \n')
        if [[ "${code}" == 0 && $(wc -l <<<"${doc}") -eq 1 ]] && jq -e --arg w "${want}" --arg a "${ADAPTER}" '
                .schemaVersion == "1.0" and .adapter == $a and .operation == "get-attribute" and .ok and .error == null
                and .status == 0 and .data == $w' <<<"${doc}" >/dev/null 2>&1
        then ok "get-attribute @1/1/7 of ${server}"; else fail "get-attribute of ${server}: exit ${code}, ${doc}"; fi
    else
        fail "${server} did not become ready"
    fi
    stop_server "${name}"

    # ---- a target that does not exist: a result document and exit 1 ----
    doc=$(run_client "${ADAPTER}" get-attribute --host "no-such-host-${RUN_ID}" --class 1 --instance 1 --attribute 1 \
        --timeout-ms 1500); code=$?
    printf '%s\n' "${doc}" > "${DOCS}/get-attribute-no-such-host.json"
    if [[ "${code}" == 1 ]] && jq -e '.ok == false and (.error.code | type == "string")' <<<"${doc}" >/dev/null 2>&1
    then ok "unreachable target: exit 1 and a result document"; else fail "unreachable target: exit ${code}, ${doc}"; fi
fi

# ---- the documents against their schemas ----
if validate_documents "${DOCS}" >"${DOCS}/schema-check.txt" 2>&1; then ok "documents match schemas/"
else fail "documents do not match schemas/: $(cat "${DOCS}/schema-check.txt")"; fi

finish
