#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Cross-stack self-test: every client image against every server image, on a
# private Docker network, for the operations both declare.
#
# Usage: selftest.sh [adapter ...]     default: the Makefile's ADAPTERS
#
# Each server serves its first fixture (print-capabilities .fixtures[0]); the
# expected values come from that fixture in this repository. A case runs
# when the client lists the operations it needs and both sides declare the
# features it needs; otherwise it is recorded as n/a with the reason. Every
# result document is kept under REPORT_DIR/selftest/<client>-<server>/, the
# outcome of every case in summary.tsv and the matrix in summary.md. The
# documents are then checked against schemas/.
#
# These results are about the reference stacks, never about a consumer.
#
# Environment: REGISTRY, VERSION (image tag, default dev), VERBOSE=1,
#              REPORT_DIR (default reports).

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=scripts/lib.sh
source "${SCRIPT_DIR}/lib.sh"

ADAPTERS=("$@")
[[ ${#ADAPTERS[@]} -gt 0 ]] || read -r -a ADAPTERS <<<"$(make -s -C "${ROOT}" print-adapters)"
require_tools
OUT="${REPORT_DIR:-${ROOT}/reports}/selftest"
rm -rf "${OUT}" && mkdir -p "${OUT}"
SUMMARY="${OUT}/summary.tsv"
printf 'client\tserver\tcase\tfeature\toutcome\tnote\n' > "${SUMMARY}"
trap cleanup EXIT
network_up

declare -A CAPS
SERVERS=() CLIENTS=()
for a in "${ADAPTERS[@]}"; do
    CAPS[$a]=$(capabilities "$a")
    [[ -n "${CAPS[$a]}" ]] || { log "no capability document from $(image_of "$a")"; exit 2; }
    jq -e '.roles.server' <<<"${CAPS[$a]}" >/dev/null && SERVERS+=("$a")
    jq -e '.roles.client' <<<"${CAPS[$a]}" >/dev/null && CLIENTS+=("$a")
done
log "servers: ${SERVERS[*]:-none}; clients: ${CLIENTS[*]:-none}"

# The cases: id | feature | client operations | client features | server features.
# "logix" in the server column means: logixTags, and tagList as well when
# the client declares tagList (such a client, pycomm3's LogixDriver, uploads
# the tag list before it reads or writes a tag).
CASES=(
    "list-identity-tcp|ListIdentity (TCP)|list-identity|listIdentityTcp|listIdentityTcp"
    "list-identity-udp|ListIdentity (UDP)|list-identity|listIdentityUdp|listIdentityUdp"
    "list-services|ListServices|list-services|listServices|listServices"
    "get-attribute|Get_Attribute_Single|get-attribute|getAttributeSingle|getAttributeSingle"
    "get-attributes-all|Get_Attributes_All (Identity)|get-attributes-all|getAttributesAll|getAttributesAll"
    "set-attribute|Set_Attribute_Single (assembly data)|set-attribute get-attribute|setAttributeSingle getAttributeSingle|setAttributeSingle getAttributeSingle"
    "error-status|Error status (unknown attribute)|get-attribute|getAttributeSingle|getAttributeSingle"
    "multiple-get|Multiple Service Packet|multiple-get|multipleService|multipleService getAttributeSingle"
    "class3|Class 3 connected explicit messaging|get-attribute|class3|class3 getAttributeSingle"
    "identity|identity operation|identity||identity"
    "io-exclusive-owner|Class 1 I/O, exclusive owner|io|class1|class1"
    "io-large|Class 1 I/O, Large_Forward_Open|io|class1 largeForwardOpen|class1 largeForwardOpen"
    "io-input-only|Class 1 I/O, input only|io|class1 inputOnly|class1 inputOnly"
    "io-refused|Forward_Open refusal (connection size)|io|class1|class1"
    "io-config-data|Class 1 I/O, configuration data|io get-attribute|class1 configData getAttributeSingle|class1 configData getAttributeSingle"
    "logix-read|Logix Read Tag (atomic types)|read-tags|logixTags|logix"
    "logix-fragmented|Logix Read Tag Fragmented|read-tag|readTagFragmented|logix readTagFragmented"
    "logix-write|Logix Write Tag|write-tag read-tag|logixTags|logix"
    "tag-list|Logix tag list|tag-list|tagList|tagList"
)

has_feature() { jq -e --arg f "$2" --arg k "$3Features" '.[$k][$f] == true' <<<"${CAPS[$1]}" >/dev/null 2>&1; }
has_op() { jq -e --arg o "$2" '.clientOperations | index($o)' <<<"${CAPS[$1]}" >/dev/null 2>&1; }

# why_not <client> <server> <ops> <client features> <server features>: the
# reason a case does not apply, or nothing.
why_not() {
    local c="$1" s="$2" x f
    for x in $3; do has_op "$c" "$x" || { echo "${c} has no ${x} operation"; return; }; done
    for f in $4; do has_feature "$c" "$f" client || { echo "${c} (client) lacks ${f}"; return; }; done
    for f in $5; do
        case "$f" in
        logix)
            has_feature "$s" logixTags server || { echo "${s} (server) lacks logixTags"; return; }
            if has_feature "$c" tagList client && ! has_feature "$s" tagList server; then
                echo "${c} uploads the tag list first; ${s} (server) lacks tagList"; return
            fi ;;
        identity)
            # pycomm3's identity is get_module_info: Unconnected_Send to slot 0;
            # EIPScanner's is Get_Attributes_All of the Identity object.
            # Either way the Identity object answers Get_Attributes_All.
            has_feature "$s" getAttributesAll server || { echo "${s} (server) lacks getAttributesAll"; return; }
            if has_feature "$c" unconnectedSend client; then
                has_feature "$s" unconnectedSend server || { echo "${c} routes it (Unconnected_Send); ${s} (server) lacks unconnectedSend"; return; }
            else
                has_feature "$s" getAttributesAll server || { echo "${s} (server) lacks getAttributesAll"; return; }
            fi ;;
        *) has_feature "$s" "$f" server || { echo "${s} (server) lacks ${f}"; return; } ;;
        esac
    done
}

# Known incompatibilities between reference stacks: "client<TAB>server<TAB>case<TAB>reason"
# lines of scripts/selftest-known.tsv. A listed case that fails is recorded
# "known" with the reason and does not fail the self-test; one that passes is
# recorded "pass" with a note to remove the entry.
KNOWN="${SCRIPT_DIR}/selftest-known.tsv"
known_reason() {
    [[ -f "${KNOWN}" ]] || return 0
    awk -F'\t' -v c="$1" -v s="$2" -v k="$3" '!/^#/ && $1 == c && $2 == s && $3 == k {print $4; exit}' "${KNOWN}"
}

# op <args...>: runs one client operation of CLIENT against HOST, keeps the
# document as <case>-<n>.json and sets DOC and CODE.
op() {
    N=$((N + 1))
    DOC=$(run_client "${CLIENT}" "$1" --host "${HOST}" "${@:2}"); CODE=$?
    printf '%s\n' "${DOC}" > "${DIR}/${CASE}-${N}.json"
    [[ -n "${VERBOSE:-}" ]] && log "${CLIENT} -> ${SERVER}: $* => exit ${CODE} ${DOC}"
    return 0
}

# expect <jq filter on DOC, with $fx the fixture>: the case fails unless it holds.
expect() {
    if [[ "${CODE}" != 0 ]] || ! jq -e --argjson fx "${FX}" "$1" <<<"${DOC}" >/dev/null 2>&1; then
        NOTE="exit ${CODE}: $(jq -c 'del(.log)' <<<"${DOC}" 2>/dev/null | cut -c1-300)"
        return 1
    fi
}

hex_le() { local v=$1 n=$2 i out=""; for ((i = 0; i < n; i++)); do out+=$(printf '%02x' $(((v >> (8 * i)) & 255))); done; echo "${out}"; }
hex_str() { printf '%s' "$1" | od -An -tx1 | tr -d ' \n'; }
repeat_hex() { local out="" i; for ((i = 0; i < $2; i++)); do out+="$1"; done; echo "${out}"; }

run_case() {
    local id="$1"
    local name name_hex vendor
    name=$(jq -r '.identity.productName' <<<"${FX}")
    name_hex=$(printf '%02x' "${#name}")$(hex_str "${name}")
    vendor=$(jq -r '.identity.vendorId' <<<"${FX}")
    local out_inst out_size in_inst in_size cfg_inst io_inst
    out_inst=$(jq -r '[.assemblies[] | select(.kind == "output")][0].instance // empty' <<<"${FX}")
    out_size=$(jq -r '[.assemblies[] | select(.kind == "output")][0].size // empty' <<<"${FX}")
    in_inst=$(jq -r '[.assemblies[] | select(.kind == "input")][0].instance // empty' <<<"${FX}")
    in_size=$(jq -r '[.assemblies[] | select(.kind == "input")][0].size // empty' <<<"${FX}")
    cfg_inst=$(jq -r '[.assemblies[] | select(.kind == "config")][0].instance // 0' <<<"${FX}")
    local cfg_size
    cfg_size=$(jq -r '[.assemblies[] | select(.kind == "config")][0].size // 0' <<<"${FX}")
    io_inst=$(jq -r '[.assemblies[] | select(.kind == "input-only")][0].instance // empty' <<<"${FX}")
    # Each client writes its own pattern, so that a value left by another
    # client cannot pass for its own.
    local pat
    pat=$(printf '%02x' $(( $(cksum <<<"${CLIENT}" | cut -d' ' -f1) % 200 + 20 )))
    case "${id}" in
    list-identity-tcp | list-identity-udp)
        op list-identity --transport "${id#list-identity-}"
        expect '.ok and (.identity | .productName == $fx.identity.productName and .productCode == $fx.identity.productCode
            and .serialNumber == $fx.identity.serialNumber and .revision == $fx.identity.revision
            and (.vendorId == null or .vendorId == $fx.identity.vendorId))' ;;
    list-services)
        op list-services
        expect '.ok and (.services | length == 1) and .services[0].name == "Communications"' ;;
    get-attribute)
        op get-attribute --class 1 --instance 1 --attribute 7
        expect ".ok and .status == 0 and .data == \"${name_hex}\"" ;;
    get-attributes-all)
        local rev major minor
        rev=$(jq -r '.identity.revision' <<<"${FX}"); major=${rev%.*}; minor=${rev#*.}
        local prefix serial
        prefix=$(hex_le "${vendor}" 2)$(hex_le "$(jq -r '.identity.deviceType' <<<"${FX}")" 2)$(hex_le "$(jq -r '.identity.productCode' <<<"${FX}")" 2)$(printf '%02x%02x' "${major}" "${minor}")
        serial=$(hex_le "$(jq -r '.identity.serialNumber' <<<"${FX}")" 4)
        op get-attributes-all --class 1 --instance 1
        expect ".ok and .status == 0 and (.data | startswith(\"${prefix}\")) and (.data | contains(\"${serial}${name_hex}\"))" ;;
    set-attribute)
        [[ -n "${out_inst}" ]] || { NOTE="the fixture has no output assembly"; return 2; }
        local data
        data=$(repeat_hex "${pat}" "${out_size}")
        op set-attribute --class 4 --instance "${out_inst}" --attribute 3 --data "${data}"
        expect '.ok and .status == 0' || return 1
        if jq -e '.echoOutputToInput' <<<"${FX}" >/dev/null; then
            op get-attribute --class 4 --instance "${in_inst}" --attribute 3      # echoed to the input
        else
            op get-attribute --class 4 --instance "${out_inst}" --attribute 3     # read back
        fi
        expect ".ok and .status == 0 and .data == \"${data}\"" ;;
    error-status)
        op get-attribute --class 1 --instance 1 --attribute 99
        expect '.ok and .status != 0' || return 1
        NOTE="status 0x$(printf '%02x' "$(jq -r '.status' <<<"${DOC}")")" ;;
    multiple-get)
        op multiple-get --path @1/1/1 --path @1/1/7
        expect ".ok and (.replies | length == 2) and .replies[0].status == 0 and .replies[0].data == \"$(hex_le "${vendor}" 2)\"
            and .replies[1].status == 0 and .replies[1].data == \"${name_hex}\"" ;;
    class3)
        op get-attribute --class 1 --instance 1 --attribute 7 --connected
        expect ".ok and .status == 0 and .data == \"${name_hex}\"" ;;
    identity)
        op identity
        # The name, and the product code where the document has one
        # (node-ethernet-ip's module has neither code nor vendor).
        expect '.ok and ((.identity // {}) as $i | (.module // {}) as $m
            | ($i.productName // $m.product_name // $m.name) == $fx.identity.productName
            and (($i.productCode // $m.product_code) | . == null or . == $fx.identity.productCode))' ;;
    io-exclusive-owner | io-large)
        [[ -n "${out_inst}" && -n "${in_inst}" ]] || { NOTE="the fixture has no output or input assembly"; return 2; }
        # Not the pattern set-attribute left in the assembly: the echo must
        # come from this connection's output.
        local data extra=() n=1
        [[ "${id}" == io-large ]] && extra=(--large) n=2
        data=$(repeat_hex "$(printf '%02x' $((16#${pat} + n)))" "${out_size}")
        op io --config "${cfg_inst}" --output "${out_inst}" --input "${in_inst}" --output-size "${out_size}" \
            --input-size "${in_size}" --rpi-ms 10 --duration-ms 1500 --output-data "${data}" "${extra[@]}"
        local echo_check="true"
        jq -e '.echoOutputToInput' <<<"${FX}" >/dev/null && echo_check=".lastInput == \"${data}\""
        expect ".ok and .forwardOpen.status == 0 and .sent >= 10 and .received >= 10 and (.timedOut | not)
            and .forwardClose.status == 0 and (.lastInput | length) == $((in_size * 2)) and ${echo_check}" ;;
    io-input-only)
        [[ -n "${io_inst}" && -n "${in_inst}" ]] || { NOTE="the fixture has no input-only connection point"; return 2; }
        op io --config "${cfg_inst}" --output "${io_inst}" --input "${in_inst}" --output-size 0 \
            --input-size "${in_size}" --rpi-ms 10 --duration-ms 1000
        expect '.ok and .forwardOpen.status == 0 and .received >= 10 and (.timedOut | not)' ;;
    io-refused)
        [[ -n "${out_inst}" && -n "${in_inst}" ]] || { NOTE="the fixture has no output or input assembly"; return 2; }
        op io --config "${cfg_inst}" --output "${out_inst}" --input "${in_inst}" --output-size $((out_size - 1)) \
            --input-size "${in_size}" --rpi-ms 10 --duration-ms 100
        expect '.ok and (.forwardOpen.status != 0 or .forwardOpen.refused == true)' || return 1
        NOTE="forwardOpen $(jq -c '.forwardOpen' <<<"${DOC}")" ;;
    io-config-data)
        [[ "${cfg_inst}" != 0 && -n "${out_inst}" && -n "${in_inst}" ]] || { NOTE="the fixture has no configuration, output or input assembly"; return 2; }
        local cfg
        cfg=$(repeat_hex "$(printf '%02x' $((16#${pat} + 3)))" "${cfg_size}")
        op io --config "${cfg_inst}" --output "${out_inst}" --input "${in_inst}" --output-size "${out_size}" \
            --input-size "${in_size}" --rpi-ms 10 --duration-ms 500 --config-data "${cfg}"
        expect '.ok and .forwardOpen.status == 0 and .received >= 5 and .forwardClose.status == 0' || return 1
        op get-attribute --class 4 --instance "${cfg_inst}" --attribute 3
        expect ".ok and .status == 0 and .data == \"${cfg}\"" ;;
    logix-read)
        local tags want
        tags=$(jq -r '[.tags[] | select((.count // 1) == 1 and .type != "BOOL") | .name] | join(",")' <<<"${FX}")
        want=$(jq -c '[.tags[] | select((.count // 1) == 1 and .type != "BOOL") | .values]' <<<"${FX}")
        op read-tags --tags "${tags}"
        expect "[.results[] | .values] == ${want} and ([.results[] | (.status // 0) == 0 and .errorText == null] | all)" ;;
    logix-fragmented)
        local tag typ count want
        tag=$(jq -c '[.tags[] | select(.pattern)] | max_by((.count // 1))' <<<"${FX}")
        [[ "${tag}" != null ]] || { NOTE="the fixture has no array tag"; return 2; }
        typ=$(jq -r '.type' <<<"${tag}"); count=$(jq -r '.count' <<<"${tag}")
        want=$(jq -c '[range(.count) as $i | $i * .pattern.multiplier + .pattern.offset]' <<<"${tag}")
        op read-tag --tag "$(jq -r '.name' <<<"${tag}")" --count "${count}" --type "${typ}"
        expect ".status == 0 and .values == ${want}" ;;
    logix-write)
        local tag typ want
        tag=$(jq -c '[.tags[] | select(.pattern and (.type | IN("SINT", "INT", "DINT")))] | min_by(.count)' <<<"${FX}")
        [[ "${tag}" != null ]] || { NOTE="the fixture has no integer array tag"; return 2; }
        typ=$(jq -r '.type' <<<"${tag}")
        want=$(jq -c '[5, 6, 7, (3 * .pattern.multiplier + .pattern.offset)]' <<<"${tag}")
        op write-tag --tag "$(jq -r '.name' <<<"${tag}")" --type "${typ}" --values 5,6,7
        expect '.ok and .status == 0' || return 1
        op read-tag --tag "$(jq -r '.name' <<<"${tag}")" --count 4 --type "${typ}"
        expect ".status == 0 and .values == ${want}" ;;
    tag-list)
        op tag-list
        expect ".ok and (.tags | length) == (\$fx.tags | length)" ;;
    *) NOTE="unknown case ${id}"; return 1 ;;
    esac
}

for SERVER in "${SERVERS[@]}"; do
    fixture=$(jq -r '.fixtures[0]' <<<"${CAPS[${SERVER}]}")
    FX=$(jq -c . "${ROOT}/fixtures/${fixture}/fixture.json")
    name=$(start_server "${SERVER}")
    if ! wait_ready "${name}"; then
        fail "${SERVER} did not become ready"
        continue
    fi
    HOST=$(ip_of "${name}")
    log "server ${SERVER} (${fixture}) at ${HOST}"
    for CLIENT in "${CLIENTS[@]}"; do
        DIR="${OUT}/${CLIENT}-${SERVER}"
        mkdir -p "${DIR}"
        for c in "${CASES[@]}"; do
            IFS='|' read -r CASE feature ops cfeat sfeat <<<"${c}"
            N=0 NOTE=""
            reason=$(why_not "${CLIENT}" "${SERVER}" "${ops}" "${cfeat}" "${sfeat}")
            if [[ -n "${reason}" ]]; then
                NA=$((NA + 1))
                printf '%s\t%s\t%s\t%s\tn/a\t%s\n' "${CLIENT}" "${SERVER}" "${CASE}" "${feature}" "${reason}" >> "${SUMMARY}"
                continue
            fi
            run_case "${CASE}"
            rc=$?
            # A server the case brought down is restarted, so that the next
            # clients are not blamed for it; the case fails with the crash.
            if [[ "$(docker inspect --format '{{.State.Running}}' "${name}" 2>/dev/null)" != true ]]; then
                NOTE="the server stopped during the case (exit $(docker inspect --format '{{.State.ExitCode}}' "${name}" 2>/dev/null || echo '?')); ${NOTE}"
                rc=1
                stop_server "${name}"
                name=$(start_server "${SERVER}")
                wait_ready "${name}" || { fail "${SERVER} did not become ready again"; break 2; }
                HOST=$(ip_of "${name}")
            fi
            known=$(known_reason "${CLIENT}" "${SERVER}" "${CASE}")
            case ${rc} in
            0) ok "${CLIENT} -> ${SERVER}: ${CASE}"; outcome=pass
               [[ -n "${known}" ]] && { NOTE="listed in selftest-known.tsv but passed: remove the entry; ${NOTE}"; log "${CLIENT} -> ${SERVER}: ${CASE} passed although listed as known"; } ;;
            2) NA=$((NA + 1)); outcome=n/a ;;
            *) if [[ -n "${known}" ]]; then
                   KNOWN_N=$((KNOWN_N + 1)); outcome=known; NOTE="${known} (${NOTE})"
               else
                   fail "${CLIENT} -> ${SERVER}: ${CASE}: ${NOTE}"; outcome=fail
               fi ;;
            esac
            printf '%s\t%s\t%s\t%s\t%s\t%s\n' "${CLIENT}" "${SERVER}" "${CASE}" "${feature}" "${outcome}" "${NOTE}" >> "${SUMMARY}"
        done
    done
    stop_server "${name}"
done

# ---- the matrix: per server, a row per case, a column per client ----
{
    echo "# Cross-stack self-test"
    echo
    echo "Images: $(for a in "${ADAPTERS[@]}"; do printf '%s ' "$(image_of "$a")"; done)"
    echo
    echo "pass: the case passed; fail: it failed; known: a known incompatibility between the two reference stacks (scripts/selftest-known.tsv, reason in the notes); n/a: one side does not declare what the case needs."
    for s in "${SERVERS[@]}"; do
        echo
        echo "## Server ${s}"
        echo
        printf '| Case |'; for c in "${CLIENTS[@]}"; do printf ' %s |' "$c"; done; echo
        printf '|------|'; for c in "${CLIENTS[@]}"; do printf ':-:|'; done; echo
        for c in "${CASES[@]}"; do
            IFS='|' read -r id feature _ <<<"${c}"
            printf '| %s |' "${feature}"
            for cl in "${CLIENTS[@]}"; do
                printf ' %s |' "$(awk -F'\t' -v c="${cl}" -v s="${s}" -v i="${id}" '$1 == c && $2 == s && $3 == i {print $5}' "${SUMMARY}")"
            done
            echo
        done
    done
    echo
    echo "Notes (n/a reasons, known incompatibilities and observed values):"
    echo
    awk -F'\t' 'NR > 1 && $6 != "" {printf "- %s → %s, %s: %s (%s)\n", $1, $2, $3, $6, $5}' "${SUMMARY}"
} > "${OUT}/summary.md"
log "summary: ${OUT}/summary.md"

if validate_documents "${OUT}" >"${OUT}/schema-check.txt" 2>&1; then ok "documents match schemas/"
else fail "documents do not match schemas/: $(cat "${OUT}/schema-check.txt")"; fi

finish
