#!/bin/sh
# Experimental native installer. Normal CLI installation is independent.
set -eu

fail() { printf 'diffx-native: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' 'Usage: install-native.sh [--version edge|vX.Y.Z|X.Y.Z] [--bin-dir DIR]' \
    'Installs experimental diffx-native and its private matching server.' \
    'Default: rolling edge, ~/.local/bin. Existing diffx is untouched.' \
    'Requires curl, tar, POSIX tools, and sha256sum or shasum.'
}
version=edge
bin_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --version)
      [ "$#" -ge 2 ] && [ -n "$2" ] || fail '--version requires edge or vX.Y.Z'
      version=${2#v}; shift 2 ;;
    --bin-dir)
      [ "$#" -ge 2 ] && [ -n "$2" ] || fail '--bin-dir requires a directory'
      bin_dir=$2; shift 2 ;;
    *) fail "unknown argument: $1 (use --help)" ;;
  esac
done
if [ "$version" != edge ]; then
  printf '%s\n' "$version" | awk '
    NR == 1 && /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/ { valid = 1 }
    END { exit !(valid && NR == 1) }
  ' || fail 'invalid version: expected edge or vX.Y.Z'
fi
if [ -z "$bin_dir" ]; then
  [ -n "${HOME:-}" ] || fail 'HOME is unset; provide --bin-dir'
  bin_dir=$HOME/.local/bin
fi
for tool in curl tar uname mkdir mktemp rm awk chmod mv ln readlink; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool not found: $tool"
done
if command -v sha256sum >/dev/null 2>&1; then hash_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then hash_tool=shasum
else fail 'required tool not found: sha256sum or shasum'; fi
case "$(uname -s)" in
  Linux) os=linux; executable=diffx-native/diffx-native ;;
  Darwin) os=darwin; executable='Diffx Native Experimental.app/Contents/MacOS/diffx-native' ;;
  *) fail 'unsupported operating system: only Linux and Darwin are supported' ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail 'unsupported architecture: only amd64 and arm64 are supported' ;;
esac
tag=v$version
[ "$version" != edge ] || tag=edge
archive=diffx-native_${version}_${os}_${arch}.tar.gz
releases=https://github.com/flexdinesh/diffx/releases/download/$tag
case "$bin_dir" in /*) ;; *) bin_dir=$PWD/$bin_dir ;; esac
mkdir -p "$bin_dir" || fail "could not create destination: $bin_dir"
[ ! -d "$bin_dir/diffx-native" ] || fail 'destination is a directory: diffx-native'
# Both the pair and launcher stage on the destination filesystem. Retain old
# pairs so a running process can still start its matching sidecar after upgrade.
versions=$bin_dir/.diffx-native-versions
[ ! -L "$versions" ] || fail 'private versions directory must not be a symlink'
mkdir -p "$versions" || fail 'could not create private versions directory'
work_dir=$(mktemp -d "$versions/install.XXXXXXXXXX") || fail 'could not stage install'
app=$work_dir/payload/$executable
installed=false
cleanup() {
  rm -f "$work_dir/launcher"
  # A signal can arrive after rename but before installed=true. Keep the
  # selected pair in that case, rather than leaving a dangling launcher.
  if [ "$installed" = false ] && [ "$(readlink "$bin_dir/diffx-native" 2>/dev/null || :)" != "$app" ]; then
    rm -rf "$work_dir"
  fi
}
trap cleanup 0
trap 'exit 1' 1 2 3 15
download() {
  curl --fail --silent --show-error --location --output "$work_dir/$1" \
    "$releases/$1" || fail "could not download $1; retry"
}
download native-checksums.txt
download "$archive"
expected=$(awk -v archive="$archive" '
  $2 == archive {
    count++
    if (NF != 2 || length($1) != 64 || $1 !~ /^[[:xdigit:]]+$/) invalid = 1
    hash = tolower($1)
  }
  END { if (count != 1 || invalid) exit 1; print hash }
' "$work_dir/native-checksums.txt") || fail "missing, duplicate or invalid checksum for $archive"
if [ "$hash_tool" = sha256sum ]; then digest=$(sha256sum < "$work_dir/$archive")
else digest=$(shasum -a 256 < "$work_dir/$archive"); fi
[ "${digest%% *}" = "$expected" ] || fail 'SHA-256 mismatch; existing install preserved. Retry.'

# Reject traversal before extraction. Published archives contain only files and
# directories, never links; check types before writing any archive members.
tar -tzf "$work_dir/$archive" > "$work_dir/members" || fail 'could not list archive'
awk '/^\// || /(^|\/)\.\.(\/|$)/ { exit 1 }' "$work_dir/members" || fail 'unsafe archive path'
tar -tvzf "$work_dir/$archive" > "$work_dir/types" || fail 'could not inspect archive'
awk 'substr($0,1,1) != "-" && substr($0,1,1) != "d" { exit 1 }' "$work_dir/types" || fail 'archive links or special files are unsupported'
mkdir "$work_dir/payload"
tar -xzf "$work_dir/$archive" -C "$work_dir/payload" || fail 'could not extract native archive'
if [ "$os" = darwin ]; then
  sidecar=$work_dir/payload/Diffx\ Native\ Experimental.app/Contents/Resources/diffx
else sidecar=$work_dir/payload/diffx-native/diffx; fi
for binary in "$app" "$sidecar"; do
  [ -s "$binary" ] && [ -f "$binary" ] && [ ! -L "$binary" ] || fail 'archive requires regular app and matching private server'
  chmod 755 "$binary" || fail 'could not make native pair executable'
done
# The archive's matching app/server version is established at build time. Do
# not execute downloaded programs during installation.
ln -s "$app" "$work_dir/launcher" || fail 'could not stage native launcher'
# POSIX mv follows a symlink to a directory, rejected above. A file symlink is
# replaced by rename, so readers see either the complete old pair or new pair.
mv -f "$work_dir/launcher" "$bin_dir/diffx-native" || fail 'could not replace native launcher'
installed=true
rm -f "$work_dir/$archive" "$work_dir/native-checksums.txt" "$work_dir/members" "$work_dir/types"
printf 'Installed experimental diffx-native %s to %s/diffx-native\n' "$tag" "$bin_dir"
printf '%s\n' 'Old private pairs are retained for running apps. Stop native apps before removing .diffx-native-versions.'
case ":${PATH:-}:" in *":$bin_dir:"*) ;; *) printf 'Add %s to your PATH to run diffx-native.\n' "$bin_dir" ;; esac
