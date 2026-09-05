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

require_contains() {
	file=$1
	text=$2
	grep -Fq -- "$text" "$file" || {
		printf 'native-megadrive-support-truth: missing from %s: %s\n' "$file" "$text" >&2
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
require_exact "$baseline" '- FogCast source: `cd85971bf0bffe36e69c381917f618620b901726`'
require_exact "$baseline" '- `libmister-runtime` source: `443b603de991b56b5f4d0d11c5bc88a3f83fad13`'
require_exact "$baseline" '- Native-dev image SHA-256: `95c9b4671e0d19781a6428d2168ab631453740215b194519f12788ade03c7c2e`'
require_exact "$baseline" '- Legacy-dev image SHA-256: `91f9870a1d2988ffec3e9cda22dea7f729633bbfc26bdd6c35f5776b4abff055`'
require_exact "$baseline" 'Hardware-supported native systems: 1 (`megadrive`).'
require_exact "$baseline" 'Audio, saves, six-button input, multiplayer, remapping, hot-plug recovery,'
require_exact "$baseline" 'development-RBF loading/video, every other system, running-game restart'
require_exact "$baseline" 'preservation, conventional Main, transient MGLs, and automatic legacy fallback'
require_exact "$baseline" 'remain unsupported by the native path.'

for file in "$root/README.md" "$root/docs/ARCHITECTURE.md" "$root/docs/DEVELOPMENT.md"; do
	require_contains "$file" \
		'Source-built Mega Drive selection is the native image default; use the explicit upstream selection for fallback.'
	require_contains "$file" \
		'make target-image-native MEGADRIVE_RBF_BUNDLE=/absolute/sealed/bundle'
	require_contains "$file" \
		'make target-image-native MEGADRIVE_RBF_SOURCE=upstream'
	require_contains "$file" \
		'There is no automatic fallback between the two RBF selections.'
	require_contains "$file" \
		'Both selections use the MiSTer ABI; generalized/custom/non-MiSTer RBF ABI support is deferred.'
	if grep -Eq 'zero supported game systems|zero supported systems|no native game system is supported|Mega Drive candidate pending acceptance|exact-image hardware acceptance pending' "$file"; then
		printf 'native-megadrive-support-truth: stale pre-acceptance claim in %s\n' "$file" >&2
		exit 1
	fi
	if grep -Eiq '(generalized|custom|non-MiSTer)[^[:cntrl:].]{0,80}(is|are)?[[:space:]]+(supported|available|works)' "$file"; then
		printf 'native-megadrive-support-truth: unsupported generalized/custom claim in %s\n' "$file" >&2
		exit 1
	fi
done

printf '%s\n' 'native Mega Drive support truth passed'
