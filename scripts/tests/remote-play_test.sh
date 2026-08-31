#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/remote-play-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

(
  cd "$repo"
  go build -trimpath -o "$fixture/remote-play-sender" ./cmd/remote-play-sender
  go build -trimpath -o "$fixture/remote-play-receiver" ./cmd/remote-play-receiver
  go build -trimpath -o "$fixture/remote-play-impair" ./cmd/remote-play-impair
)

probe=$($fixture/remote-play-sender probe)
printf '%s\n' "$probe" | grep -q '"physical_capture_required": true'
printf '%s\n' "$probe" | grep -q '"synthetic_frames": false'

if "$fixture/remote-play-sender" sender >"$fixture/sender.out" 2>"$fixture/sender.err"; then
  printf '%s\n' 'sender unexpectedly accepted a missing physical capture device' >&2
  exit 1
fi
grep -q 'physical HDMI capture device' "$fixture/sender.err"

sh -n "$repo/scripts/remote-play/capture-sender.sh" "$repo/scripts/remote-play/receiver.sh" "$repo/scripts/remote-play/impair.sh"
if "$fixture/remote-play-receiver" >/dev/null 2>"$fixture/receiver.err"; then
  printf '%s\n' 'receiver unexpectedly accepted missing required session configuration' >&2
  exit 1
fi
grep -q -- '--session' "$fixture/receiver.err"
if "$fixture/remote-play-impair" --drop-every 1 >/dev/null 2>"$fixture/impair.err"; then
  printf '%s\n' 'impairment harness unexpectedly accepted --drop-every 1' >&2
  exit 1
fi
grep -q -- '--drop-every' "$fixture/impair.err"
printf '%s\n' 'remote-play command checks passed'
