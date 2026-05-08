#!/usr/bin/env bash
# Usage:
#   Latest stable:     curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
#   Latest prerelease: curl -sSfL https://johnnygreco.dev/beacon/install.sh | INCLUDE_PRERELEASE=1 sh
#   Pinned:            curl -sSfL https://johnnygreco.dev/beacon/install.sh | VERSION=0.1.0 sh
#   No DB dep:         curl -sSfL https://johnnygreco.dev/beacon/install.sh | INSTALL_CLICKHOUSE=0 sh
#   Uninstall:         curl -sSfL https://johnnygreco.dev/beacon/install.sh | UNINSTALL=1 sh
set -euo pipefail

REPO="johnnygreco/beacon"
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"
BEACON_HOME="${BEACON_HOME:-${HOME}/.beacon}"
CLICKHOUSE_INSTALL_DIR="${CLICKHOUSE_INSTALL_DIR:-${BEACON_HOME}/bin}"
INSTALL_CLICKHOUSE="${INSTALL_CLICKHOUSE:-1}"
INCLUDE_PRERELEASE="${INCLUDE_PRERELEASE:-0}"
if [ -z "${CLICKHOUSE_VERSION:-}" ]; then
    if [ -n "${CLICKHOUSE_TAG:-}" ]; then
        CLICKHOUSE_VERSION="${CLICKHOUSE_TAG#v}"
        CLICKHOUSE_VERSION="${CLICKHOUSE_VERSION%%-*}"
    else
        CLICKHOUSE_VERSION="24.12.6.70"
    fi
fi
CLICKHOUSE_TAG="${CLICKHOUSE_TAG:-v${CLICKHOUSE_VERSION}-stable}"

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
    if [ -d "${HOME}/.beacon" ]; then
        rm -rf "${HOME}/.beacon"
        echo "Removed ~/.beacon data directory"
    fi
    echo "beacon uninstalled"
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

install_clickhouse() {
    if [ "$INSTALL_CLICKHOUSE" = "0" ]; then
        echo "Skipping ClickHouse install because INSTALL_CLICKHOUSE=0"
        return 0
    fi

    if command -v clickhouse >/dev/null 2>&1; then
        echo "ClickHouse already available at $(command -v clickhouse)"
        return 0
    fi

    clickhouse_bin="${CLICKHOUSE_INSTALL_DIR}/clickhouse"
    if [ -x "$clickhouse_bin" ]; then
        echo "ClickHouse already installed to ${clickhouse_bin}"
        return 0
    fi

    mkdir -p "$CLICKHOUSE_INSTALL_DIR"
    echo "Installing ClickHouse ${CLICKHOUSE_TAG}..."

    case "$OS" in
        darwin)
            clickhouse_asset="clickhouse-macos"
            if [ "$ARCH" = "arm64" ]; then
                clickhouse_asset="clickhouse-macos-aarch64"
            fi
            clickhouse_url="https://github.com/ClickHouse/ClickHouse/releases/download/${CLICKHOUSE_TAG}/${clickhouse_asset}"
            curl -sSfL "$clickhouse_url" -o "${tmp_dir}/clickhouse"
            chmod 755 "${tmp_dir}/clickhouse"
            mv "${tmp_dir}/clickhouse" "$clickhouse_bin"
            ;;
        linux)
            clickhouse_asset="clickhouse-common-static-${CLICKHOUSE_VERSION}-${ARCH}.tgz"
            clickhouse_url="https://github.com/ClickHouse/ClickHouse/releases/download/${CLICKHOUSE_TAG}/${clickhouse_asset}"
            clickhouse_extract_dir="${tmp_dir}/clickhouse-extract"
            mkdir -p "$clickhouse_extract_dir"
            curl -sSfL "$clickhouse_url" -o "${tmp_dir}/${clickhouse_asset}"
            tar -xzf "${tmp_dir}/${clickhouse_asset}" -C "$clickhouse_extract_dir"
            extracted_bin="$(find "$clickhouse_extract_dir" -type f -path '*/usr/bin/clickhouse' -print -quit)"
            if [ -z "$extracted_bin" ]; then
                echo "Error: could not find clickhouse binary in ${clickhouse_asset}"
                exit 1
            fi
            chmod 755 "$extracted_bin"
            mv "$extracted_bin" "$clickhouse_bin"
            ;;
        *)
            echo "Error: unsupported OS for ClickHouse install: $OS"
            exit 1
            ;;
    esac

    echo "$CLICKHOUSE_TAG" > "${clickhouse_bin}.version"
    echo "ClickHouse ${CLICKHOUSE_TAG} installed to ${clickhouse_bin}"
}

latest_release_version() {
    curl -sSf "https://api.github.com/repos/${REPO}/releases?per_page=50" | awk -v include_prerelease="$INCLUDE_PRERELEASE" '
        /"tag_name":/ {
            tag = $0
            sub(/^.*"tag_name": *"/, "", tag)
            sub(/".*$/, "", tag)
            draft = ""
            prerelease = ""
        }
        /"draft":/ && tag != "" && draft == "" {
            draft = $0
            sub(/^.*"draft": */, "", draft)
            sub(/,.*/, "", draft)
        }
        /"prerelease":/ && tag != "" && prerelease == "" {
            prerelease = $0
            sub(/^.*"prerelease": */, "", prerelease)
            sub(/,.*/, "", prerelease)
            if (found == "" && draft == "false" && (include_prerelease == "1" || prerelease == "false")) {
                found = tag
            }
        }
        END {
            if (found != "") print found
        }
    '
}

# Use provided version or fetch latest
if [ -z "${VERSION:-}" ]; then
    VERSION="$(latest_release_version)"
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

install_clickhouse

# Check if INSTALL_DIR is in PATH
case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *) echo "Add ${INSTALL_DIR} to your PATH: export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac
