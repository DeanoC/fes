#!/usr/bin/env bash
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

manifest=docs/provenance/main-mister-346ba9f.sha256
recorded_tip=$(tr -d '\r\n' <docs/provenance/imported-tip)
test -n "$recorded_tip"
git rev-parse --verify --quiet "$recorded_tip^{commit}" >/dev/null
git merge-base --is-ancestor "$recorded_tip" HEAD

allowlist=$(mktemp)
trap 'rm -f "$allowlist"' EXIT
cut -d' ' -f3- "$manifest" >"$allowlist"
test -s "$allowlist"
LC_ALL=C sort -cu "$allowlist"

while IFS= read -r commit
do
  unexpected=$(git ls-tree -r --name-only "$commit" |
    LC_ALL=C sort -u |
    comm -23 - "$allowlist")
  if test -n "$unexpected"
  then
    printf 'unexpected path in imported history at %s:\n%s\n' "$commit" "$unexpected" >&2
    exit 1
  fi
done < <(git rev-list "$recorded_tip")

if ! git ls-tree -r --name-only "$recorded_tip" | cmp "$allowlist" -
then
  printf 'imported tip tree does not exactly match the recorded manifest paths\n' >&2
  exit 1
fi

while IFS=' ' read -r digest path
do
  actual=$(git cat-file blob "$recorded_tip:$path" | sha256sum | cut -d' ' -f1)
  if test "$actual" != "$digest"
  then
    printf 'manifest digest mismatch for %s\n' "$path" >&2
    exit 1
  fi
done <"$manifest"

printf 'history and provenance checks passed\n'
