#!/bin/sh
# Contributor installer: builds diffx from a branch or commit of the canonical
# repository. The build recipe is the checkout's install:source mise task.
set -eu

repo=https://github.com/flexdinesh/diffx.git
fail() {
  printf 'diffx: %s\n' "$*" >&2
  exit 1
}
usage() {
  printf '%s\n' 'Usage: install-dev.sh [--ref BRANCH|SHA] [--bin-dir DIR] [--clean]' \
    'Builds diffx from a flexdinesh/diffx branch or commit (default: main).' \
    'Default destination: ~/.local/bin. Installs only diffx.' \
    'Reuses a checkout in ${XDG_CACHE_HOME:-~/.cache}/diffx/src; --clean recreates it.' \
    'Requires git and mise (https://mise.jdx.dev/getting-started.html).'
}

ref=main
bin_dir=
clean=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --ref)
      [ "$#" -ge 2 ] && [ -n "$2" ] || fail '--ref requires a branch or commit'
      ref=$2
      shift 2
      ;;
    --bin-dir)
      [ "$#" -ge 2 ] && [ -n "$2" ] || fail '--bin-dir requires a directory'
      bin_dir=$2
      shift 2
      ;;
    --clean) clean=true; shift ;;
    *) fail "unknown argument: $1 (use --help)" ;;
  esac
done

for tool in git mkdir mktemp rm rmdir chmod mv; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool not found: $tool"
done
command -v mise >/dev/null 2>&1 ||
  fail 'mise is required to build from source; install it from https://mise.jdx.dev/getting-started.html'

case "$ref" in
  -*|*@\{*) fail "invalid --ref: $ref" ;;
esac
git check-ref-format --branch "$ref" >/dev/null 2>&1 || fail "invalid --ref: $ref"
is_commit=false
case "$ref" in
  *[!0-9a-fA-F]*) ;;
  *) [ "${#ref}" -lt 7 ] || [ "${#ref}" -gt 40 ] || is_commit=true ;;
esac

if [ -z "$bin_dir" ]; then
  [ -n "${HOME:-}" ] || fail 'HOME is unset; provide --bin-dir'
  bin_dir=$HOME/.local/bin
fi
if [ -n "${XDG_CACHE_HOME:-}" ]; then
  cache=$XDG_CACHE_HOME/diffx
else
  [ -n "${HOME:-}" ] || fail 'HOME is unset; set XDG_CACHE_HOME'
  cache=$HOME/.cache/diffx
fi
case "$bin_dir" in
  /*) ;;
  *) bin_dir=$PWD/$bin_dir ;;
esac
case "$cache" in
  /*) ;;
  *) cache=$PWD/$cache ;;
esac
src=$cache/src
lock=$cache/install-dev.lock

mkdir -p "$bin_dir" || fail "could not create destination: $bin_dir"
[ ! -d "$bin_dir/diffx" ] || fail "destination is a directory: $bin_dir/diffx"
mkdir -p "$cache" || fail "could not create cache: $cache"
# Never remove another installer's lock; a concurrent run leaves its checkout alone.
mkdir "$lock" 2>/dev/null || fail 'another source install is in progress; retry'
work_dir=
cleanup() {
  [ -z "$work_dir" ] || rm -rf "$work_dir"
  rmdir "$lock"
}
trap cleanup 0
trap 'exit 1' 1 2 3 15

if [ "$clean" = true ]; then
  rm -rf "$src" || fail "could not remove checkout cache: $src"
fi
if [ -d "$src/.git" ]; then
  origin=$(git -C "$src" remote get-url origin 2>/dev/null) || origin=
  [ "$origin" = "$repo" ] ||
    fail "checkout cache $src is not a clone of $repo; rerun with --clean"
else
  [ ! -e "$src" ] || fail "checkout cache $src is not a git clone; rerun with --clean"
  git clone --quiet --filter=blob:none --no-checkout "$repo" "$src" ||
    fail "could not clone $repo; retry"
fi
git -C "$src" fetch --quiet --prune origin || fail "could not fetch $repo; retry"
if commit=$(git -C "$src" rev-parse --verify --quiet "refs/remotes/origin/$ref^{commit}"); then
  :
elif [ "$is_commit" = true ] &&
  commit=$(git -C "$src" rev-parse --verify --quiet "$ref^{commit}"); then
  :
else
  fail "unknown ref: $ref (expected a branch or fetched commit of flexdinesh/diffx)"
fi
git -C "$src" checkout --quiet --detach --force "$commit" || fail "could not check out $commit"
git -C "$src" reset --quiet --hard "$commit" || fail "could not reset checkout to $commit"
# Keep ignored dependencies and build outputs; mise content hashes decide rebuilds.
git -C "$src" clean --quiet -fd || fail 'could not clean checkout'
short=$(git -C "$src" rev-parse --short=7 "$commit")
[ -f "$src/.mise/tasks/install/source" ] ||
  fail "$ref ($short) predates install-dev.sh: it has no install:source task"

# Staging inside the destination ensures rename stays on the same filesystem.
work_dir=$(mktemp -d "$bin_dir/.diffx-install.XXXXXXXXXX") ||
  fail "could not create temporary directory in $bin_dir"
(
  cd "$src"
  # Trust only this checkout's mise config, for this run only.
  MISE_TRUSTED_CONFIG_PATHS=$src
  export MISE_TRUSTED_CONFIG_PATHS
  mise install --locked
  mise run install:source -- "$work_dir/diffx"
) || fail "build failed for $ref ($short); existing install preserved"
[ -f "$work_dir/diffx" ] && [ ! -L "$work_dir/diffx" ] ||
  fail 'build did not produce a regular diffx binary'
chmod 755 "$work_dir/diffx" || fail 'could not make diffx executable'
mv -f "$work_dir/diffx" "$bin_dir/diffx" || fail 'could not replace diffx'
printf 'Installed diffx dev %s (%s) to %s/diffx\n' "$short" "$ref" "$bin_dir"
case ":${PATH:-}:" in
  *":$bin_dir:"*) ;;
  *) printf 'Add %s to your PATH to run diffx.\n' "$bin_dir" ;;
esac
