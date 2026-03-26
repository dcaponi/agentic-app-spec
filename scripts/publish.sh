#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLI_DIR="$SCRIPT_DIR/../cli"

# ── Preflight checks ────────────────────────────────────────────────────────

if ! command -v npm &>/dev/null; then
  echo "Error: npm is not installed." >&2
  exit 1
fi

# Ensure the user is logged in to npm
if ! npm whoami &>/dev/null; then
  echo "Error: You are not logged in to npm. Run 'npm login' first." >&2
  exit 1
fi

# Ensure working tree is clean
if [ -n "$(git -C "$CLI_DIR" status --porcelain)" ]; then
  echo "Error: Working tree is dirty. Commit or stash changes before publishing." >&2
  exit 1
fi

# ── Build ────────────────────────────────────────────────────────────────────

echo "==> Installing dependencies…"
(cd "$CLI_DIR" && npm ci)

echo "==> Building…"
(cd "$CLI_DIR" && npm run build)

# ── Publish ──────────────────────────────────────────────────────────────────

# Pass through any extra args (e.g. --dry-run, --tag beta)
echo "==> Publishing to npmjs.org…"
(cd "$CLI_DIR" && npm publish --access public "$@")

echo "==> Done! Published $(node -p "require('./cli/package.json').name + '@' + require('./cli/package.json').version" --prefix "$SCRIPT_DIR/..")"
