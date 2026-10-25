#!/bin/sh
# install.sh installs the groot CLI from a GitHub release on macOS or Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/tinchoram/groot-cli/main/install.sh | sh
#   sh install.sh --help
#
# It downloads groot_<version>_<os>_<arch>.tar.gz, checks it against
# checksums.txt, and puts the groot binary in GROOT_INSTALL_DIR. Then it runs
# `groot doctor`. Only POSIX sh is used, so it runs under dash and /bin/sh.

set -eu

REPO=${GROOT_REPO:-tinchoram/groot-cli}
API=${GROOT_BASE_URL:-https://api.github.com}
DOWNLOAD=${GROOT_DOWNLOAD_BASE:-https://github.com}
INSTALL_DIR=${GROOT_INSTALL_DIR:-$HOME/.local/bin}
TOKEN=${GITHUB_TOKEN:-${GH_TOKEN:-}}
PRIVATE_HINT="the groot repository is private for now: set GITHUB_TOKEN (or GH_TOKEN) to a GitHub token that can read it, until the repository is public"
TAB=$(printf '\t')
TMP=
STAGE=
HTTP_STATUS=

usage() {
	cat <<'EOF'
usage: install.sh [-h|--help]

Installs groot into $GROOT_INSTALL_DIR (default: $HOME/.local/bin).

  GROOT_VERSION        release to install, with or without the v (default: latest)
  GROOT_INSTALL_DIR    install directory (default: $HOME/.local/bin)
  GITHUB_TOKEN         token that can read the repository; GH_TOKEN is the fallback.
                       Needed while the repository is private.
  GROOT_REPO           repository (default: tinchoram/groot-cli)
  GROOT_BASE_URL       GitHub API root (default: https://api.github.com), for tests
  GROOT_DOWNLOAD_BASE  download root for assets with no browser_download_url
                       (default: https://github.com), for tests
EOF
}

die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	if [ -n "$TMP" ]; then
		rm -rf "$TMP"
	fi
	if [ -n "$STAGE" ]; then
		rm -f "$STAGE"
	fi
}

# detect_platform sets OS (darwin or linux) and ARCH (amd64 or arm64).
detect_platform() {
	case $(uname -s) in
	Darwin) OS=darwin ;;
	Linux) OS=linux ;;
	*) die "unsupported operating system: $(uname -s). groot installs on macOS (darwin) and Linux." ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) ARCH=amd64 ;;
	arm64 | aarch64) ARCH=arm64 ;;
	*) die "unsupported architecture: $(uname -m). groot installs on amd64 and arm64." ;;
	esac
}

# http_get URL FILE [ACCEPT] stores the body in FILE and the status in
# HTTP_STATUS. It fails only when the request itself failed.
http_get() {
	url=$1
	out=$2
	accept=${3:-}
	if command -v curl >/dev/null 2>&1; then
		set -- -sSL -o "$out" -w '%{http_code}'
		if [ -n "$accept" ]; then
			set -- "$@" -H "Accept: $accept"
		fi
		if [ -n "$TOKEN" ]; then
			set -- "$@" -H "Authorization: Bearer $TOKEN"
		fi
		HTTP_STATUS=$(curl "$@" "$url") || return 1
	else
		set -- -q -S -O "$out"
		if [ -n "$accept" ]; then
			set -- "$@" "--header=Accept: $accept"
		fi
		if [ -n "$TOKEN" ]; then
			set -- "$@" "--header=Authorization: Bearer $TOKEN"
		fi
		if wget "$@" "$url" 2>"$TMP/wget.err"; then
			HTTP_STATUS=200
		else
			HTTP_STATUS=$(awk '/^ *HTTP\//{c=$2} END{print c}' "$TMP/wget.err")
			[ -n "$HTTP_STATUS" ] || return 1
		fi
	fi
}

# find_asset NAME sets ASSET_API (the API url, used with a token) and
# ASSET_URL (the public download url) of the release asset NAME.
find_asset() {
	info=$(tr -d '\r\n' <"$TMP/release.json" | tr ',' '\n' | awk -v want="$1" '
		/"url": *"[^"]*\/releases\/assets\// { v = $0; sub(/.*"url": *"/, "", v); sub(/".*/, "", v); api = v }
		/"name": *"/ { v = $0; sub(/.*"name": *"/, "", v); sub(/".*/, "", v); if (v == want) { hit = 1; hit_api = api } }
		/"browser_download_url": *"/ { if (hit && !done) { v = $0; sub(/.*"browser_download_url": *"/, "", v); sub(/".*/, "", v); browser = v; done = 1 } }
		END { if (hit) printf "%s\t%s\n", hit_api, browser }
	')
	[ -n "$info" ] || die "release $TAG has no asset $1"
	ASSET_API=${info%%"$TAB"*}
	ASSET_URL=${info#*"$TAB"}
	if [ -z "$ASSET_URL" ]; then
		ASSET_URL=$DOWNLOAD/$REPO/releases/download/$TAG/$1
	fi
}

# fetch_asset NAME FILE downloads a release asset. A token makes it go through
# the API url, which is how a private repository serves its assets.
fetch_asset() {
	find_asset "$1"
	if [ -n "$TOKEN" ]; then
		http_get "$ASSET_API" "$2" application/octet-stream || die "download of $1 failed"
	else
		http_get "$ASSET_URL" "$2" || die "download of $1 failed"
	fi
	[ "$HTTP_STATUS" = 200 ] || die "download of $1 failed: HTTP $HTTP_STATUS"
}

sha256_of() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		die "shasum or sha256sum is needed to check the download"
	fi
}

main() {
	case ${1:-} in
	-h | --help)
		usage
		exit 0
		;;
	"") ;;
	*) die "unknown argument: $1 (try --help)" ;;
	esac

	detect_platform
	if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
		die "curl or wget is needed to download groot"
	fi
	TMP=$(mktemp -d)

	if [ -n "${GROOT_VERSION:-}" ]; then
		release_url=$API/repos/$REPO/releases/tags/v${GROOT_VERSION#v}
	else
		release_url=$API/repos/$REPO/releases/latest
	fi
	http_get "$release_url" "$TMP/release.json" application/vnd.github+json || die "cannot reach $API"
	case $HTTP_STATUS in
	200) ;;
	404)
		if [ -z "$TOKEN" ]; then
			die "$PRIVATE_HINT"
		fi
		die "no release found for $REPO ($release_url)"
		;;
	*) die "release lookup failed: HTTP $HTTP_STATUS ($release_url)" ;;
	esac
	TAG=$(tr -d '\r\n' <"$TMP/release.json" | tr ',' '\n' | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$TAG" ] || die "the release has no tag_name"
	VERSION=${TAG#v}
	ARCHIVE=groot_${VERSION}_${OS}_${ARCH}.tar.gz

	fetch_asset checksums.txt "$TMP/checksums.txt"
	want=$(awk -v n="$ARCHIVE" '{f = $2; sub(/^\*/, "", f)} f == n {print $1; exit}' "$TMP/checksums.txt")
	[ -n "$want" ] || die "checksums.txt has no entry for $ARCHIVE"
	fetch_asset "$ARCHIVE" "$TMP/$ARCHIVE"
	got=$(sha256_of "$TMP/$ARCHIVE")
	if [ "$got" != "$want" ]; then
		rm -f "$TMP/$ARCHIVE"
		die "checksum mismatch for $ARCHIVE (want $want, got $got); nothing was installed"
	fi

	mkdir "$TMP/extract"
	tar -xzf "$TMP/$ARCHIVE" -C "$TMP/extract" groot || die "cannot extract groot from $ARCHIVE"
	[ -f "$TMP/extract/groot" ] || die "$ARCHIVE has no groot binary"

	mkdir -p "$INSTALL_DIR" || die "cannot create $INSTALL_DIR"
	STAGE=$(mktemp "$INSTALL_DIR/.groot.XXXXXX") || die "cannot write to $INSTALL_DIR"
	cp "$TMP/extract/groot" "$STAGE"
	chmod 0755 "$STAGE"
	mv -f "$STAGE" "$INSTALL_DIR/groot"
	STAGE=
	BIN=$INSTALL_DIR/groot
	printf 'installed groot %s to %s\n' "$VERSION" "$BIN"

	case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		printf '\n%s is not in your PATH. Add this line to ~/.zshrc (zsh) or ~/.bashrc (bash):\n' "$INSTALL_DIR"
		# shellcheck disable=SC2016 # $PATH is printed literally for the user's shell.
		printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
		;;
	esac

	if [ -x "$BIN" ]; then
		printf '\n'
		doctor_out=$("$BIN" doctor 2>&1 </dev/null) || true
		printf '%s\n' "$doctor_out"
		case $doctor_out in
		*"missing: run groot auth login"*) printf '\nnext: groot auth login\n' ;;
		esac
	fi
}

trap cleanup EXIT
trap 'exit 1' INT TERM
main "$@"
