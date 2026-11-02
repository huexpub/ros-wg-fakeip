#!/bin/sh
set -eu

WG_CONFIG=${WG_CONFIG:-/etc/wireguard/wg0.conf}
if [ -f "$WG_CONFIG" ]; then
    wg-quick up "$WG_CONFIG"
    cleanup() {
        wg-quick down "$WG_CONFIG" || true
    }
    trap 'kill -TERM "$gateway_pid" 2>/dev/null || true; cleanup' INT TERM EXIT
fi

/usr/local/bin/gateway &
gateway_pid=$!
wait "$gateway_pid"
