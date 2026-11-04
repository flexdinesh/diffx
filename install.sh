#!/bin/sh
# Public stable entrypoint. Installation rules live in scripts/install-common.sh.
set -eu

for tool in curl mktemp rm; do
  command -v "$tool" >/dev/null 2>&1 || {
    printf 'diffx: required tool not found: %s\n' "$tool" >&2
    exit 1
  }
done

helper=$(mktemp "${TMPDIR:-/tmp}/diffx-installer.XXXXXXXXXX")
trap 'rm -f "$helper"' 0
trap 'exit 1' 1 2 3 15
if ! curl --fail --silent --show-error --location \
  --output "$helper" \
  https://raw.githubusercontent.com/flexdinesh/diffx/main/scripts/install-common.sh; then
  printf 'diffx: could not download installer helper; retry.\n' >&2
  exit 1
fi
if [ ! -s "$helper" ]; then
  printf 'diffx: downloaded installer helper is empty; retry.\n' >&2
  exit 1
fi
sh "$helper" stable "$@"
