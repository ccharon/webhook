#!/usr/bin/env bash
set -euo pipefail

# Token must match test/config.json
TOKEN="this-is-a-local-dev-token-not-for-prod"
HOST="${WEBHOOK_HOST:-http://localhost:6080}"

ID="${1:-test-$(date +%s)}"
PARAM="${2:-v1.0}"

BODY="{\"id\":\"${ID}\",\"param\":\"${PARAM}\",\"unix_seconds\":$(date +%s)}"
SIG="sha256=$(printf '%s' "${BODY}" | openssl dgst -sha256 -hmac "${TOKEN}" | awk '{print $NF}')"

echo "POST ${HOST}"
echo "Body: ${BODY}"
echo "Sig:  ${SIG}"
echo "---"
curl -s -X POST "${HOST}" \
  -H "Content-Type: application/json" \
  -H "X-Hub-Signature-256: ${SIG}" \
  -d "${BODY}"
echo