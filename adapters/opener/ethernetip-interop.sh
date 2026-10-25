#!/bin/bash
# SPDX-License-Identifier: MIT
#
# Container contract of the OpENer adapter (docs/CONTAINER_CONTRACT.md):
# server, print-capabilities, print-fixture, version. OpENer has no
# originator role, so there is no client command.
set -euo pipefail

ready_file=/run/ethernetip-interop/ready

usage() {
	echo "usage: ethernetip-interop server [--interface IF] [--ready-file PATH] | print-capabilities | print-fixture [NAME] | version" >&2
	exit 2
}

event() { printf '%s\n' "$1"; }

server() {
	local iface=eth0
	while [ $# -gt 0 ]; do
		case "$1" in
		--interface) iface=${2:?}; shift 2 ;;
		--ready-file) ready_file=${2:?}; shift 2 ;;
		--fixture)
			# OpENer serves its sample application only.
			[ "${2:-}" = /fixtures/opener-sample/fixture.json ] || { echo "the opener adapter serves only the opener-sample fixture" >&2; exit 4; }
			shift 2 ;;
		*) usage ;;
		esac
	done
	local ip
	ip=$(ip -4 -o addr show dev "$iface" | awk '{print $4}' | cut -d/ -f1 | head -n1)
	[ -n "$ip" ] || { echo "interface $iface has no IPv4 address" >&2; exit 1; }
	rm -f "$ready_file"
	/usr/local/bin/OpENer "$iface" 1>&2 &
	local pid=$!
	stop() {
		rm -f "$ready_file"
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
			touch "$ready_file"
			event "{\"event\":\"ready\",\"adapter\":\"opener\",\"address\":\"$ip:44818\",\"fixture\":\"opener-sample\"}"
			wait "$pid"
			echo "OpENer exited" >&2
			exit 1
		fi
		sleep 0.1
	done
	echo "OpENer did not accept connections within 30 s" >&2
	kill -TERM "$pid" 2>/dev/null || true
	exit 1
}

case "${1:-}" in
server) shift; server "$@" ;;
print-capabilities) cat /opt/adapter/capabilities.json ;;
print-fixture)
	name=${2:-opener-sample}
	[ -f "/fixtures/$name/fixture.json" ] || { echo "no fixture $name" >&2; exit 2; }
	cat "/fixtures/$name/fixture.json" ;;
version)
	printf '{"adapter":"opener","adapterVersion":"%s","upstream":"OpENer","upstreamRevision":"%s","contract":"1.0"}\n' \
		"${ETHERNETIP_INTEROP_ADAPTER_VERSION:-dev}" "${ETHERNETIP_INTEROP_UPSTREAM_REVISION:-}" ;;
client) echo "the opener adapter has no client role" >&2; exit 2 ;;
*) usage ;;
esac
