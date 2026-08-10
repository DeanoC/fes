#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-host-build-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

script=$repo/scripts/build-fogcast-host.sh
plist=$repo/resources/fogcast-host/Info.plist

if "$script" "$fixture/FogCastHost.app" >"$fixture/stdout" 2>"$fixture/stderr"; then
	printf '%s\n' 'fogcast-host-build: missing signing identity was accepted' >&2
	exit 1
fi
grep -q 'FOGCAST_SIGNING_IDENTITY' "$fixture/stderr"
if FOGCAST_SIGNING_IDENTITY=- "$script" "$fixture/AdHoc.app" >"$fixture/adhoc-stdout" 2>"$fixture/adhoc-stderr"; then
	printf '%s\n' 'fogcast-host-build: ad-hoc signing identity was accepted' >&2
	exit 1
fi
grep -q 'FOGCAST_SIGNING_IDENTITY' "$fixture/adhoc-stderr"
grep -q 'CFBundleIdentifier' "$plist"
grep -q 'NSCameraUsageDescription' "$plist"
grep -q 'NSMicrophoneUsageDescription' "$plist"
runbook=$repo/docs/runbooks/fogcast-authorized-capture.md
grep -q 'FOGCAST_SIGNING_IDENTITY' "$runbook"
grep -q 'NSCameraUsageDescription' "$runbook"
grep -q 'System Settings' "$runbook"
grep -q 'codesign' "$runbook"
grep -q 'captured_frames' "$runbook"
grep -q 'TCC' "$runbook"

printf '%s\n' 'fogcast host build checks passed'
