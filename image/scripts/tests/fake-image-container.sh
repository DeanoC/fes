#!/bin/sh
# Test-only container metadata for the current immutable builder recipe.
set -eu
if [ "$1" = image ] && [ "${4:-}" = --format ]; then
  case "$3" in
    *@sha256:*) printf '%s\n' "$3" ;;
    fogcast-target-image-build:*)
      suffix=${3#*:}; context=$(printf '%s' "$suffix" | cut -d- -f2)
      digest=$(sed -n "s/^digest = ['\"]\([^'\"]*\)['\"].*/\1/p" "$IMAGE_TEST_REPO/build/target-image.sources.lock.toml" | head -1)
      packages=$(tr -d '[:space:]' < "$IMAGE_TEST_REPO/build/target-image-container-packages.sha256")
      printf 'sha256:%064d|linux/amd64|%s|%s|%s|%s|%s\n' 0 "$digest" "$context" "$packages" "$(id -u)" "$(id -g)"
      ;;
  esac
fi
