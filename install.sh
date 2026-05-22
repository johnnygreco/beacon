#!/bin/sh
# Usage:
#   Latest stable:     curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
#   Latest prerelease: curl -sSfL https://johnnygreco.dev/beacon/install.sh | INCLUDE_PRERELEASE=1 sh
#   Pinned:            curl -sSfL https://johnnygreco.dev/beacon/install.sh | VERSION=x.y.z sh
#   No DB dep:         curl -sSfL https://johnnygreco.dev/beacon/install.sh | INSTALL_CLICKHOUSE=0 sh
#   Skip checksums:    curl -sSfL https://johnnygreco.dev/beacon/install.sh | VERIFY_CHECKSUMS=0 sh
#   Uninstall:         curl -sSfL https://johnnygreco.dev/beacon/install.sh | UNINSTALL=1 sh
set -eu

# Keep the advertised `curl ... | sh` install path compatible with shells like
# Ubuntu's dash. Enable pipefail only when the current shell supports it.
(set -o pipefail) 2>/dev/null && set -o pipefail

REPO="johnnygreco/beacon"
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"
BEACON_HOME="${BEACON_HOME:-${HOME}/.beacon}"
CLICKHOUSE_INSTALL_DIR="${CLICKHOUSE_INSTALL_DIR:-${BEACON_HOME}/bin}"
INSTALL_CLICKHOUSE="${INSTALL_CLICKHOUSE:-1}"
INCLUDE_PRERELEASE="${INCLUDE_PRERELEASE:-0}"
VERIFY_CHECKSUMS="${VERIFY_CHECKSUMS:-1}"
if [ -z "${CLICKHOUSE_VERSION:-}" ]; then
    if [ -n "${CLICKHOUSE_TAG:-}" ]; then
        CLICKHOUSE_VERSION="${CLICKHOUSE_TAG#v}"
        CLICKHOUSE_VERSION="${CLICKHOUSE_VERSION%%-*}"
    else
        CLICKHOUSE_VERSION="24.12.6.70"
    fi
fi
CLICKHOUSE_TAG="${CLICKHOUSE_TAG:-v${CLICKHOUSE_VERSION}-stable}"
tmp_dir=""

cleanup() {
    if [ -n "$tmp_dir" ] && [ -d "$tmp_dir" ]; then
        rm -rf "$tmp_dir"
    fi
}

need_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Error: required command not found: $1" >&2
        exit 1
    fi
}

download_file() {
    url="$1"
    destination="$2"
    label="$3"
    if ! curl -fsSL --proto '=https' --tlsv1.2 "$url" -o "$destination"; then
        echo "Error: failed to download ${label}: ${url}" >&2
        exit 1
    fi
}

checksum_value() {
    algorithm="$1"
    file="$2"
    case "$algorithm" in
        sha256)
            if command -v sha256sum >/dev/null 2>&1; then
                sha256sum "$file" | awk '{print $1}'
                return 0
            fi
            if command -v shasum >/dev/null 2>&1; then
                shasum -a 256 "$file" | awk '{print $1}'
                return 0
            fi
            ;;
        sha512)
            if command -v sha512sum >/dev/null 2>&1; then
                sha512sum "$file" | awk '{print $1}'
                return 0
            fi
            if command -v shasum >/dev/null 2>&1; then
                shasum -a 512 "$file" | awk '{print $1}'
                return 0
            fi
            ;;
        *)
            echo "Error: unsupported checksum algorithm: ${algorithm}" >&2
            exit 1
            ;;
    esac
    echo "Error: cannot verify ${algorithm} checksum; install sha256sum/sha512sum or shasum." >&2
    exit 1
}

checksum_from_file() {
    artifact="$1"
    checksum_file="$2"
    awk -v artifact="$artifact" '
        $1 ~ /^[0-9a-fA-F]+$/ && ($2 == artifact || $2 == "*" artifact) {
            print $1
            found = 1
            exit
        }
        NF == 1 && $1 ~ /^[0-9a-fA-F]+$/ {
            single = $1
        }
        END {
            if (found != 1 && single != "") print single
        }
    ' "$checksum_file"
}

verify_checksum() {
    algorithm="$1"
    artifact="$2"
    file="$3"
    checksum_file="$4"
    if [ "$VERIFY_CHECKSUMS" = "0" ]; then
        echo "Warning: checksum verification disabled for ${artifact}" >&2
        return 0
    fi
    expected="$(checksum_from_file "$artifact" "$checksum_file")"
    if [ -z "$expected" ]; then
        echo "Error: checksum file does not contain an entry for ${artifact}" >&2
        exit 1
    fi
    actual="$(checksum_value "$algorithm" "$file")"
    if [ "$actual" != "$expected" ]; then
        echo "Error: checksum mismatch for ${artifact}" >&2
        echo "Expected: ${expected}" >&2
        echo "Actual:   ${actual}" >&2
        exit 1
    fi
    echo "Verified ${artifact} ${algorithm} checksum"
}

install_executable() {
    source_file="$1"
    target_file="$2"
    target_dir="${target_file%/*}"
    if [ "$target_dir" = "$target_file" ]; then
        target_dir="."
    fi

    if ! mkdir -p "$target_dir" 2>/dev/null; then
        echo "Creating ${target_dir} (requires sudo)..."
        sudo mkdir -p "$target_dir"
    fi

    if [ -w "$target_dir" ]; then
        staging_file="${target_file}.tmp.$$"
        rm -f "$staging_file"
        if command -v install >/dev/null 2>&1; then
            if ! install -m 755 "$source_file" "$staging_file"; then
                rm -f "$staging_file"
                exit 1
            fi
        else
            if ! cp "$source_file" "$staging_file"; then
                rm -f "$staging_file"
                exit 1
            fi
            chmod 755 "$staging_file"
        fi
        if ! mv "$staging_file" "$target_file"; then
            rm -f "$staging_file"
            exit 1
        fi
    else
        echo "Installing ${target_file} (requires sudo)..."
        staging_file="${target_file}.tmp.$$"
        sudo rm -f "$staging_file"
        if command -v install >/dev/null 2>&1; then
            if ! sudo install -m 755 "$source_file" "$staging_file"; then
                sudo rm -f "$staging_file"
                exit 1
            fi
        else
            if ! sudo cp "$source_file" "$staging_file"; then
                sudo rm -f "$staging_file"
                exit 1
            fi
            sudo chmod 755 "$staging_file"
        fi
        if ! sudo mv "$staging_file" "$target_file"; then
            sudo rm -f "$staging_file"
            exit 1
        fi
    fi
}

stop_managed_clickhouse() {
    force="${1:-0}"
    pid_file="${BEACON_HOME}/clickhouse/clickhouse.pid"
    data_dir="${BEACON_HOME}/clickhouse/data"

    if [ -f "$pid_file" ]; then
        pid="$(cat "$pid_file" 2>/dev/null || true)"
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            if [ "$force" != "1" ]; then
                return 0
            fi
            echo "Stopping Beacon-managed ClickHouse (pid ${pid})..."
            kill "$pid" 2>/dev/null || true
            i=0
            while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 20 ]; do
                sleep 0.5
                i=$((i + 1))
            done
        fi
        rm -f "$pid_file"
    fi

    # If the pidfile is gone but the managed server is still alive, it can keep
    # serving from a deleted/corrupt ~/.beacon/clickhouse tree. Stop that stale
    # process before installing or removing files.
    pids="$(ps -eo pid=,args= 2>/dev/null | awk -v data_dir="$data_dir" '
        index($0, "clickhouse") && index($0, " server ") && index($0, "--path=" data_dir) { print $1 }
    ' || true)"
    if [ -n "$pids" ]; then
        echo "Stopping stale Beacon-managed ClickHouse..."
        for pid in $pids; do
            kill "$pid" 2>/dev/null || true
        done
        i=0
        while [ "$i" -lt 20 ]; do
            alive=""
            for pid in $pids; do
                if kill -0 "$pid" 2>/dev/null; then
                    alive="1"
                fi
            done
            [ -z "$alive" ] && break
            sleep 0.5
            i=$((i + 1))
        done
    fi
}

# Uninstall
if [ "${UNINSTALL:-}" = "1" ]; then
    stop_managed_clickhouse 1
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

need_cmd curl
need_cmd find
need_cmd tar
need_cmd awk

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

    if ! mkdir -p "$CLICKHOUSE_INSTALL_DIR" 2>/dev/null; then
        echo "Creating ${CLICKHOUSE_INSTALL_DIR} (requires sudo)..."
        sudo mkdir -p "$CLICKHOUSE_INSTALL_DIR"
    fi
    echo "Installing ClickHouse ${CLICKHOUSE_TAG}..."

    case "$OS" in
        darwin)
            clickhouse_asset="clickhouse-macos"
            if [ "$ARCH" = "arm64" ]; then
                clickhouse_asset="clickhouse-macos-aarch64"
            fi
            clickhouse_url="https://github.com/ClickHouse/ClickHouse/releases/download/${CLICKHOUSE_TAG}/${clickhouse_asset}"
            download_file "$clickhouse_url" "${tmp_dir}/clickhouse" "ClickHouse ${clickhouse_asset}"
            if [ "$VERIFY_CHECKSUMS" != "0" ]; then
                echo "Warning: upstream ClickHouse macOS assets do not publish sidecar checksums; relying on GitHub TLS and pinned ${CLICKHOUSE_TAG}." >&2
            fi
            install_executable "${tmp_dir}/clickhouse" "$clickhouse_bin"
            ;;
        linux)
            clickhouse_asset="clickhouse-common-static-${CLICKHOUSE_VERSION}-${ARCH}.tgz"
            clickhouse_url="https://github.com/ClickHouse/ClickHouse/releases/download/${CLICKHOUSE_TAG}/${clickhouse_asset}"
            clickhouse_extract_dir="${tmp_dir}/clickhouse-extract"
            mkdir -p "$clickhouse_extract_dir"
            download_file "$clickhouse_url" "${tmp_dir}/${clickhouse_asset}" "ClickHouse ${clickhouse_asset}"
            if [ "$VERIFY_CHECKSUMS" != "0" ]; then
                download_file "${clickhouse_url}.sha512" "${tmp_dir}/${clickhouse_asset}.sha512" "ClickHouse ${clickhouse_asset}.sha512"
                verify_checksum sha512 "$clickhouse_asset" "${tmp_dir}/${clickhouse_asset}" "${tmp_dir}/${clickhouse_asset}.sha512"
            else
                echo "Warning: checksum verification disabled for ${clickhouse_asset}" >&2
            fi
            tar -xzf "${tmp_dir}/${clickhouse_asset}" -C "$clickhouse_extract_dir"
            extracted_bin="$(find "$clickhouse_extract_dir" -type f -path '*/usr/bin/clickhouse' -print -quit)"
            if [ -z "$extracted_bin" ]; then
                echo "Error: could not find clickhouse binary in ${clickhouse_asset}"
                exit 1
            fi
            install_executable "$extracted_bin" "$clickhouse_bin"
            ;;
        *)
            echo "Error: unsupported OS for ClickHouse install: $OS"
            exit 1
            ;;
    esac

    if [ -w "$CLICKHOUSE_INSTALL_DIR" ]; then
        echo "$CLICKHOUSE_TAG" > "${clickhouse_bin}.version"
    else
        echo "$CLICKHOUSE_TAG" | sudo tee "${clickhouse_bin}.version" >/dev/null
    fi
    echo "ClickHouse ${CLICKHOUSE_TAG} installed to ${clickhouse_bin}"
}

latest_stable_release_version() {
    latest_url="$(curl -sSfL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
    tag="${latest_url##*/}"
    if [ -n "$tag" ] && [ "$tag" != "latest" ]; then
        echo "$tag"
    fi
}

latest_listed_release_version() {
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

latest_release_version() {
    if [ "$INCLUDE_PRERELEASE" = "1" ]; then
        latest_listed_release_version
    else
        latest_stable_release_version
    fi
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
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

echo "Installing beacon ${VERSION} (${OS}/${ARCH})..."

# Download, verify, and extract before modifying the current install.
tmp_dir="$(mktemp -d)"
trap cleanup EXIT

download_file "$URL" "${tmp_dir}/${ARCHIVE}" "${ARCHIVE}"
if [ "$VERIFY_CHECKSUMS" != "0" ]; then
    download_file "$CHECKSUMS_URL" "${tmp_dir}/checksums.txt" "checksums.txt"
    verify_checksum sha256 "$ARCHIVE" "${tmp_dir}/${ARCHIVE}" "${tmp_dir}/checksums.txt"
else
    echo "Warning: checksum verification disabled for ${ARCHIVE}" >&2
fi
tar -xzf "${tmp_dir}/${ARCHIVE}" -C "$tmp_dir"
if [ ! -f "${tmp_dir}/beacon" ]; then
    echo "Error: ${ARCHIVE} did not contain a beacon binary" >&2
    exit 1
fi

stop_managed_clickhouse 0
install_clickhouse

# Install binary only after all required downloads have verified.
install_executable "${tmp_dir}/beacon" "${INSTALL_DIR}/beacon"

echo "beacon ${VERSION} installed to ${INSTALL_DIR}/beacon"

# Check if INSTALL_DIR is in PATH
case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *) echo "Add ${INSTALL_DIR} to your PATH: export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac
