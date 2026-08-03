#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-build-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

host_os=$(go env GOOS)
host_arch=$(go env GOARCH)
expected_revision=$(git -C "$repo" rev-parse --verify HEAD)
normal=$fixture/fogcast-normal
override=$fixture/fogcast-override

make -C "$repo" build-fogcast \
  VERSION=9.8.7 \
  FOGCAST_GOOS="$host_os" \
  FOGCAST_GOARCH="$host_arch" \
  FOGCAST_OUTPUT="$normal"

expected="fogcast version=9.8.7 revision=$expected_revision"
actual=$($normal --version)
if [ "$actual" != "$expected" ]; then
  echo "fogcast-build: normal metadata mismatch: got '$actual', want '$expected'" >&2
  exit 1
fi
if go version -m "$normal" | grep -q '[[:space:]]vcs\.'; then
  echo 'fogcast-build: automatic Go VCS metadata is present' >&2
  exit 1
fi

make -C "$repo" build-fogcast \
  VERSION=1.2.3 \
  REVISION=override-revision \
  FOGCAST_GOOS="$host_os" \
  FOGCAST_GOARCH="$host_arch" \
  FOGCAST_OUTPUT="$override"

expected='fogcast version=1.2.3 revision=override-revision'
actual=$($override --version)
if [ "$actual" != "$expected" ]; then
  echo "fogcast-build: override metadata mismatch: got '$actual', want '$expected'" >&2
  exit 1
fi
