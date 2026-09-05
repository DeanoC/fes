#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
baseline=$root/docs/hardware/native-development-rbf-baseline.md

fail() {
	printf 'native-development-rbf-support-truth: %s\n' "$1" >&2
	exit 1
}

require_exact() {
	file=$1
	line=$2
	grep -Fx -- "$line" "$file" >/dev/null || fail "missing from $file: $line"
}

paragraphs() {
	awk 'BEGIN { RS = ""; ORS = "\n" } { gsub(/[[:space:]]+/, " "); print }' "$1"
}

[ -f "$baseline" ] || fail 'hardware baseline is missing'

require_exact "$baseline" 'Acceptance date: 2026-09-04'
require_exact "$baseline" 'Result: PASS'
require_exact "$baseline" 'Hardware-tested: yes'
require_exact "$baseline" 'Accepted consecutive cycles: 2'
require_exact "$baseline" '- FogCast source: `1fc2ef075b3d80ede1a25592b2155ccc256a56af`'
require_exact "$baseline" '- `libmister-runtime` source: `4398f41bf504329e5c9b21f916cb37952bfb4cc7`'
require_exact "$baseline" '- Native-dev image SHA-256: `2bcfea7ef52dafa718edba4cfc15bb7ffbc2c9e83a6790be7e8d840dc008d730`'
require_exact "$baseline" '- Legacy-prod image SHA-256: `078ba05de5acf39a182d86912673bbec1ad73f50aa2e4191100eff025129ff56`'
require_exact "$baseline" '- Legacy-dev image SHA-256: `c6a025f55d79df2af9e9c6ab0503b212ebf9dd3ec1ab3360cb300a64a023c4a7`'
require_exact "$baseline" 'Packaged development RBF: no'
require_exact "$baseline" 'Generic development video/input guarantee: no'
require_exact "$baseline" 'Browser file picker: no'
require_exact "$baseline" 'Generalized RBF ABI: no'
require_exact "$baseline" 'Final target state: exact legacy-dev image, visible Menu, capture/input/helpers released'

for file in "$root/README.md" "$root/docs/ARCHITECTURE.md" "$root/docs/DEVELOPMENT.md"; do
	require_exact "$file" 'native development RBF = hardware-tested MiSTer-compatible load/Stop/game-regression path'
	if paragraphs "$file" | grep -Eiq \
		'native[[:space:]]+development(-RBF)?[^.]*(pending|awaits)'; then
		fail "stale pending-acceptance claim in $file"
	fi
	if grep -Eiq 'native[^.]*((generic|arbitrary|non-MiSTer)[^.]*RBF|guarantee[^.]*(video|HDMI|input))' "$file"; then
		fail "generic native ABI/video/input claim in $file"
	fi
done

require_exact "$root/docs/superpowers/specs/2026-09-04-native-development-rbf-design.md" \
	'**Status:** Implemented and hardware-tested for the MiSTer-compatible development ABI'

for path in \
	buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/mister-runtime/development.rbf \
	buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/mister-runtime/cores/development.rbf \
	buildroot/board/fogcast-target/native-rootfs-overlay/tmp/fogcast-development/core.rbf; do
	[ ! -e "$root/$path" ] || fail "packaged development artifact: $path"
done

if grep -R -n -E --exclude=native-development-rbf-support-truth_test.sh \
	'/usr/share/mister-runtime/(cores/)?development\.rbf' \
	"$root/buildroot" "$root/scripts" 2>/dev/null | grep -vE 'reject|absence|forbid|must not|test' >/dev/null; then
	fail 'production packaging references a development RBF'
fi

printf '%s\n' 'native development RBF support truth passed'
