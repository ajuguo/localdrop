#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cleanup() {
  if [[ -n "${VITE_PID:-}" ]]; then
    kill "${VITE_PID}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT INT TERM

cd "${ROOT_DIR}/web"
npm install
npm run dev -- --host 127.0.0.1 &
VITE_PID=$!

cd "${ROOT_DIR}"
LOCALDROP_WEB_DEV_URL="http://127.0.0.1:5173" go run ./cmd/localdrop

