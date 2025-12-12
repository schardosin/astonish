#!/bin/sh
set -e

# --- CONFIGURATION (CHANGE THESE) ---
OWNER="schardosin"
REPO="astonish"
BINARY="astonish"
# ------------------------------------

# 1. Detect OS
OS="$(uname -s)"
case "$OS" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *)      echo "OS $OS is not supported"; exit 1 ;;
esac

# 2. Detect Arch
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)      echo "Architecture $ARCH is not supported"; exit 1 ;;
esac

echo "Detected OS: $OS"
echo "Detected Arch: $ARCH"

# 3. Find the latest release URL
# We construct the asset name we want: astonish-OS-ARCH (e.g., astonish-linux-amd64)
# We avoid .exe since we are on *nix
ASSET_NAME="${BINARY}-${OS}-${ARCH}"

echo "Fetching latest release for asset: $ASSET_NAME..."

RELEASE_URL=$(curl -s "https://api.github.com/repos/$OWNER/$REPO/releases/latest" | \
    grep "browser_download_url" | \
    grep "$ASSET_NAME" | \
    cut -d '"' -f 4 | \
    head -n 1)

if [ -z "$RELEASE_URL" ]; then
    echo "Error: Could not find a release asset named '$ASSET_NAME'."
    echo "Please check the release page to ensure binaries are uploaded correctly."
    exit 1
fi

echo "Found latest version: $RELEASE_URL"

# 4. Download and Install
TEMP_DIR=$(mktemp -d)
echo "Downloading..."
# Download directly as the binary name
curl -L -o "$TEMP_DIR/$BINARY" "$RELEASE_URL"

# Install
INSTALL_DIR="/usr/local/bin"
echo "Installing to $INSTALL_DIR..."

chmod +x "$TEMP_DIR/$BINARY"
sudo mv "$TEMP_DIR/$BINARY" "$INSTALL_DIR/$BINARY"

rm -rf "$TEMP_DIR"

echo "Success! Run '$BINARY' to start."
