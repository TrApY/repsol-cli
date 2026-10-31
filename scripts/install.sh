#!/bin/sh
# Installs the repsol binary from a GitHub release and checks it against the
# release's checksums.txt.
#
#   curl -fsSL https://raw.githubusercontent.com/felipe9962/repsol-cli/main/scripts/install.sh | sh
#
# REPSOL_INSTALL_DIR   where to put the binary (default: ~/.local/bin)
# REPSOL_VERSION       the release tag to install, such as v0.1.0 (default: latest)
# REPSOL_DOWNLOAD_URL  base URL of the release downloads (default: GitHub)
set -eu

repo=felipe9962/repsol-cli
base=${REPSOL_DOWNLOAD_URL:-https://github.com/$repo/releases/download}
install_dir=${REPSOL_INSTALL_DIR:-$HOME/.local/bin}
version=${REPSOL_VERSION:-}

say() { printf 'repsol install: %s\n' "$*" >&2; }
die() {
	say "$*"
	exit 1
}
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported system $(uname -s); download an archive from https://github.com/$repo/releases" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "unsupported architecture $(uname -m); download an archive from https://github.com/$repo/releases" ;;
esac

need curl
need tar
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | awk '{ print $1 }'; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | awk '{ print $1 }'; }
else
	die "sha256sum or shasum is required"
fi

if [ -z "$version" ]; then
	# The latest release page redirects to the page of its tag.
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		die "could not look up the latest release"
	version=${latest##*/}
fi
case $version in
v[0-9]*) ;;
*) die "no release found (got \"$version\"); set REPSOL_VERSION to a tag such as v0.1.0" ;;
esac

archive="repsol_${version#v}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "downloading $archive ($version)"
curl -fsSL -o "$tmp/$archive" "$base/$version/$archive" || die "could not download $base/$version/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/$version/checksums.txt" || die "could not download checksums.txt"

expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || die "$archive is not listed in checksums.txt"
actual=$(sha256 "$tmp/$archive")
[ "$expected" = "$actual" ] || die "checksum mismatch for $archive; nothing was installed"

tar -xzf "$tmp/$archive" -C "$tmp" repsol || die "could not extract repsol from $archive"

mkdir -p "$install_dir"
# Copy next to the target, then rename, so a running repsol is never left
# half-written.
cp "$tmp/repsol" "$install_dir/.repsol.$$"
chmod 0755 "$install_dir/.repsol.$$"
mv -f "$install_dir/.repsol.$$" "$install_dir/repsol"
say "installed $install_dir/repsol ($version)"

case ":$PATH:" in
*":$install_dir:"*) ;;
*) say "$install_dir is not on your PATH; add it to run repsol by name" ;;
esac
