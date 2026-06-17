#!/bin/sh
# vnetviz installer.
#
#   curl -fsSL https://raw.githubusercontent.com/vz-shark/vnetviz/main/install.sh | sh
#
# Downloads a prebuilt vnetviz binary from GitHub Releases and installs it into
# /usr/local/bin (using sudo if that directory is not writable). vnetviz is a
# Linux-only tool, so only linux/amd64 and linux/arm64 are published.
#
# Environment overrides (mostly for testing / unusual setups):
#   VNETVIZ_VERSION   release to install: "latest" (default) or a tag like v0.1.0
#   VNETVIZ_BIN_DIR   install directory (default: /usr/local/bin)
#   VNETVIZ_BASE_URL  fetch the tarball + checksums.txt directly from this URL
#                     instead of GitHub Releases (used by the local test harness)
#   VNETVIZ_NO_VERIFY set to 1 to skip the checksum verification
set -eu

REPO="vz-shark/vnetviz"
VERSION="${VNETVIZ_VERSION:-latest}"
BIN_DIR="${VNETVIZ_BIN_DIR:-/usr/local/bin}"
BINARY="vnetviz"

err() { printf 'install: %s\n' "$*" >&2; }
die() { err "$*"; exit 1; }

# --- detect platform ---------------------------------------------------------
os="$(uname -s)"
[ "$os" = "Linux" ] || die "vnetviz is Linux-only; detected $os"

case "$(uname -m)" in
	x86_64 | amd64)   arch="amd64" ;;
	aarch64 | arm64)  arch="arm64" ;;
	*) die "unsupported architecture: $(uname -m) (only amd64 and arm64 are published)" ;;
esac

asset="${BINARY}_linux_${arch}.tar.gz"

# --- pick a downloader -------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
	dl() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
	dl() { wget -qO "$2" "$1"; }
else
	die "need curl or wget to download releases"
fi

# --- resolve URLs ------------------------------------------------------------
if [ -n "${VNETVIZ_BASE_URL:-}" ]; then
	asset_url="${VNETVIZ_BASE_URL%/}/${asset}"
	sums_url="${VNETVIZ_BASE_URL%/}/checksums.txt"
elif [ "$VERSION" = "latest" ]; then
	asset_url="https://github.com/${REPO}/releases/latest/download/${asset}"
	sums_url="https://github.com/${REPO}/releases/latest/download/checksums.txt"
else
	asset_url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
	sums_url="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
fi

# --- download into a temp dir ------------------------------------------------
tmp="$(mktemp -d "${TMPDIR:-/tmp}/vnetviz-install.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

err "downloading ${asset} (${VERSION})"
dl "$asset_url" "$tmp/$asset" || die "download failed: $asset_url"

# --- verify checksum ---------------------------------------------------------
if [ "${VNETVIZ_NO_VERIFY:-0}" != "1" ]; then
	if command -v sha256sum >/dev/null 2>&1; then
		sha_cmd="sha256sum"
	elif command -v shasum >/dev/null 2>&1; then
		sha_cmd="shasum -a 256"
	else
		sha_cmd=""
	fi
	if [ -n "$sha_cmd" ] && dl "$sums_url" "$tmp/checksums.txt" 2>/dev/null; then
		want="$(awk -v f="$asset" '$2 == f || $2 == "*"f {print $1}' "$tmp/checksums.txt" | head -n1)"
		[ -n "$want" ] || die "no checksum for $asset in checksums.txt"
		got="$($sha_cmd "$tmp/$asset" | awk '{print $1}')"
		[ "$want" = "$got" ] || die "checksum mismatch for $asset (want $want, got $got)"
		err "checksum ok"
	else
		err "warning: skipping checksum verification (checksums.txt or sha256 tool unavailable)"
	fi
fi

# --- extract -----------------------------------------------------------------
tar -xzf "$tmp/$asset" -C "$tmp" || die "failed to extract $asset"
[ -f "$tmp/$BINARY" ] || die "archive did not contain a '$BINARY' binary"
chmod +x "$tmp/$BINARY"

# --- install (sudo only if needed) -------------------------------------------
install_to() {
	# $1 = the command prefix ("" or "sudo")
	$1 mkdir -p "$BIN_DIR" && $1 install -m 0755 "$tmp/$BINARY" "$BIN_DIR/$BINARY"
}

if [ -w "$BIN_DIR" ] || { [ ! -e "$BIN_DIR" ] && [ -w "$(dirname "$BIN_DIR")" ]; }; then
	install_to ""
elif [ "$(id -u)" = "0" ]; then
	install_to ""
elif command -v sudo >/dev/null 2>&1; then
	err "elevating with sudo to write to $BIN_DIR"
	install_to "sudo"
else
	die "cannot write to $BIN_DIR and sudo is not available; set VNETVIZ_BIN_DIR to a writable path"
fi

# --- report ------------------------------------------------------------------
installed="$BIN_DIR/$BINARY"
err "installed $installed"
if command -v "$BINARY" >/dev/null 2>&1 && [ "$(command -v "$BINARY")" = "$installed" ]; then
	"$installed" --version 2>/dev/null || true
else
	err "note: $BIN_DIR is not on your PATH; run it as $installed or add $BIN_DIR to PATH"
fi
