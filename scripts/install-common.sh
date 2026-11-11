#!/bin/sh
# Implementation for the GitHub raw stable entrypoint, install.sh.
set -eu

fail() {
  printf 'diffx: %s\n' "$*" >&2
  exit 1
}

# install.sh passes the stable channel; keep accepting it so cached entrypoints
# and this helper stay compatible.
[ "${1:-}" = stable ] || fail 'expected stable channel'
shift

usage() {
  printf '%s\n' 'Usage: install.sh [--version vX.Y.Z|X.Y.Z] [--bin-dir DIR]' \
    'Installs the latest stable diffx by default.' \
    'Default destination: ~/.local/bin. Installs only diffx.' \
    'Requires curl, tar, POSIX tools, and sha256sum or shasum.'
}

version=
bin_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --version)
      [ "$#" -ge 2 ] || fail '--version requires vX.Y.Z or X.Y.Z'
      version=${2#v}
      [ -n "$version" ] || fail '--version requires vX.Y.Z or X.Y.Z'
      shift 2
      ;;
    --bin-dir)
      [ "$#" -ge 2 ] && [ -n "$2" ] || fail '--bin-dir requires a directory'
      bin_dir=$2
      shift 2
      ;;
    *) fail "unknown argument: $1 (use --help)" ;;
  esac
done
if [ -z "$bin_dir" ]; then
  [ -n "${HOME:-}" ] || fail 'HOME is unset; provide --bin-dir'
  bin_dir=$HOME/.local/bin
fi

for tool in curl tar uname mkdir mktemp rm awk chmod mv; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool not found: $tool"
done
if command -v sha256sum >/dev/null 2>&1; then
  hash_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  hash_tool=shasum
else
  fail 'required tool not found: sha256sum or shasum'
fi

valid_version() {
  printf '%s\n' "$1" | awk '
    NR == 1 && /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/ { valid = 1 }
    END { exit !(valid && NR == 1) }
  '
}
if [ -n "$version" ]; then
  valid_version "$version" || fail 'invalid version: expected vX.Y.Z or X.Y.Z'
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail 'unsupported operating system: only Linux and Darwin are supported' ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail 'unsupported architecture: only amd64 and arm64 are supported' ;;
esac

releases=https://github.com/flexdinesh/diffx/releases
if [ -n "$version" ]; then
  tag=v$version
else
  # Read only the first redirect: following asset redirects loses the release tag
  # at GitHub's storage CDN. Both downloads below then use this immutable tag.
  resolved=$(curl --fail --silent --show-error --output /dev/null \
    --write-out '%{redirect_url}' "$releases/latest/download/checksums.txt") ||
    fail 'could not resolve latest stable release; retry or use --version'
  case "$resolved" in
    "$releases/download/v"*/checksums.txt)
      tag=${resolved#"$releases/download/"}
      tag=${tag%/checksums.txt}
      version=${tag#v}
      valid_version "$version" || fail 'latest stable release has an invalid version'
      ;;
    *) fail 'could not resolve latest stable release tag; retry or use --version' ;;
  esac
fi
archive=diffx_${version}_${os}_${arch}.tar.gz

case "$bin_dir" in
  /*) ;;
  *) bin_dir=$PWD/$bin_dir ;;
esac
mkdir -p "$bin_dir" || fail "could not create destination: $bin_dir"
[ ! -d "$bin_dir/diffx" ] || fail "destination is a directory: $bin_dir/diffx"
work_dir=$(mktemp -d "$bin_dir/.diffx-install.XXXXXXXXXX") ||
  fail "could not create temporary directory in $bin_dir"
trap 'rm -rf "$work_dir"' 0
trap 'exit 1' 1 2 3 15

download() {
  curl --fail --silent --show-error --location --output "$work_dir/$1" \
    "$releases/download/$tag/$1" || fail "could not download $1; retry"
}
download checksums.txt
download "$archive"

expected=$(awk -v archive="$archive" '
  $2 == archive {
    count++
    if (NF != 2 || length($1) != 64 || $1 !~ /^[[:xdigit:]]+$/) invalid = 1
    hash = tolower($1)
  }
  END {
    if (count != 1 || invalid) exit 1
    print hash
  }
' "$work_dir/checksums.txt") || fail "missing, duplicate or invalid checksum for $archive"
if [ "$hash_tool" = sha256sum ]; then
  digest=$(sha256sum < "$work_dir/$archive") || fail 'could not compute SHA-256'
else
  digest=$(shasum -a 256 < "$work_dir/$archive") || fail 'could not compute SHA-256'
fi
actual=${digest%% *}
[ "$actual" = "$expected" ] || fail 'SHA-256 mismatch; no binary installed. Retry the installer.'

# Extract only the CLI. Release archives also contain diffx-server.
tar -xzf "$work_dir/$archive" -C "$work_dir" diffx || fail 'could not extract diffx'
[ -f "$work_dir/diffx" ] && [ ! -L "$work_dir/diffx" ] ||
  fail 'archive does not contain a regular diffx binary'
chmod 755 "$work_dir/diffx" || fail 'could not make diffx executable'
# Staging inside the destination ensures rename stays on the same filesystem.
mv -f "$work_dir/diffx" "$bin_dir/diffx" || fail 'could not replace diffx'
printf 'Installed diffx %s to %s/diffx\n' "$tag" "$bin_dir"
case ":${PATH:-}:" in
  *":$bin_dir:"*) ;;
  *) printf 'Add %s to your PATH to run diffx.\n' "$bin_dir" ;;
esac
