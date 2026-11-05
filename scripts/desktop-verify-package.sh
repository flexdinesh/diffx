#!/bin/sh
# Inspect locally generated archives. Execute informational commands only when
# the target matches the host; platform window acceptance belongs elsewhere.
set -eu
cd "$(dirname "$0")/.."
fail() { printf 'desktop verify: %s\n' "$*" >&2; exit 1; }
version=${DIFFX_RELEASE_VERSION:-0.0.0-dev}
source_sha=${DIFFX_SOURCE_SHA:-$(git rev-parse HEAD)}
host=$(go env GOHOSTOS)/$(go env GOHOSTARCH)
target=${DIFFX_DESKTOP_TARGET:-$host}
out=$PWD/apps/desktop/build/release
archive=diffx-desktop_${version}_${target%/*}_${target#*/}.tar.gz
stage=$(mktemp -d "$out/.verify.XXXXXXXXXX")
trap 'rm -rf "$stage"' 0
trap 'exit 1' 1 2 3 15
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$out" && sha256sum -c "$archive.sha256")
else
  (cd "$out" && shasum -a 256 -c "$archive.sha256")
fi
tar -xzf "$out/$archive" -C "$stage"
case "$target" in
  darwin/*)
    payload=$stage/Diffx\ Desktop\ Experimental.app
    app=$payload/Contents/MacOS/diffx-desktop
    resources=$payload/Contents/Resources
    [ -x "$payload/Contents/MacOS/launch" ] || fail 'missing bundle launcher'
    [ -s "$payload/Contents/Info.plist" ] || fail 'missing bundle metadata'
    ;;
  linux/*)
    resources=$stage/diffx-desktop
    app=$resources/diffx-desktop
    [ -s "$resources/diffx-desktop.desktop" ] || fail 'missing Linux desktop entry'
    ;;
  *) fail 'unsupported target' ;;
esac
sidecar=$resources/diffx
[ -x "$app" ] && [ -x "$sidecar" ] || fail 'missing executable pair'
for notice in LICENSE NOTICE.txt README.txt BUILD-INFO.txt release-licensing.md licenses/github.com_egoist_mygo/LICENSE licenses/github.com_egoist_mygo/internal/shader/LICENSE licenses/go/LICENSE licenses/web/dependencies.json; do
  [ -s "$resources/$notice" ] || fail "missing $notice"
done
node scripts/desktop-assets.mjs "$app" "$sidecar"
go version -m "$app" > "$stage/app-build"
go version -m "$sidecar" > "$stage/server-build"
grep -Fq -- '-tags=mygo_noinspector' "$stage/app-build" || fail 'inspector exclusion tag missing'
grep -Fxq "Version: $version" "$resources/BUILD-INFO.txt" || fail 'version metadata mismatch'
grep -Fxq "Source: $source_sha" "$resources/BUILD-INFO.txt" || fail 'source metadata mismatch'
awk '$1 == "dep" && $2 == "github.com/egoist/mygo" && $3 == "v0.7.4" { found = 1 } END { exit !found }' "$stage/app-build" || fail 'MyGo v0.7.4 pin missing'
for build in "$stage/app-build" "$stage/server-build"; do
  grep -Fq "GOOS=${target%/*}" "$build" || fail 'wrong target OS'
  grep -Fq "GOARCH=${target#*/}" "$build" || fail 'wrong target architecture'
done
if [ "$target" = "$host" ]; then
  [ "$("$app" --version)" = "diffx-desktop $version" ] || fail 'desktop version mismatch'
  "$sidecar" --version > "$stage/server-version"
  awk -v version="$version" 'NR == 1 && $1 == "diffx" && $2 == version { valid = 1 } END { exit !(valid && NR == 1) }' "$stage/server-version" || fail 'private server version mismatch'
  printf 'Verified desktop archive and executable version pair: %s\n' "$target"
else
  printf 'Verified cross-compiled desktop archive: %s (execution requires target host)\n' "$target"
fi
