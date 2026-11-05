#!/bin/sh
# Experimental desktop/CLI pair installer. CLI-only installation is independent.
set -eu

fail() { printf 'diffx-desktop: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' 'Usage: install-desktop.sh [--version edge|vX.Y.Z|X.Y.Z] [--bin-dir DIR]' \
    'Installs experimental diffx-desktop and matching diffx commands.' \
    'Default: rolling edge, ~/.local/bin. Replaces existing command aliases.' \
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
for tool in curl tar uname mkdir mktemp rm rmdir awk chmod cp mv ln readlink; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool not found: $tool"
done
if command -v sha256sum >/dev/null 2>&1; then hash_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then hash_tool=shasum
else fail 'required tool not found: sha256sum or shasum'; fi
case "$(uname -s)" in
  Linux) os=linux; executable=diffx-desktop/diffx-desktop ;;
  Darwin) os=darwin; executable='Diffx Desktop Experimental.app/Contents/MacOS/diffx-desktop' ;;
  *) fail 'unsupported operating system: only Linux and Darwin are supported' ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail 'unsupported architecture: only amd64 and arm64 are supported' ;;
esac
if [ "$version" = edge ] && [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
  fail 'edge does not support macOS Intel; use --version vX.Y.Z'
fi
tag=v$version
[ "$version" != edge ] || tag=edge
archive=diffx-desktop_${version}_${os}_${arch}.tar.gz
releases=https://github.com/flexdinesh/diffx/releases/download/$tag
case "$bin_dir" in /*) ;; *) bin_dir=$PWD/$bin_dir ;; esac
mkdir -p "$bin_dir" || fail "could not create destination: $bin_dir"
for command in diffx diffx-desktop; do
  [ ! -d "$bin_dir/$command" ] || fail "destination is a directory: $command"
done
# Both the pair and launchers stage on the destination filesystem. Retain old
# pairs so a running process can still start its matching sidecar after upgrade.
versions=$bin_dir/.diffx-desktop-versions
[ ! -L "$versions" ] || fail 'private versions directory must not be a symlink'
mkdir -p "$versions" || fail 'could not create private versions directory'
chmod 700 "$versions" || fail 'could not secure private versions directory'
[ -L "$versions/current" ] || [ ! -e "$versions/current" ] || fail 'active installation must be a symlink'
# Serialize migrations as well as pointer switches. Never remove another
# installer's lock; a failed concurrent invocation leaves the active pair alone.
mkdir "$versions/.install-lock" 2>/dev/null || fail 'another desktop install is in progress; retry'
work_dir=
installed=false
changed_cli=false
changed_desktop=false
cleanup() {
  if [ -n "$work_dir" ]; then
    # A signal can arrive after activation but before installed=true.
    if [ "$(readlink "$versions/current" 2>/dev/null || :)" = "${work_dir##*/}" ]; then
      installed=true
    fi
    if [ "$installed" = false ]; then
      restored=true
      for command in diffx diffx-desktop; do
        case "$command" in
          diffx) changed=$changed_cli ;;
          diffx-desktop) changed=$changed_desktop ;;
        esac
        [ "$changed" = true ] || continue
        if [ -e "$work_dir/previous/$command" ] || [ -L "$work_dir/previous/$command" ]; then
          mv -f "$work_dir/previous/$command" "$bin_dir/$command" || restored=false
        else
          rm -f "$bin_dir/$command" || restored=false
        fi
      done
      if [ "$restored" = true ]; then rm -rf "$work_dir"
      else printf 'diffx-desktop: command recovery required; backups retained in %s/previous\n' "$work_dir" >&2; fi
    else
      rm -f "$work_dir/active" "$work_dir/alias-diffx" "$work_dir/alias-diffx-desktop" \
        "$work_dir/$archive" "$work_dir/desktop-checksums.txt" "$work_dir/members" "$work_dir/types"
    fi
  fi
  rmdir "$versions/.install-lock"
}
trap cleanup 0
trap 'exit 1' 1 2 3 15
work_dir=$(mktemp -d "$versions/install.XXXXXXXXXX") || fail 'could not stage install'
app=$work_dir/payload/$executable
download() {
  curl --fail --silent --show-error --location --output "$work_dir/$1" \
    "$releases/$1" || fail "could not download $1; retry"
}
download desktop-checksums.txt
download "$archive"
expected=$(awk -v archive="$archive" '
  $2 == archive {
    count++
    if (NF != 2 || length($1) != 64 || $1 !~ /^[[:xdigit:]]+$/) invalid = 1
    hash = tolower($1)
  }
  END { if (count != 1 || invalid) exit 1; print hash }
' "$work_dir/desktop-checksums.txt") || fail "missing, duplicate or invalid checksum for $archive"
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
tar -xzf "$work_dir/$archive" -C "$work_dir/payload" || fail 'could not extract desktop archive'
if [ "$os" = darwin ]; then
  sidecar=$work_dir/payload/Diffx\ Desktop\ Experimental.app/Contents/Resources/diffx
else sidecar=$work_dir/payload/diffx-desktop/diffx; fi
for binary in "$app" "$sidecar"; do
  [ -s "$binary" ] && [ -f "$binary" ] && [ ! -L "$binary" ] || fail 'archive requires regular app and matching private server'
  chmod 755 "$binary" || fail 'could not make desktop pair executable'
done
# The archive's matching app/CLI version is established at build time. Do
# not execute downloaded programs during installation.
mkdir "$work_dir/bin" "$work_dir/previous"
ln -s "../payload/$executable" "$work_dir/bin/diffx-desktop"
ln -s "../${sidecar#"$work_dir"/}" "$work_dir/bin/diffx"
ln -s "${work_dir##*/}" "$work_dir/active" || fail 'could not stage active installation'
for command in diffx diffx-desktop; do
  alias_target=.diffx-desktop-versions/current/bin/$command
  [ "$(readlink "$bin_dir/$command" 2>/dev/null || :)" != "$alias_target" ] || continue
  # Preserve standalone CLIs and legacy desktop-only links, including dangling
  # links. These backups also make a partially completed migration reversible.
  if [ -e "$bin_dir/$command" ] || [ -L "$bin_dir/$command" ]; then
    cp -P "$bin_dir/$command" "$work_dir/previous/$command" || fail "could not preserve prior $command"
  fi
  ln -s "$alias_target" "$work_dir/alias-$command" || fail "could not stage $command alias"
  case "$command" in diffx) changed_cli=true ;; diffx-desktop) changed_desktop=true ;; esac
  mv -f "$work_dir/alias-$command" "$bin_dir/$command" || fail "could not replace $command alias"
done
# Both aliases stay fixed on subsequent upgrades. Rename the single directory
# symlink itself, never follow it and move a file into the old installation.
case "$os" in
  linux) mv -fT "$work_dir/active" "$versions/current" ;;
  darwin) mv -fh "$work_dir/active" "$versions/current" ;;
esac || fail 'could not activate desktop pair; existing install preserved'
installed=true
printf 'Installed experimental diffx-desktop and matching diffx %s to %s\n' "$tag" "$bin_dir"
printf '%s\n' 'Old private pairs are retained for running apps. Stop desktop apps before removing .diffx-desktop-versions.'
case ":${PATH:-}:" in *":$bin_dir:"*) ;; *) printf 'Add %s to your PATH to run diffx and diffx-desktop.\n' "$bin_dir" ;; esac
