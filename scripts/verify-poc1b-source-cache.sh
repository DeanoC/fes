#!/bin/sh
set -eu

[ "$#" -eq 2 ] || {
  printf 'usage: verify-poc1b-source-cache.sh LOCK CACHE\n' >&2
  exit 2
}

lock=$1
cache=$2
test -f "$lock"
cache=$(CDPATH='' cd -- "$cache" && pwd -P)

read_commit() {
  read_section=$1
  awk -v wanted="$read_section" '
    /^\[/ { section=$0; gsub(/^\[|\]$/, "", section); next }
    section == wanted && /^commit[[:space:]]*=/ {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) && substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$lock"
}

verify_repo() {
  verify_section=$1
  verify_directory=$2
  verify_expected=$(read_commit "$verify_section")
  printf '%s\n' "$verify_expected" | grep -Eq '^[0-9a-f]{40}$' || {
    printf 'verify-poc1b-source-cache: invalid %s commit\n' "$verify_section" >&2
    exit 2
  }
  verify_repo_path=$cache/$verify_directory
  test -d "$verify_repo_path/.git" || {
    printf 'verify-poc1b-source-cache: missing Git checkout: %s\n' "$verify_directory" >&2
    exit 1
  }
  verify_actual=$(git -C "$verify_repo_path" rev-parse HEAD)
  test "$verify_actual" = "$verify_expected" || {
    printf 'verify-poc1b-source-cache: %s HEAD is %s, expected %s\n' \
      "$verify_directory" "$verify_actual" "$verify_expected" >&2
    exit 1
  }
  if git -C "$verify_repo_path" symbolic-ref -q HEAD >/dev/null; then
    printf 'verify-poc1b-source-cache: %s HEAD is not detached\n' "$verify_directory" >&2
    exit 1
  fi
  test -z "$(git -C "$verify_repo_path" status --porcelain --untracked-files=all)" || {
    printf 'verify-poc1b-source-cache: %s checkout is dirty\n' "$verify_directory" >&2
    exit 1
  }
}

verify_bare_repo() {
  verify_section=$1
  verify_directory=$2
  verify_expected=$(read_commit "$verify_section")
  printf '%s\n' "$verify_expected" | grep -Eq '^[0-9a-f]{40}$' || {
    printf 'verify-poc1b-source-cache: invalid %s commit\n' "$verify_section" >&2
    exit 2
  }
  verify_repo_path=$cache/$verify_directory.git
  test -f "$verify_repo_path/HEAD" && \
    test "$(git --git-dir="$verify_repo_path" rev-parse --is-bare-repository)" = true || {
      printf 'verify-poc1b-source-cache: missing bare Git cache: %s.git\n' "$verify_directory" >&2
      exit 1
    }
  verify_actual=$(git --git-dir="$verify_repo_path" rev-parse refs/poc1b/pinned)
  test "$verify_actual" = "$verify_expected" || {
    printf 'verify-poc1b-source-cache: %s.git pin is %s, expected %s\n' \
      "$verify_directory" "$verify_actual" "$verify_expected" >&2
    exit 1
  }
  git --git-dir="$verify_repo_path" cat-file -e "$verify_expected^{commit}" || {
    printf 'verify-poc1b-source-cache: %s.git lacks pinned commit object\n' "$verify_directory" >&2
    exit 1
  }
}

verify_repo buildroot buildroot
verify_repo image_creator image-creator
verify_bare_repo kernel linux-kernel
printf '%s\n' 'POC 1B source cache provenance verified'
