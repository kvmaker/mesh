#!/bin/bash
set -euo pipefail

REPO="kvmaker/mesh"
INSTALL_DIR="/usr/local/bin"

usage() {
    echo "Usage: install.sh [meshd|mesh]"
    echo ""
    echo "Install mesh VPN binaries from GitHub Releases."
    echo ""
    echo "Examples:"
    echo "  curl -fsSL https://raw.githubusercontent.com/$REPO/master/install.sh | bash -s -- meshd"
    echo "  curl -fsSL https://raw.githubusercontent.com/$REPO/master/install.sh | bash -s -- mesh"
    exit 1
}

detect_os() {
    case "$(uname -s)" in
        Linux)  echo "linux" ;;
        Darwin) echo "darwin" ;;
        *)      echo "unsupported"; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)            echo "unsupported"; exit 1 ;;
    esac
}

get_latest_version() {
    curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/'
}

main() {
    local binary="${1:-}"
    if [[ -z "$binary" ]] || [[ "$binary" != "meshd" && "$binary" != "mesh" ]]; then
        usage
    fi

    local os=$(detect_os)
    local arch=$(detect_arch)

    # meshd 仅支持 linux/amd64
    if [[ "$binary" == "meshd" ]]; then
        if [[ "$os" != "linux" || "$arch" != "amd64" ]]; then
            echo "Error: meshd is only available for linux/amd64"
            exit 1
        fi
    fi

    echo "Detecting platform: ${os}/${arch}"

    local version=$(get_latest_version)
    if [[ -z "$version" ]]; then
        echo "Error: could not determine latest version"
        exit 1
    fi
    echo "Latest version: v${version}"

    local filename="${binary}_${version}_${os}_${arch}.tar.gz"
    local url="https://github.com/$REPO/releases/download/v${version}/${filename}"

    echo "Downloading ${url}..."
    local tmpdir=$(mktemp -d)
    trap "rm -rf $tmpdir" EXIT

    curl -fsSL "$url" -o "$tmpdir/$filename"
    tar -xzf "$tmpdir/$filename" -C "$tmpdir"

    echo "Installing ${binary} to ${INSTALL_DIR}..."
    if [[ -w "$INSTALL_DIR" ]]; then
        mv "$tmpdir/$binary" "$INSTALL_DIR/$binary"
    else
        sudo mv "$tmpdir/$binary" "$INSTALL_DIR/$binary"
    fi
    chmod +x "$INSTALL_DIR/$binary"

    echo "Done! ${binary} v${version} installed to ${INSTALL_DIR}/${binary}"
    echo ""
    echo "Run '${binary} --help' to get started."
}

main "$@"
