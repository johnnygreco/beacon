#!/usr/bin/env bash
# Usage:
#   Latest:     curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
#   Pinned:     curl -sSfL https://johnnygreco.dev/beacon/install.sh | VERSION=0.1.0 sh
#   Uninstall:  curl -sSfL https://johnnygreco.dev/beacon/install.sh | UNINSTALL=1 sh
set -euo pipefail

REPO="johnnygreco/beacon"
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"

# Uninstall
if [ "${UNINSTALL:-}" = "1" ]; then
    if [ ! -f "${INSTALL_DIR}/beacon" ]; then
        echo "beacon not found in ${INSTALL_DIR}"
        exit 1
    fi
    if [ -w "${INSTALL_DIR}/beacon" ]; then
        rm "${INSTALL_DIR}/beacon"
    else
        echo "Removing ${INSTALL_DIR}/beacon (requires sudo)..."
        sudo rm "${INSTALL_DIR}/beacon"
    fi
    echo "beacon uninstalled from ${INSTALL_DIR}"
    exit 0
fi

# Detect OS
OS="$(uname -s)"
case "$OS" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *)      echo "Error: unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)             echo "Error: unsupported architecture: $ARCH"; exit 1 ;;
esac

# Use provided version or fetch latest
if [ -z "${VERSION:-}" ]; then
    VERSION="$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)"
    if [ -z "$VERSION" ]; then
        echo "Error: could not determine latest version."
        exit 1
    fi
else
    # Normalize: ensure leading 'v'
    VERSION="v${VERSION#v}"
fi

ARCHIVE="beacon_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

echo "Installing beacon ${VERSION} (${OS}/${ARCH})..."

# Download and extract
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

curl -sSfL "$URL" -o "${tmp_dir}/${ARCHIVE}"
tar -xzf "${tmp_dir}/${ARCHIVE}" -C "$tmp_dir"

# Install binary
mkdir -p "$INSTALL_DIR"
mv "${tmp_dir}/beacon" "${INSTALL_DIR}/beacon"

echo "beacon ${VERSION} installed to ${INSTALL_DIR}/beacon"

# Check if INSTALL_DIR is in PATH
case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *) echo "Add ${INSTALL_DIR} to your PATH: export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac
