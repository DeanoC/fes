#!/bin/bash
# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: profile_provenance_test.sh ROOT" >&2
	exit 2
fi

root=$1
fixture=$root/tests/fixtures/megadrive-profile-v1.txt
[[ -f "$fixture" ]] || {
	echo "Mega Drive provenance fixture is missing" >&2
	exit 1
}

fail() {
	echo "profile_provenance_test: $1" >&2
	exit 1
}

require_exact() {
	local assignment=$1
	local count
	count=$(grep -Fxc "$assignment" "$fixture" || true)
	[[ "$count" -eq 1 ]] || fail "expected exactly one fixture assignment: $assignment"
}

require_exact 'system=megadrive'
require_exact 'core=MegaDrive'
require_exact 'rbf_size=4296864'
require_exact 'rbf_sha256=0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839'
require_exact 'media_role=cartridge'
require_exact 'media_index=1'
require_exact 'file_wire=little_endian_byte_pairs'
require_exact 'player_count=1'
require_exact 'video_recipe=menu_720p60'
require_exact 'core_source_repository=https://github.com/MiSTer-devel/MegaDrive_MiSTer'
require_exact 'core_source_revision=7365a137cfd8fa6f041e964d8b953159c0ec42d9'
require_exact 'core_source_path=MegaDrive.sv'
require_exact 'rbf_source_path=releases/MegaDrive_20260603.rbf'
require_exact 'main_source_repository=https://github.com/MiSTer-devel/Main_MiSTer'
require_exact 'main_source_revision=c73802332ff9c73659410084b6319ccd29f0b3aa'
require_exact 'main_status_source=user_io.cpp:1427-1433,1515-1530,1676-1679'
require_exact 'main_input_source=user_io.h:16,user_io.cpp:1789-1799'
require_exact 'main_file_wire_source=fpga_io.cpp:553-556,user_io.cpp:1400-1402,2042-2047,spi.cpp:180-193'
require_exact 'core_contract_source=MegaDrive.sv:77-79,132-135,218-220,252-278,307-318,1020-1048'

numeric_keys='reset_assert_word initial_status_word reset_release_word player_command dpad_up_bit dpad_down_bit dpad_left_bit dpad_right_bit button_a_bit button_b_bit button_c_bit button_start_bit'
for key in $numeric_keys; do
	count=$(grep -Ec "^${key}=(0x[0-9A-Fa-f]+|[0-9]+)$" "$fixture" || true)
	[[ "$count" -eq 1 ]] || fail "expected exactly one concrete numeric assignment for $key"
done

expected_keys='button_a_bit button_b_bit button_c_bit button_start_bit core core_contract_source core_source_path core_source_repository core_source_revision dpad_down_bit dpad_left_bit dpad_right_bit dpad_up_bit file_wire initial_status_word main_file_wire_source main_input_source main_source_repository main_source_revision main_status_source media_index media_role player_command player_count rbf_sha256 rbf_size rbf_source_path reset_assert_word reset_release_word system video_recipe'
actual_keys=$(sed -n 's/^\([a-z0-9_]*\)=.*/\1/p' "$fixture" | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')
[[ "$actual_keys" == "$expected_keys" ]] || fail 'fixture keys differ from the immutable v1 schema'

temporary=$(mktemp -d /tmp/libmister-profile-provenance.XXXXXX)
trap 'rm -rf -- "$temporary"' EXIT
core_revision=7365a137cfd8fa6f041e964d8b953159c0ec42d9
main_revision=c73802332ff9c73659410084b6319ccd29f0b3aa
core_base=https://raw.githubusercontent.com/MiSTer-devel/MegaDrive_MiSTer/$core_revision
main_base=https://raw.githubusercontent.com/MiSTer-devel/Main_MiSTer/$main_revision

fetch() {
	local source=$1
	local destination=$2
	curl --fail --silent --show-error --location --retry 3 \
		--connect-timeout 15 --max-time 120 "$source" -o "$destination"
}

fetch "$core_base/releases/MegaDrive_20260603.rbf" "$temporary/MegaDrive_20260603.rbf"
fetch "$core_base/MegaDrive.sv" "$temporary/MegaDrive.sv"
fetch "$main_base/user_io.cpp" "$temporary/user_io.cpp"
fetch "$main_base/user_io.h" "$temporary/user_io.h"
fetch "$main_base/fpga_io.cpp" "$temporary/fpga_io.cpp"
fetch "$main_base/spi.cpp" "$temporary/spi.cpp"

[[ $(wc -c <"$temporary/MegaDrive_20260603.rbf" | tr -d ' ') == 4296864 ]] ||
	fail 'official RBF size does not reproduce the designated-kit identity'
[[ $(sha256sum "$temporary/MegaDrive_20260603.rbf" | cut -d' ' -f1) == \
	0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839 ]] ||
	fail 'official RBF digest does not reproduce the designated-kit identity'
[[ $(sha256sum "$temporary/MegaDrive.sv" | cut -d' ' -f1) == \
	1fe3c4276798cd6cf06b18b5fcb2895792881974396f65ff635e76b8f3cc996a ]] ||
	fail 'pinned MegaDrive.sv content differs'
[[ $(sha256sum "$temporary/user_io.cpp" | cut -d' ' -f1) == \
	939ef10c033ed631f361d88001908382ffa23234ef0d9ef363e8357ea08d7557 ]] ||
	fail 'pinned user_io.cpp content differs'
[[ $(sha256sum "$temporary/user_io.h" | cut -d' ' -f1) == \
	38ff1bea1ed5b883dd258256dca7fed0d2021b46d7e84175710876863e1b7234 ]] ||
	fail 'pinned user_io.h content differs'
[[ $(sha256sum "$temporary/fpga_io.cpp" | cut -d' ' -f1) == \
	3a690b1a5c0c20b0994d44c4853c92c8c4c8698623589928f04a8ca6e333a127 ]] ||
	fail 'pinned fpga_io.cpp content differs'
[[ $(sha256sum "$temporary/spi.cpp" | cut -d' ' -f1) == \
	d5d8ca0fff08b1d5fa7dcb0aa7bb99eaf2fe57a9baaf30d3d6903cc3e5471871 ]] ||
	fail 'pinned spi.cpp content differs'

core_name=$(sed -n '78s/^[[:space:]]*"\([^;]*\);.*/\1/p' "$temporary/MegaDrive.sv")
wide=$(sed -n '252s/.*\.WIDE(\([0-9][0-9]*\)).*/\1/p' "$temporary/MegaDrive.sv")
media_index=$(sed -n '79s/^[[:space:]]*"FS\([0-9][0-9]*\),.*/\1/p' "$temporary/MegaDrive.sv")
reset_assert=$(sed -n '1433s/.*"\[0\]", \([0-9][0-9]*\)).*/\1/p' "$temporary/user_io.cpp")
initial_status=$(sed -n '1530s/.*"\[0\]", \([0-9][0-9]*\)).*/\1/p' "$temporary/user_io.cpp")
reset_release=$(sed -n '1679s/.*"\[0\]", \([0-9][0-9]*\)).*/\1/p' "$temporary/user_io.cpp")
player_command=$(awk '$1 == "#define" && $2 == "UIO_JOYSTICK0" { print $3 }' "$temporary/user_io.h")

bit_mask() {
	local port=$1
	local index
	index=$(sed -n "s/.*\\.P1_${port}(joy0\\[\\([0-9][0-9]*\\)\\]).*/\\1/p" \
		"$temporary/MegaDrive.sv")
	[[ "$index" =~ ^[0-9]+$ ]] || fail "could not derive P1_$port bit from official core source"
	printf '0x%04x' "$((1 << index))"
}

[[ "$core_name" == MegaDrive ]] || fail 'official core name differs'
[[ "$wide" == 1 ]] || fail 'official core is not configured for wide file I/O'
grep -Fq 'return (fpga_gpi_read() >> 16) & 1;' "$temporary/fpga_io.cpp" ||
	fail 'matching Main no longer reads the core wide-file flag'
grep -Fq 'while (len16--) spi_w(*a16++);' "$temporary/spi.cpp" ||
	fail 'matching Main no longer sends wide file data as native byte pairs'
[[ "$media_index" == 1 ]] || fail 'official core cartridge index differs'
grep -Fq 'if(ioctl_index[4:0] == 1) cart_ms <= 0;' "$temporary/MegaDrive.sv" ||
	fail 'official core normal Mega Drive cartridge mode index differs'
[[ "$reset_assert" == 1 && "$initial_status" == 1 && "$reset_release" == 0 ]] ||
	fail 'matching Main reset/status sequence differs'
[[ "$player_command" == 0x02 ]] || fail 'matching Main player-one command differs'

require_exact "core=$core_name"
require_exact "media_index=$media_index"
require_exact 'file_wire=little_endian_byte_pairs'
require_exact "reset_assert_word=$(printf '0x%04x' "$reset_assert")"
require_exact "initial_status_word=$(printf '0x%04x' "$initial_status")"
require_exact "reset_release_word=$(printf '0x%04x' "$reset_release")"
require_exact "player_command=$player_command"
require_exact "dpad_up_bit=$(bit_mask UP)"
require_exact "dpad_down_bit=$(bit_mask DOWN)"
require_exact "dpad_left_bit=$(bit_mask LEFT)"
require_exact "dpad_right_bit=$(bit_mask RIGHT)"
require_exact "button_a_bit=$(bit_mask A)"
require_exact "button_b_bit=$(bit_mask B)"
require_exact "button_c_bit=$(bit_mask C)"
require_exact "button_start_bit=$(bit_mask START)"

combined=0
for key in dpad_up_bit dpad_down_bit dpad_left_bit dpad_right_bit \
	button_a_bit button_b_bit button_c_bit button_start_bit; do
	value=$(sed -n "s/^${key}=//p" "$fixture")
	numeric=$((value))
	[[ "$numeric" -ne 0 ]] || fail "$key must be nonzero"
	[[ $((combined & numeric)) -eq 0 ]] || fail "$key overlaps another supported input bit"
	combined=$((combined | numeric))
done

echo "profile_provenance_test: passed"
