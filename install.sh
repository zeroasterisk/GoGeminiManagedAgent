#!/usr/bin/env bash
set -euo pipefail

# Installer for gemini-managed-agents
REPO="zeroasterisk/GoGeminiManagedAgent"
BINARY_NAME="gemini-managed-agents"
INSTALL_DIR="/usr/local/bin"

echo "Installing ${BINARY_NAME}..."

if command -v go >/dev/null 2>&1; then
    echo "Found Go toolchain. Building from source..."
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT
    git clone --depth 1 "https://github.com/${REPO}.git" "$TMP_DIR"
    (cd "$TMP_DIR" && go build -o "${BINARY_NAME}" ./src)
    if [ -w "$INSTALL_DIR" ]; then
        mv "$TMP_DIR/${BINARY_NAME}" "$INSTALL_DIR/"
    else
        echo "Elevating permissions to install into ${INSTALL_DIR}..."
        sudo mv "$TMP_DIR/${BINARY_NAME}" "$INSTALL_DIR/"
    fi
    echo "${BINARY_NAME} installed successfully to ${INSTALL_DIR}/${BINARY_NAME}"
    exit 0
fi

echo "Error: Go 1.25+ is required to build ${BINARY_NAME}."
echo "Please install Go from https://go.dev/dl/ and retry."
exit 1
