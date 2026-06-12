#!/bin/sh
# Self-contained on-prem TLS. With KALITA_TLS=self-signed the node generates a
# self-signed certificate on first boot (persisted in $KALITA_TLS_DIR, a volume)
# and serves HTTPS itself — no reverse proxy, no public domain. For production,
# mount real certs into that directory (node.crt/node.key) and they are reused;
# or set KALITA_TLS= empty and run behind a TLS-terminating proxy with
# --insecure-http. Everything after the script is passed straight to `kalita`.
set -e

TLS_DIR="${KALITA_TLS_DIR:-/data/tls}"

if [ "${KALITA_TLS:-}" = "self-signed" ]; then
    if [ ! -f "$TLS_DIR/node.crt" ]; then
        mkdir -p "$TLS_DIR"
        CN="${KALITA_TLS_CN:-localhost}"
        openssl req -x509 -newkey rsa:2048 -nodes -days 825 \
            -keyout "$TLS_DIR/node.key" -out "$TLS_DIR/node.crt" \
            -subj "/CN=$CN" \
            -addext "subjectAltName=DNS:$CN,DNS:localhost,IP:127.0.0.1" >/dev/null 2>&1
        echo "kalita: generated self-signed TLS certificate at $TLS_DIR (CN=$CN)"
    fi
    set -- "$@" --tls-cert "$TLS_DIR/node.crt" --tls-key "$TLS_DIR/node.key"
fi

exec kalita "$@"
