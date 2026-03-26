#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$SCRIPT_DIR/.."

USAGE="Usage: $0 <target> [extra-args...]

Targets:
  all           Publish typescript runtime + go tag
  typescript    TypeScript runtime (npm: agentic-engine)
  go            Go module (tagged release)

The CLI is distributed as prebuilt binaries — see scripts/build-release.sh.

Python and Ruby runtimes are installed directly from GitHub:
  pip install \"agentic-engine @ git+https://github.com/dominickcaponi/agentic-app-spec.git#subdirectory=runtime/python\"
  gem \"agentic_engine\", git: \"https://github.com/dominickcaponi/agentic-app-spec.git\", glob: \"runtime/ruby/*.gemspec\"

Extra args are passed through to npm publish
(e.g. --dry-run, --tag beta)."

if [ $# -lt 1 ]; then
  echo "$USAGE"
  exit 1
fi

TARGET="$1"; shift

# ── Shared helpers ───────────────────────────────────────────────────────────

ensure_clean_tree() {
  if [ -n "$(git -C "$ROOT_DIR" status --porcelain)" ]; then
    echo "Error: Working tree is dirty. Commit or stash changes before publishing." >&2
    exit 1
  fi
}

copy_license() {
  cp "$ROOT_DIR/LICENSE" "$1/LICENSE"
}

# ── TypeScript runtime (npm) ─────────────────────────────────────────────────

publish_typescript() {
  local dir="$ROOT_DIR/runtime/typescript"
  echo "==> Publishing TypeScript runtime (npm: agentic-engine)"

  command -v npm &>/dev/null || { echo "Error: npm not found" >&2; exit 1; }
  npm whoami &>/dev/null     || { echo "Error: not logged in to npm. Run 'npm login'." >&2; exit 1; }

  copy_license "$dir"
  (cd "$dir" && npm ci && npm run build && npm publish --access public "$@")
  echo "    Done: agentic-engine@$(node -p "require('$dir/package.json').version")"
}

# ── Go module (tag only — Go modules are fetched from the repo) ──────────────

publish_go() {
  echo "==> Publishing Go module"

  local version
  version="${1:-}"
  if [ -z "$version" ]; then
    echo "Error: Go requires a version argument. Usage: $0 go v0.1.0" >&2
    exit 1
  fi

  local tag="runtime/go/${version}"
  echo "    Tagging ${tag}"
  git -C "$ROOT_DIR" tag -a "$tag" -m "Release Go runtime ${version}"
  echo "    Tag created. Push with: git push origin ${tag}"
}

# ── Dispatch ─────────────────────────────────────────────────────────────────

ensure_clean_tree

case "$TARGET" in
  typescript) publish_typescript "$@" ;;
  go)         publish_go "$@" ;;
  all)
    publish_typescript "$@"
    echo ""
    echo "Note: Go module requires a manual version tag. Run:"
    echo "  $0 go v0.1.0"
    echo ""
    echo "CLI binaries are built with: scripts/build-release.sh"
    ;;
  *)
    echo "Unknown target: $TARGET"
    echo "$USAGE"
    exit 1
    ;;
esac
