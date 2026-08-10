#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-host-build-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

script=$repo/scripts/build-fogcast-host.sh
plist=$repo/resources/fogcast-host/Info.plist
entitlements=$repo/resources/fogcast-host/Entitlements.plist

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
grep -q 'com.apple.security.device.camera' "$entitlements"
runbook=$repo/docs/runbooks/fogcast-authorized-capture.md
grep -q 'FOGCAST_SIGNING_IDENTITY' "$runbook"
grep -q 'NSCameraUsageDescription' "$runbook"
grep -q 'System Settings' "$runbook"
grep -q 'codesign' "$runbook"
grep -q 'captured_frames' "$runbook"
grep -q 'TCC' "$runbook"

if [ "$(uname -s)" = Darwin ]; then
	fake_bin=$fixture/fake-bin
	mkdir -p "$fake_bin"
	cat >"$fake_bin/codesign" <<'EOF'
#!/bin/sh
set -eu
entitlements=
app=
while [ "$#" -gt 0 ]; do
	case "$1" in
		--entitlements) entitlements=$2; shift 2 ;;
		*.app) app=$1; shift ;;
		*) shift ;;
	esac
done
test -n "$entitlements"
test -f "$entitlements"
grep -q 'com.apple.security.device.camera' "$entitlements"
test -d "$app/Contents/MacOS"
test -x "$app/Contents/MacOS/fogcast-api"
touch "$app/Contents/.fake-signed"
EOF
	chmod 755 "$fake_bin/codesign"
	output=$fixture/Signed.app
	PATH="$fake_bin:$PATH" FOGCAST_SIGNING_IDENTITY=TestIdentity "$script" "$output"
	test -x "$output/Contents/MacOS/fogcast-api"
	test -f "$output/Contents/.fake-signed"
fi

printf '%s\n' 'fogcast host build checks passed'
