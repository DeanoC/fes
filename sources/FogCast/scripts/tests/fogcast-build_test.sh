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

api=$fixture/fogcast-api
make -C "$repo" build-fogcast-api \
  VERSION=9.8.7 \
  FOGCAST_GOOS="$host_os" \
  FOGCAST_GOARCH="$host_arch" \
  FOGCAST_API_OUTPUT="$api"
if ! go version -m "$api" | grep -q "GOOS=$host_os"; then
  echo "fogcast-build: api GOOS is not $host_os" >&2
  go version -m "$api" >&2
  exit 1
fi
if ! go version -m "$api" | grep -q "GOARCH=$host_arch"; then
  echo "fogcast-build: api GOARCH is not $host_arch" >&2
  go version -m "$api" >&2
  exit 1
fi

make -C "$repo" -n build-fogcast-api \
  FOGCAST_GOOS=linux FOGCAST_GOARCH=amd64 FOGCAST_API_OUTPUT="$fixture/linux-api" \
  > "$fixture/api-linux.n"
grep -q 'GOOS=linux GOARCH=amd64' "$fixture/api-linux.n"
if grep -q 'GOOS=darwin GOARCH=arm64' "$fixture/api-linux.n"; then
  echo 'fogcast-build: linux fogcast-api recipe still hard-codes Darwin' >&2
  exit 1
fi

make -C "$repo" build-agent \
  VERSION=9.8.7 \
  REVISION="$expected_revision"
agent=$repo/bin/mister-agent-linux-armv7
if ! strings "$agent" | grep -Fqx "$expected_revision"; then
  echo 'fogcast-build: mister-agent is missing stamped revision' >&2
  exit 1
fi
if go version -m "$agent" | grep -q '[[:space:]]vcs\.'; then
  echo 'fogcast-build: mister-agent automatic Go VCS metadata is present' >&2
  exit 1
fi
