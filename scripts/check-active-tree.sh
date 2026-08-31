#!/bin/bash
# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

root=${1:-"$(cd "$(dirname "$0")/.." && pwd -P)"}

for active_path in fogcast runtime support lib releases; do
	if [[ -e "$root/$active_path" ]]; then
		echo "obsolete root active path exists: $active_path" >&2
		exit 1
	fi
done

historic_pattern='stage[-_ ]?c0|poc[0-9]*|fogcast-runtime|native[-_ ]personality|mister_runtime_linux_v2|NativeLinuxV2|HardwareBroker|OperationLease|CapabilityBundle|AuthorityView|BackendFence|ReplayTracker|native_recovery|native_containment|native_peripheral_session|native_resources|native_audio|native_av_io|native_video|native_input|native_save|native_scheduler|native_offload|native_snes|v2'

if grep -ERni --include='*.[ch]' --include='*.cpp' --include='*.hpp' \
	--include='Makefile' "$historic_pattern" \
	"$root/include" "$root/src" "$root/Makefile" >/tmp/libmister-active-tree.$$.log; then
	echo "historic compatibility term remains in the active tree" >&2
	cat /tmp/libmister-active-tree.$$.log >&2
	rm -f -- /tmp/libmister-active-tree.$$.log
	exit 1
fi
rm -f -- /tmp/libmister-active-tree.$$.log
