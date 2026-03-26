#!/usr/bin/env bash
set -euo pipefail

# Cross-compile the agentic CLI for all supported platforms.
# Outputs go to cli/releases/<version>/
#
# Prerequisites:
#   brew install zig
#   cargo install cargo-zigbuild
#   rustup target add x86_64-unknown-linux-gnu \
#                      aarch64-unknown-linux-gnu \
#                      x86_64-pc-windows-gnu \
#                      x86_64-apple-darwin \
#                      aarch64-apple-darwin

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLI_DIR="$SCRIPT_DIR/../cli"
VERSION=$(grep '^version' "$CLI_DIR/Cargo.toml" | head -1 | sed 's/.*"\(.*\)".*/\1/')
OUT_DIR="$CLI_DIR/releases/$VERSION"
BIN_NAME="agentic"

# Targets: <rustc-triple>:<friendly-name>:<extension>:<use-zigbuild>
# macOS targets build natively; Linux/Windows use cargo-zigbuild
TARGETS=(
  "x86_64-apple-darwin:darwin-x86_64::no"
  "aarch64-apple-darwin:darwin-arm64::no"
  "x86_64-unknown-linux-gnu:linux-x86_64::yes"
  "aarch64-unknown-linux-gnu:linux-arm64::yes"
  "x86_64-pc-windows-gnu:windows-x86_64:.exe:yes"
)

echo "Building agentic v${VERSION} for all platforms"
echo "Output: ${OUT_DIR}"
echo ""

mkdir -p "$OUT_DIR"

for entry in "${TARGETS[@]}"; do
  IFS=":" read -r triple friendly ext zigbuild <<< "$entry"

  echo "==> ${friendly} (${triple})"

  # Ensure the target is installed
  rustup target add "$triple" 2>/dev/null || true

  if [ "$zigbuild" = "yes" ]; then
    (cd "$CLI_DIR" && cargo zigbuild --release --target "$triple") 2>&1 | sed 's/^/    /'
  else
    (cd "$CLI_DIR" && cargo build --release --target "$triple") 2>&1 | sed 's/^/    /'
  fi

  src="$CLI_DIR/target/${triple}/release/${BIN_NAME}${ext}"
  dst="${OUT_DIR}/${BIN_NAME}-${friendly}${ext}"

  if [ -f "$src" ]; then
    cp "$src" "$dst"
    echo "    -> $(basename "$dst")"
  else
    echo "    WARNING: binary not found at $src — skipping"
  fi
  echo ""
done

# Generate checksums
echo "==> Generating checksums"
(cd "$OUT_DIR" && shasum -a 256 agentic-* > checksums-sha256.txt)
cat "$OUT_DIR/checksums-sha256.txt" | sed 's/^/    /'

echo ""
echo "Done! Binaries are in ${OUT_DIR}/"
