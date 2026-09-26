#!/usr/bin/env bash
# Copyright 2026-present R. Thomazella. All rights reserved.
# Use of this source code is governed by the BSD-3-Clause
# license that can be found in the LICENSE file and online
# at https://opensource.org/license/BSD-3-clause.

# Generate the relay's runtime credentials (leaf cert + role tokens) into
# deploy/relay/credentials, then print the next steps. Intended to run on the
# VPS before the first `docker compose up`.
#
#   ./init_credentials.sh
#
# The credentials themselves are excluded from git (see repo .gitignore:
# *.key, *.csr, *.srl, *.token), so this script must run wherever the relay is
# deployed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CREDS_DIR="$SCRIPT_DIR/credentials"

mkdir -p "$CREDS_DIR"
"$SCRIPT_DIR/../scripts/generate_certs.sh" "$CREDS_DIR"

echo
echo "Credentials ready in $CREDS_DIR. Next:"
echo "  docker compose -f compose.yml up -d --build"
