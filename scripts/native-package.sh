#!/bin/sh
# Local packaging entry. Release callers enforce clean source in the mise task.
set -eu
cd "$(dirname "$0")/.."
fail() { printf 'native package: %s\n' "$*" >&2; exit 1; }
version=${DIFFX_RELEASE_VERSION:-0.0.0-dev}
source_sha=${DIFFX_SOURCE_SHA:-$(git rev-parse HEAD)}
printf '%s\n' "$version" | awk '
  NR == 1 && /^(0\.0\.0-dev|edge|[0-9]+\.[0-9]+\.[0-9]+)$/ { valid = 1 }
  END { exit !(valid && NR == 1) }
' || fail 'invalid version'
[ "$(git rev-parse HEAD)" = "$source_sha" ] || fail 'checkout does not match source'
target=${DIFFX_NATIVE_TARGET:-$(go env GOHOSTOS)/$(go env GOHOSTARCH)}
case "$target" in linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;; *) fail 'unsupported native target';; esac
export GOOS="${target%/*}" GOARCH="${target#*/}" CGO_ENABLED=0
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-mod=readonly"
out=$PWD/apps/desktop/build/release
mkdir -p "$out"
stage=$(mktemp -d "$out/.package.XXXXXXXXXX")
trap 'rm -rf "$stage"' 0
trap 'exit 1' 1 2 3 15
if [ "$GOOS" = darwin ]; then
  payload=$stage/Diffx\ Native\ Experimental.app
  app_dir=$payload/Contents/MacOS
  resources=$payload/Contents/Resources
  mkdir -p "$app_dir" "$resources"
  # Bundle versions must be numeric even for a rolling edge build.
  bundle_version=$version
  case "$version" in 0.0.0-dev|edge) bundle_version=0.0.0 ;; esac
  sed "s/@VERSION@/$bundle_version/g" apps/desktop/resources/Info.plist > "$payload/Contents/Info.plist"
  cp apps/desktop/resources/launch "$app_dir/launch"
  chmod 755 "$app_dir/launch"
else
  payload=$stage/diffx-native
  app_dir=$payload
  resources=$payload
  mkdir -p "$payload"
  cp apps/desktop/resources/diffx-native.desktop "$payload/"
fi
go -C apps/desktop list -deps -tags mygo_noinspector \
  -f '{{.ImportPath}}' ./cmd/diffx-native > "$stage/dependencies"
if awk '/github.com\/egoist\/mygo\/plugins\/automation/ { found = 1 } END { exit !found }' "$stage/dependencies"; then
  fail 'automation must never ship in native releases'
fi
go -C apps/desktop build -trimpath -tags mygo_noinspector \
  -ldflags "-s -w -X github.com/flexdinesh/diffx/apps/desktop/internal/app.Version=$version" \
  -o "$app_dir/diffx-native" ./cmd/diffx-native
build_date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
go build -trimpath -ldflags "-s -w -X github.com/flexdinesh/diffx/internal/version.Version=$version -X github.com/flexdinesh/diffx/internal/version.Commit=$source_sha -X github.com/flexdinesh/diffx/internal/version.Date=$build_date" \
  -o "$resources/diffx" ./cmd/diffx
chmod 755 "$app_dir/diffx-native" "$resources/diffx"
cp LICENSE "$resources/LICENSE"
cp apps/desktop/resources/README.txt apps/desktop/resources/NOTICE.txt "$resources/"
cp docs/release.md "$resources/release-licensing.md"
{
  printf 'Version: %s\nSource: %s\nTarget: %s\nBuilt: %s\n' "$version" "$source_sha" "$target" "$build_date"
  git status --porcelain
  go version -m "$app_dir/diffx-native"
  go version -m "$resources/diffx"
} > "$resources/BUILD-INFO.txt"

# Collect notices from the modules actually linked into each executable. Retain
# nested notices too, including MyGo's shader and font sources where supplied.
module_template='{{if .Module}}{{.Module.Path}}|{{.Module.Dir}}{{end}}'
go -C apps/desktop list -deps -tags mygo_noinspector -f "$module_template" ./cmd/diffx-native > "$stage/modules"
go list -deps -f "$module_template" ./cmd/diffx >> "$stage/modules"
sort -u "$stage/modules" > "$stage/unique-modules"
mkdir -p "$resources/licenses"
while IFS='|' read -r module directory; do
  case "$module" in ''|github.com/flexdinesh/diffx|github.com/flexdinesh/diffx/apps/desktop) continue ;; esac
  name=$(printf '%s' "$module" | tr '/' '_')
  find "$directory" -type f \( -iname 'license*' -o -iname 'notice*' -o -iname 'copying*' -o -iname 'ofl*' -o -iname 'copyright*' -o -iname 'patents*' \) > "$stage/license-files"
  [ -s "$stage/license-files" ] || fail "no license found for $module"
  while IFS= read -r license; do
    relative=${license#"$directory"/}
    destination=$resources/licenses/$name/$relative
    mkdir -p "$(dirname "$destination")"
    cp "$license" "$destination"
  done < "$stage/license-files"
done < "$stage/unique-modules"
mkdir -p "$resources/licenses/go" "$resources/licenses/themes"
cp "$(go env GOROOT)/LICENSE" "$(go env GOROOT)/PATENTS" "$resources/licenses/go/"
cp tools/native-tokens/themes/LICENSE-* "$resources/licenses/themes/"
archive=diffx-native_${version}_${GOOS}_${GOARCH}.tar.gz
tar -czf "$stage/$archive" -C "$stage" "${payload##*/}"
if command -v sha256sum >/dev/null 2>&1; then digest=$(sha256sum < "$stage/$archive")
else digest=$(shasum -a 256 < "$stage/$archive"); fi
printf '%s  %s\n' "${digest%% *}" "$archive" > "$stage/$archive.sha256"
mv -f "$stage/$archive" "$out/$archive"
mv -f "$stage/$archive.sha256" "$out/$archive.sha256"
printf 'Packaged %s/%s\n' "$out" "$archive"
