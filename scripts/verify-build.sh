#!/usr/bin/env sh
# Run from repo root: ./backend/scripts/verify-build.sh
# Or from backend/: ./scripts/verify-build.sh
set -e
cd "$(dirname "$0")/.."
echo "==> go build ./cmd/server"
go build -o /dev/null ./cmd/server
echo "OK"
if command -v docker >/dev/null 2>&1; then
  echo "==> docker build"
  docker build -t wealthflow-backend:verify .
  echo "OK (image: wealthflow-backend:verify)"
else
  echo "(skip docker: not installed — Fly builds remotely)"
fi
