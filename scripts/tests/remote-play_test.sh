#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/remote-play-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

(
  cd "$repo"
  mise exec go@1.26.5 -- go build -trimpath -o "$fixture/remote-play-spike" ./cmd/remote-play-spike
)

probe=$($fixture/remote-play-spike probe)
printf '%s\n' "$probe" | grep -q '"physical_capture_required": true'
printf '%s\n' "$probe" | grep -q '"synthetic_frames": false'

if "$fixture/remote-play-spike" sender >/tmp/remote-play-sender.out 2>"$fixture/sender.err"; then
  printf '%s\n' 'sender unexpectedly accepted a missing physical capture device' >&2
  exit 1
fi
grep -q 'physical HDMI capture device' "$fixture/sender.err"

sh -n "$repo/scripts/remote-play/capture-sender.sh" "$repo/scripts/remote-play/receiver.sh"
printf '%s\n' 'remote-play command checks passed'
