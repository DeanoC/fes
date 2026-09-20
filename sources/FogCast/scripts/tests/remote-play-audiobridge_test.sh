#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-audiobridge-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

CGO_ENABLED=0 mise exec go@1.26.5 -- go test ./cmd/remote-play-audiobridge -count=1

CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  mise exec go@1.26.5 -- go build -buildvcs=false -trimpath \
  -o "$fixture/remote-play-audiobridge-linux-armv7" ./cmd/remote-play-audiobridge

test -s "$fixture/remote-play-audiobridge-linux-armv7"
if grep -nE 'cast/start|mister-agent|/dev/fb0|/dev/MiSTer_cmd|alsa|ffmpeg' \
  "$repo/cmd/remote-play-audiobridge/main.go"; then
  echo 'remote-play-audiobridge-test: diagnostic bridge contains a forbidden public/physical sink seam' >&2
  exit 1
fi

grep -q -- 'flags.String("token-file"' "$repo/cmd/remote-play-audiobridge/main.go"
if grep -q -- 'flags.String("token"' "$repo/cmd/remote-play-audiobridge/main.go"; then
  echo 'remote-play-audiobridge-test: token must be file-backed, not a process argument' >&2
  exit 1
fi
grep -q -- 'audio-device' "$repo/cmd/remote-play-audiobridge/main.go"
grep -q -- 'receiver.AcceptControl' "$repo/cmd/remote-play-audiobridge/main.go"
