#!/usr/bin/env bash
# Print a commit holding the first parent's tree plus staged embedded web assets.
# Usage: asset-commit.sh MESSAGE BASE_COMMIT [EXTRA_PARENT...]

set -euo pipefail
message="${1:?message is required}"
base="${2:?base commit is required}"
shift

assets=internal/webui/dist
if [[ ! -f "$assets/index.html" ]]; then
  echo "$assets is not staged; run mise run web:stage" >&2
  exit 1
fi

index_dir="$(mktemp -d)"
trap 'rm -rf "$index_dir"' EXIT
export GIT_INDEX_FILE="$index_dir/index"
git read-tree "$base"
git add --force -- "$assets"
tree="$(git write-tree)"

parents=()
for parent in "$@"; do
  parents+=(-p "$(git rev-parse --verify "$parent^{commit}")")
done
git commit-tree "$tree" "${parents[@]}" -m "$message"
