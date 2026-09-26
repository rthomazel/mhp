#!/usr/bin/env bash
# Copyright 2026-present R. Thomazella. All rights reserved.
# Use of this source code is governed by the BSD-3-Clause
# license that can be found in the LICENSE file and online
# at https://opensource.org/license/BSD-3-clause.

# Reproducible generation of the relay's private-CA trust material for MHP.
#
# Mirrors the procedure documented in spc/tasks.md (task 2) and validated
# against openssl 3.0.x: an offline CA signing key, a self-signed CA cert,
# and a CA-signed relay leaf whose SAN matches the marker SNI.
#
#   ./scripts/generate_certs.sh [output-dir]
#
# Output defaults to ./credentials. The CA signing key is the only secret here
# and must stay offline; distribute relay-ca.crt to both clients.

set -euo pipefail

# Keys and tokens land 0600.
umask 077

OUT_DIR="${1:-./credentials}"
SAN_DNS="${MHP_SAN:-en.zalando.de}"
CA_DAYS="${MHP_CA_DAYS:-3650}"
LEAF_DAYS="${MHP_LEAF_DAYS:-825}"
CA_CN="${MHP_CA_CN:-MHP Relay Private CA}"

mkdir -p "$OUT_DIR"
cd "$OUT_DIR"

# 1. Offline CA signing key — keep it offline, never mount into the relay.
openssl genrsa -out ca.key 4096

# 2. Self-signed CA certificate (public; shipped to clients only).
openssl req -x509 -new -nodes -key ca.key -sha256 -days "$CA_DAYS" \
  -subj "/CN=$CA_CN" -out relay-ca.crt

# 3. Relay leaf key + CSR. CN is the marker SNI the clients present.
openssl genrsa -out relay.key 2048
openssl req -new -key relay.key -subj "/CN=$SAN_DNS" -out relay.csr

# 4. Sign the leaf: serverAuth EKU, no CA flag, SAN = marker SNI.
printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n' \
  "$SAN_DNS" > san.ext
openssl x509 -req -in relay.csr -CA relay-ca.crt -CAkey ca.key -CAcreateserial \
  -days "$LEAF_DAYS" -sha256 -extfile san.ext -out relay.crt

chmod 600 ca.key relay.key 2>/dev/null || true

# 5. High-entropy role tokens. Distinct per role, never reused. Land 0600
# under umask 077. The relay loads these; clients never see them.
openssl rand -hex 32 > exit.token
openssl rand -hex 32 > proxy.token

echo "Generated in $OUT_DIR:"
ls -1 ca.key relay-ca.crt relay-ca.srl relay.key relay.crt san.ext exit.token proxy.token 2>/dev/null || true
echo
echo "Relay runtime material: relay.crt relay.key exit.token proxy.token  (mount read-only)"
echo "Client trust material:  relay-ca.crt   (distribute to both clients; keep ca.key offline)"
