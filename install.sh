#!/usr/bin/env bash
set -e
# pipefail makes a pipeline fail when any stage fails, not just the last one.
# Without it the release-tag lookup below could not tell a failed curl from an
# empty response body.
set -o pipefail

# install.sh - Zero-dependency devgeta installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cjairm/devgeta/main/install.sh | bash
#   bash install.sh --local /path/to/devgeta-binary

REPO="cjairm/devgeta"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="devgeta"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_error() {
	echo -e "${RED}Error: $1${NC}" >&2
}

print_success() {
	echo -e "${GREEN}$1${NC}"
}

print_info() {
	echo -e "${YELLOW}$1${NC}"
}

# Parse command-line arguments
LOCAL_BINARY=""
while [[ $# -gt 0 ]]; do
	case $1 in
	--local)
		LOCAL_BINARY="$2"
		shift 2
		;;
	*)
		print_error "Unknown option: $1"
		echo "Usage: $0 [--local /path/to/binary]"
		exit 1
		;;
	esac
done

# Detect OS
OS=$(uname -s)
case "$OS" in
Darwin)
	OS_NAME="darwin"
	;;
Linux)
	OS_NAME="linux"
	;;
*)
	print_error "Unsupported operating system: $OS"
	echo "Devgeta only supports macOS (Darwin) and Linux (Debian/Ubuntu)."
	exit 1
	;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
x86_64)
	ARCH_NAME="amd64"
	;;
amd64)
	ARCH_NAME="amd64"
	;;
arm64)
	ARCH_NAME="arm64"
	;;
aarch64)
	ARCH_NAME="arm64"
	;;
*)
	print_error "Unsupported architecture: $ARCH"
	echo "Devgeta only supports amd64 (x86_64) and arm64 (aarch64) architectures."
	exit 1
	;;
esac

BINARY_FILENAME="devgeta-${OS_NAME}-${ARCH_NAME}"

print_info "Installing devgeta for ${OS_NAME}/${ARCH_NAME}..."

# Create install directory if it doesn't exist
mkdir -p "$INSTALL_DIR"

# Install binary
DEST_PATH="$INSTALL_DIR/$BINARY_NAME"

# The binary is always staged in a temporary file first and only moved into
# place once it is complete and runnable. Writing straight to DEST_PATH would
# let an interrupted or partial transfer leave a truncated, executable devgeta
# on the user's PATH. The temp file lives in INSTALL_DIR so the final mv is a
# rename inside a single filesystem, which is atomic.
TMP_BINARY=""

cleanup() {
	if [ -n "$TMP_BINARY" ] && [ -f "$TMP_BINARY" ]; then
		rm -f "$TMP_BINARY"
	fi
}

trap cleanup EXIT
trap 'cleanup; exit 130' INT TERM

# Six X's keeps this template valid for both BSD (macOS) and GNU (Debian/Ubuntu) mktemp.
TMP_BINARY=$(mktemp "$INSTALL_DIR/.devgeta.XXXXXX")

if [ -n "$LOCAL_BINARY" ]; then
	# Local installation mode
	print_info "Installing from local file: $LOCAL_BINARY"

	if [ ! -f "$LOCAL_BINARY" ]; then
		print_error "Local binary not found: $LOCAL_BINARY"
		exit 1
	fi

	cp "$LOCAL_BINARY" "$TMP_BINARY"
else
	# Download from GitHub
	print_info "Fetching latest release from GitHub..."

	# Fetch and extract the latest release tag in two steps, so a failing curl
	# is reported as a failed request instead of surfacing as an empty tag.
	if ! RELEASE_JSON=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest"); then
		print_error "Failed to query the GitHub releases API"
		print_error "URL: https://api.github.com/repos/$REPO/releases/latest"
		exit 1
	fi

	# sed -n exits 0 when nothing matches, so an unexpected response shape is
	# caught by the empty check below instead of by an opaque pipeline failure.
	LATEST_RELEASE=$(printf '%s\n' "$RELEASE_JSON" |
		sed -n -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')

	if [ -z "$LATEST_RELEASE" ]; then
		print_error "Failed to fetch latest release tag from GitHub"
		exit 1
	fi

	print_info "Latest release: $LATEST_RELEASE"

	# Construct download URL
	DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST_RELEASE/$BINARY_FILENAME"

	print_info "Downloading from: $DOWNLOAD_URL"

	# Download binary
	if ! curl -fsSL -o "$TMP_BINARY" "$DOWNLOAD_URL"; then
		print_error "Failed to download binary from GitHub"
		print_error "URL: $DOWNLOAD_URL"
		exit 1
	fi
fi

# Verify the staged binary before it becomes the devgeta on the user's PATH.
if [ ! -s "$TMP_BINARY" ]; then
	print_error "The devgeta binary is empty; the transfer did not complete"
	exit 1
fi

chmod +x "$TMP_BINARY"

if ! "$TMP_BINARY" --version >/dev/null 2>&1; then
	print_error "The devgeta binary did not run; it is corrupt or built for another platform"
	print_error "Expected: ${OS_NAME}/${ARCH_NAME}"
	exit 1
fi

# Commit point: this rename replaces any previous install in a single step.
mv -f "$TMP_BINARY" "$DEST_PATH"
TMP_BINARY=""

print_success "Installed devgeta to $DEST_PATH"

# Detect shell and shell config file
SHELL_CONFIG=""
CURRENT_SHELL=$(basename "$SHELL")

case "$CURRENT_SHELL" in
zsh)
	SHELL_CONFIG="$HOME/.zshrc"
	;;
bash)
	# Check for .bash_profile first (macOS), then .bashrc (Linux)
	if [ -f "$HOME/.bash_profile" ]; then
		SHELL_CONFIG="$HOME/.bash_profile"
	else
		SHELL_CONFIG="$HOME/.bashrc"
	fi
	;;
*)
	print_error "Unsupported shell: $CURRENT_SHELL"
	echo "Please manually add $INSTALL_DIR to your PATH"
	exit 1
	;;
esac

# Add to PATH and create alias if not already present
PATH_EXPORT="export PATH=\"\$HOME/.local/bin:\$PATH\""
ALIAS_EXPORT="alias dg='devgeta'"
SOURCE_CONFIG="source $HOME/.local/share/devgeta/devgeta.zsh"

if [ -f "$SHELL_CONFIG" ]; then
	# Check if devgeta installer block already exists
	if grep -qF "# Added by devgeta installer" "$SHELL_CONFIG" 2>/dev/null; then
		print_info "devgeta already configured in $SHELL_CONFIG"
	else
		print_info "Adding devgeta configuration to $SHELL_CONFIG"
		echo "" >>"$SHELL_CONFIG"
		echo "# Added by devgeta installer" >>"$SHELL_CONFIG"
		echo "$PATH_EXPORT" >>"$SHELL_CONFIG"
		echo "$ALIAS_EXPORT" >>"$SHELL_CONFIG"
		echo "$SOURCE_CONFIG" >>"$SHELL_CONFIG"
		print_success "Updated $SHELL_CONFIG"
	fi
else
	# Create shell config if it doesn't exist
	print_info "Creating $SHELL_CONFIG"
	echo "# Added by devgeta installer" >"$SHELL_CONFIG"
	echo "$PATH_EXPORT" >>"$SHELL_CONFIG"
	echo "$ALIAS_EXPORT" >>"$SHELL_CONFIG"
	echo "$SOURCE_CONFIG" >>"$SHELL_CONFIG"
	print_success "Created $SHELL_CONFIG with devgeta configuration"
fi

# Verify installation
print_info "Verifying installation..."

# Add to current PATH for verification
export PATH="$INSTALL_DIR:$PATH"

if command -v devgeta &>/dev/null; then
	INSTALLED_VERSION=$(devgeta --version 2>/dev/null || echo "unknown")
	print_success "✓ devgeta installed successfully!"
	print_success "  Version: $INSTALLED_VERSION"
	echo ""
	print_info "Next steps:"
	echo "  1. Restart your shell or run: source $SHELL_CONFIG"
	echo "  2. Run: dg install"
	echo ""
	print_info "Available commands:"
	echo "  dg install              - Set up your development environment"
	echo "  dg install --only terminal   - Install only terminal tools"
	echo "  dg install --skip desktop    - Install everything except desktop apps"
	echo ""
else
	print_error "Installation verification failed"
	echo "Binary installed to: $DEST_PATH"
	echo "Please restart your shell and try again"
	exit 1
fi
