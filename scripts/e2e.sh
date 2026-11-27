#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="/usr/local/go/bin:$PATH"
go build -o startshipd ./cmd/startshipd
python3 scripts/recipe_apps_e2e.py
python3 scripts/e2e.py
