#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "Building..."
go build -o webhook .

echo "Starting webhook server (test/config.json) — Ctrl+C to stop"
exec ./webhook -c test/config.json