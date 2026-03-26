#!/usr/bin/env bash
set -euo pipefail

REPO="dominickcaponi/agentic-app-spec"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

# Detect platform
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin) OS_NAME="darwin" ;;
  Linux)  OS_NAME="linux" ;;
  MINGW*|MSYS*|CYGWIN*) OS_NAME="windows" ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

case "$ARCH" in
  arm64|aarch64) ARCH_NAME="arm64" ;;
  x86_64|amd64)  ARCH_NAME="x86_64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

EXT=""
if [ "$OS_NAME" = "windows" ]; then
  EXT=".exe"
fi

BINARY="agentic-${OS_NAME}-${ARCH_NAME}${EXT}"

# Resolve version
if [ "$VERSION" = "latest" ]; then
  DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"
else
  DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"
fi

echo "Installing agentic (${OS_NAME}/${ARCH_NAME})..."
echo "  from: ${DOWNLOAD_URL}"
echo "  to:   ${INSTALL_DIR}/agentic${EXT}"

curl -fSL "$DOWNLOAD_URL" -o "${INSTALL_DIR}/agentic${EXT}"
chmod +x "${INSTALL_DIR}/agentic${EXT}"

echo "Done! Run 'agentic --help' to get started."
