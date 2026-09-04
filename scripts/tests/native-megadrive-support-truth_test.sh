#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
baseline=$root/docs/hardware/native-megadrive-baseline.md

require_exact() {
	file=$1
	line=$2
	grep -Fx -- "$line" "$file" >/dev/null || {
		printf 'native-megadrive-support-truth: missing from %s: %s\n' "$file" "$line" >&2
		exit 1
	}
}

[ -f "$baseline" ] || {
	printf '%s\n' 'native-megadrive-support-truth: hardware baseline is missing' >&2
	exit 1
}

require_exact "$root/README.md" \
	'native-dev = hardware-tested Mega Drive launch/input/Stop/relaunch'
require_exact "$root/docs/ARCHITECTURE.md" \
	'native-dev = hardware-tested Mega Drive launch/input/Stop/relaunch'
require_exact "$root/docs/DEVELOPMENT.md" \
	'native-dev = hardware-tested Mega Drive launch/input/Stop/relaunch'

require_exact "$baseline" 'Acceptance date: 2026-09-04'
require_exact "$baseline" 'Result: PASS'
require_exact "$baseline" '- FogCast source: `889c777f0b7fe6fc662a9d8c7d57834c986239a7`'
require_exact "$baseline" '- `libmister-runtime` source: `443b603de991b56b5f4d0d11c5bc88a3f83fad13`'
require_exact "$baseline" '- Native-dev image SHA-256: `7591ee6a943fabd3e40134f662bb76e242bffa201716ee0d4201a7859d31e480`'
require_exact "$baseline" '- Legacy-dev image SHA-256: `54210721b1419b80d4a15ca415e0a9068a081c1a01b4675dc64eed8f868e7a79`'
require_exact "$baseline" 'Hardware-supported native systems: 1 (`megadrive`).'
require_exact "$baseline" 'Audio, saves, six-button input, multiplayer, remapping, hot-plug recovery,'
require_exact "$baseline" 'development-RBF loading/video, every other system, running-game restart'
require_exact "$baseline" 'preservation, conventional Main, transient MGLs, and automatic legacy fallback'
require_exact "$baseline" 'remain unsupported by the native path.'

for file in "$root/README.md" "$root/docs/ARCHITECTURE.md" "$root/docs/DEVELOPMENT.md"; do
	if grep -Eq 'zero supported game systems|zero supported systems|no native game system is supported|Mega Drive candidate pending acceptance|exact-image hardware acceptance pending' "$file"; then
		printf 'native-megadrive-support-truth: stale pre-acceptance claim in %s\n' "$file" >&2
		exit 1
	fi
done

printf '%s\n' 'native Mega Drive support truth passed'
