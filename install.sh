#!/bin/sh
# vnetviz installer.
#
#   curl -fsSL https://raw.githubusercontent.com/vz-shark/vnetviz/main/install.sh | sudo sh
#
# Downloads a prebuilt vnetviz binary from GitHub Releases and installs it. When
# run as root (e.g. via sudo) it installs system-wide into /usr/local/bin;
# unprivileged it installs into the user's own ~/.local/bin (no sudo needed).
# This script never calls sudo itself: if the chosen directory needs privileges
# we don't have, it tells you to re-run with sudo (or to pick a writable
# VNETVIZ_BIN_DIR) and exits. vnetviz is a Linux-only tool, so only linux/amd64
# and linux/arm64 are published.
#
# Environment overrides (mostly for testing / unusual setups):
#   VNETVIZ_VERSION   release to install: "latest" (default) or a tag like v0.1.0
#   VNETVIZ_BIN_DIR   install directory (default: /usr/local/bin as root,
#                     else ${XDG_BIN_HOME:-~/.local/bin})
#   VNETVIZ_BASE_URL  fetch the tarball + checksums.txt directly from this URL
#                     instead of GitHub Releases (used by the local test harness)
#   VNETVIZ_NO_VERIFY set to 1 to skip the checksum verification
set -eu

REPO="vz-shark/vnetviz"
VERSION="${VNETVIZ_VERSION:-latest}"
BINARY="vnetviz"

# Default install directory. An explicit VNETVIZ_BIN_DIR always wins. Otherwise
# we install system-wide when running as root (e.g. via sudo) and into the
# user's own ~/.local/bin when running unprivileged — the de-facto convention
# for per-user executables, which modern distros add to PATH automatically.
if [ -n "${VNETVIZ_BIN_DIR:-}" ]; then
	BIN_DIR="$VNETVIZ_BIN_DIR"
elif [ "$(id -u)" = "0" ]; then
	BIN_DIR="/usr/local/bin"
else
	BIN_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
fi

# --- logging -----------------------------------------------------------------
# Level-labelled output, colorized only when stderr is a terminal (plain when
# piped or redirected). Everything goes to stderr so stdout stays clean.
if [ -t 2 ]; then
	_e=$(printf '\033')
	C_INFO="${_e}[36m"; C_OK="${_e}[32m"; C_WARN="${_e}[33m"; C_ERR="${_e}[31m"; C_OFF="${_e}[0m"
else
	C_INFO=; C_OK=; C_WARN=; C_ERR=; C_OFF=
fi
info()    { printf '%sInfo:%s %s\n'    "$C_INFO" "$C_OFF" "$*" >&2; }
warn()    { printf '%sWarn:%s %s\n'    "$C_WARN" "$C_OFF" "$*" >&2; }
error()   { printf '%sError:%s %s\n'   "$C_ERR"  "$C_OFF" "$*" >&2; }
success() { printf '%sSuccess:%s %s\n' "$C_OK"   "$C_OFF" "$*" >&2; }
die() { error "$*"; exit 1; }

# --- detect platform ---------------------------------------------------------
os="$(uname -s)"
[ "$os" = "Linux" ] || die "vnetviz is Linux-only; detected $os"

case "$(uname -m)" in
	x86_64 | amd64)   arch="amd64" ;;
	aarch64 | arm64)  arch="arm64" ;;
	*) die "unsupported architecture: $(uname -m) (only amd64 and arm64 are published)" ;;
esac

# --- pick a downloader -------------------------------------------------------
# latest_tag resolves the most recent release tag from the /releases/latest
# redirect target, so no GitHub API call (and no rate limit) is needed.
if command -v curl >/dev/null 2>&1; then
	dl() { curl -fsSL "$1" -o "$2"; }
	latest_tag() {
		url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
			"https://github.com/${REPO}/releases/latest") || return 1
		printf '%s\n' "${url##*/}"
	}
elif command -v wget >/dev/null 2>&1; then
	dl() { wget -qO "$2" "$1"; }
	latest_tag() {
		url=$(wget -S --max-redirect=0 -O /dev/null \
			"https://github.com/${REPO}/releases/latest" 2>&1 \
			| awk 'tolower($1) == "location:" { print $2 }' | tr -d '\r' | tail -n1)
		[ -n "$url" ] && printf '%s\n' "${url##*/}"
	}
else
	die "need curl or wget to download releases"
fi

# --- resolve version, asset name, and URLs -----------------------------------
# Asset names embed the version (vnetviz_<ver>_linux_<arch>.tar.gz), so when
# installing "latest" we must first resolve the concrete tag.
if [ "$VERSION" = "latest" ] && [ -z "${VNETVIZ_BASE_URL:-}" ]; then
	VERSION="$(latest_tag)" || die "could not resolve the latest release tag"
	[ -n "$VERSION" ] || die "could not resolve the latest release tag"
fi
[ "$VERSION" != "latest" ] || die "set VNETVIZ_VERSION (e.g. v0.1.0) when using VNETVIZ_BASE_URL"

asset="${BINARY}_${VERSION#v}_linux_${arch}.tar.gz"

if [ -n "${VNETVIZ_BASE_URL:-}" ]; then
	base="${VNETVIZ_BASE_URL%/}"
else
	base="https://github.com/${REPO}/releases/download/${VERSION}"
fi
asset_url="${base}/${asset}"
sums_url="${base}/checksums.txt"

# --- download into a temp dir ------------------------------------------------
tmp="$(mktemp -d "${TMPDIR:-/tmp}/vnetviz-install.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

info "downloading ${asset} (${VERSION})"
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
		info "checksum verified"
	else
		warn "skipping checksum verification (checksums.txt or sha256 tool unavailable)"
	fi
fi

# --- extract -----------------------------------------------------------------
tar -xzf "$tmp/$asset" -C "$tmp" || die "failed to extract $asset"
[ -f "$tmp/$BINARY" ] || die "archive did not contain a '$BINARY' binary"
chmod +x "$tmp/$BINARY"

# --- install -----------------------------------------------------------------
# This script never calls sudo itself. We create the target directory when we
# can (always possible for the unprivileged ~/.local/bin default); if the copy
# would need privileges we don't have, explain how to re-run and exit.
if mkdir -p "$BIN_DIR" 2>/dev/null && [ -w "$BIN_DIR" ]; then
	install -m 0755 "$tmp/$BINARY" "$BIN_DIR/$BINARY"
else
	error "cannot write to $BIN_DIR without elevated privileges — nothing was installed."
	printf '       re-run with sudo:\n' >&2
	printf '           curl -fsSL https://raw.githubusercontent.com/%s/main/install.sh | sudo sh\n' "$REPO" >&2
	printf '       or install into a directory you own:\n' >&2
	printf '           curl -fsSL https://raw.githubusercontent.com/%s/main/install.sh | VNETVIZ_BIN_DIR="$HOME/.local/bin" sh\n' "$REPO" >&2
	exit 1
fi

# --- report ------------------------------------------------------------------
installed="$BIN_DIR/$BINARY"
ver_line=$("$installed" --version 2>/dev/null | head -n1)
success "${ver_line:-$BINARY} installed to $installed"
if ! { command -v "$BINARY" >/dev/null 2>&1 && [ "$(command -v "$BINARY")" = "$installed" ]; }; then
	warn "$BIN_DIR is not on your PATH — run it as $installed, or add $BIN_DIR to PATH"
fi
