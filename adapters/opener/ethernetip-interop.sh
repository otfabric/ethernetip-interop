#!/bin/bash
# SPDX-License-Identifier: MIT
#
# Container contract of the OpENer adapter (docs/CONTAINER_CONTRACT.md):
# server, print-capabilities, print-fixture, version. OpENer has no
# originator role, so "client" is a usage error (exit 2).
set -euo pipefail

ready_file=/run/ethernetip-interop/ready

usage() {
	echo "usage: ethernetip-interop server [--fixture PATH] [--port 44818] [--interface IF] [--ready-file PATH]" >&2
	echo "       ethernetip-interop print-capabilities | print-fixture [NAME] | version" >&2
	exit 2
}

event() { printf '%s\n' "$1"; }

server() {
	local iface=eth0
	while [ $# -gt 0 ]; do
		[ $# -ge 2 ] || usage
		case "$1" in
		--interface) iface=$2 ;;
		--ready-file) ready_file=$2 ;;
		--port)
			# OpENer's sample application listens on 44818 only.
			[ "$2" = 44818 ] || { echo "the opener adapter listens on port 44818 only" >&2; exit 2; } ;;
		--fixture)
			# OpENer serves its sample application only: its identity and
			# assemblies are compiled in.
			[ -f "$2" ] || { echo "no fixture $2" >&2; exit 4; }
			case "$2" in
			*/opener-sample/fixture.json) ;;
			*) echo "the opener adapter serves only the opener-sample fixture" >&2; exit 4 ;;
			esac ;;
		*) usage ;;
		esac
		shift 2
	done
	local ip
	ip=$(ip -4 -o addr show dev "$iface" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -n1)
	[ -n "$ip" ] || { echo "interface $iface has no IPv4 address" >&2; exit 1; }
	[ -z "$ready_file" ] || rm -f "$ready_file"
	/usr/local/bin/OpENer "$iface" 1>&2 &
	local pid=$!
	stop() {
		[ -z "$ready_file" ] || rm -f "$ready_file"
		kill -TERM "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
		event '{"event":"stopped","adapter":"opener"}'
		exit 0
	}
	trap stop TERM INT
	# Ready when OpENer accepts TCP on its address: it binds the interface's
	# address, not the loopback.
	for _ in $(seq 1 300); do
		if ! kill -0 "$pid" 2>/dev/null; then
			echo "OpENer exited" >&2
			exit 1
		fi
		if (exec 3<>"/dev/tcp/$ip/44818") 2>/dev/null; then
			[ -z "$ready_file" ] || touch "$ready_file"
			event "{\"event\":\"ready\",\"adapter\":\"opener\",\"address\":\"$ip:44818\",\"fixture\":\"opener-sample\"}"
			wait "$pid" || true
			echo "OpENer exited" >&2
			exit 1
		fi
		sleep 0.1
	done
	echo "OpENer did not accept connections within 30 s" >&2
	kill -TERM "$pid" 2>/dev/null || true
	exit 1
}

print_fixture() {
	local name=${1:-opener-sample}
	case "$name" in
	"" | */* | *\\* | *..*) echo "invalid fixture name $name" >&2; exit 2 ;;
	esac
	[ -f "/fixtures/$name/fixture.json" ] || { echo "no fixture $name" >&2; exit 2; }
	cat "/fixtures/$name/fixture.json"
}

case "${1:-}" in
server) shift; server "$@" ;;
print-capabilities)
	sed "s/@ADAPTER_VERSION@/${ETHERNETIP_INTEROP_ADAPTER_VERSION:-dev}/" /opt/adapter/capabilities.json | tr -d '\n'
	echo ;;
print-fixture) print_fixture "${2:-}" ;;
version)
	printf '{"adapter":"opener","adapterVersion":"%s","upstream":"OpENer","upstreamVersion":"%s","upstreamRevision":"%s","contract":"1.0"}\n' \
		"${ETHERNETIP_INTEROP_ADAPTER_VERSION:-dev}" "${ETHERNETIP_INTEROP_UPSTREAM_VERSION:-}" \
		"${ETHERNETIP_INTEROP_UPSTREAM_REVISION:-}" ;;
client) echo "the opener adapter has no client role" >&2; exit 2 ;;
*) usage ;;
esac
