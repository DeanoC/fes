#!/usr/bin/env bash
set -euo pipefail

root=${1:-$(cd "$(dirname "$0")/.." && pwd)}

require_exact() {
	local file=$1
	local line=$2
	grep -Fx -- "$line" "$file" >/dev/null || {
		printf 'hardware-support-truth: missing from %s: %s\n' "$file" "$line" >&2
		exit 1
	}
}

require_exact "$root/README.md" 'Hardware-supported systems: 1.'
require_exact "$root/README.md" \
	'Mega Drive is software- and hardware-supported on the designated FogCast kit.'
require_exact "$root/docs/support-matrix.md" \
	'| `megadrive` | native fixed-video, one-player launch/Stop/relaunch | software: yes | hardware: yes |'
require_exact "$root/docs/support-matrix.md" 'Hardware-supported systems: 1 (`megadrive`).'
require_exact "$root/docs/support-matrix.md" \
	'Acceptance date: 2026-09-04. Accepted runtime commit:'
require_exact "$root/docs/support-matrix.md" \
	'`443b603de991b56b5f4d0d11c5bc88a3f83fad13`. Exact native image SHA-256:'
require_exact "$root/docs/support-matrix.md" \
	'`95c9b4671e0d19781a6428d2168ab631453740215b194519f12788ade03c7c2e`.'
require_exact "$root/README.md" \
	'[dated FogCast hardware baseline](https://github.com/DeanoC/FogCast/blob/main/docs/hardware/native-megadrive-baseline.md).'
require_exact "$root/docs/support-matrix.md" \
	'The [FogCast native Mega Drive baseline](https://github.com/DeanoC/FogCast/blob/main/docs/hardware/native-megadrive-baseline.md)'

hardware_yes_rows=$(grep -Ec '^\| .* \| hardware: yes \|$' \
	"$root/docs/support-matrix.md" || true)
if [ "$hardware_yes_rows" -ne 1 ]; then
	printf 'hardware-support-truth: expected one hardware-supported row, found %s\n' \
		"$hardware_yes_rows" >&2
	exit 1
fi

for required_scope in \
	'Native audio, save RAM, save states, six-button' \
	'input, multiplayer, remapping, hot-plug recovery, and' \
	'development-RBF loading/video acceptance remain outside this slice.' \
	'every other production system remain unsupported.' \
	'preserve a running game across restart and does not start conventional Main,' \
	'transient MGLs, or an automatic legacy fallback.'
do
	grep -F -- "$required_scope" "$root/README.md" >/dev/null || {
		printf 'hardware-support-truth: README lost unsupported scope: %s\n' \
			"$required_scope" >&2
		exit 1
	}
done

for required_scope in \
	'Audio, saves, six-button input, multiplayer, remapping, hot-plug recovery,' \
	'development-RBF loading/video acceptance, every other' \
	'system, running-game restart preservation, conventional Main, transient MGLs,' \
	'and automatic legacy fallback remain outside this slice.'
do
	grep -F -- "$required_scope" "$root/docs/support-matrix.md" >/dev/null || {
		printf 'hardware-support-truth: support matrix lost unsupported scope: %s\n' \
			"$required_scope" >&2
		exit 1
	}
done

if grep -Eq 'Hardware-supported systems: 0|Mega Drive is software-supported, not hardware-supported|hardware-supported systems remain zero|Physical status remains pending' \
	"$root/README.md" "$root/docs/support-matrix.md"; then
	printf '%s\n' 'hardware-support-truth: stale pre-acceptance claim remains' >&2
	exit 1
fi

printf '%s\n' 'hardware support truth passed'
