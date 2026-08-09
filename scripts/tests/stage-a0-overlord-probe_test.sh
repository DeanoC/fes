#!/bin/sh
set -eu

repo="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
probe="$repo/scripts/stage-a0-overlord-probe.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-overlord-probe.XXXXXX")
home="$tmp/home"
mkdir -p "$home"

init_repo() {
    dir=$1
    mkdir -p "$dir"
    git -C "$dir" init -q
    git -C "$dir" config user.name stage-a0-probe
    git -C "$dir" config user.email stage-a0-probe@example.invalid
    git -C "$dir" add .
    git -C "$dir" commit -q -m fixture
}

write_lock() {
    printf '%s\n' \
        'format = 1' \
        '[overlord]' \
        "commit = \"$(git -C "$tmp/overlord" rev-parse HEAD)\"" \
        "tree = \"$(git -C "$tmp/overlord" rev-parse 'HEAD^{tree}')\"" \
        '[resources]' \
        "commit = \"$(git -C "$tmp/resources" rev-parse HEAD)\"" \
        "tree = \"$(git -C "$tmp/resources" rev-parse 'HEAD^{tree}')\"" \
        > "$tmp/overlord.lock.toml"
}

mkdir -p "$tmp/overlord/src/main/scala" "$tmp/resources/prefabs"
printf '%s\n' 'package fixture' > "$tmp/overlord/src/main/scala/Main.scala"
printf '%s\n' 'resources = []' > "$tmp/resources/prefabs/boards.toml"
init_repo "$tmp/overlord"
init_repo "$tmp/resources"
write_lock

blocked="$tmp/blocked.json"
set +e
HOME="$home" GIT_CONFIG_NOSYSTEM=1 "$probe" \
    --overlord "$tmp/overlord" \
    --resources "$tmp/resources" \
    --lock "$tmp/overlord.lock.toml" \
    --output "$blocked"
code=$?
set -e
test "$code" -eq 1
grep -Fq '"status":"blocked"' "$blocked"
grep -Fq 'OVERLORD_BOARD_DE10_NANO_MISSING' "$blocked"
grep -Fq 'OVERLORD_SOC_CYCLONE_V_MISSING' "$blocked"

printf '%s\n' 'type: board.de10_nano' > "$tmp/resources/untracked-de10-nano.yaml"
dirty="$tmp/dirty.json"
set +e
HOME="$home" GIT_CONFIG_NOSYSTEM=1 "$probe" \
    --overlord "$tmp/overlord" \
    --resources "$tmp/resources" \
    --lock "$tmp/overlord.lock.toml" \
    --output "$dirty"
code=$?
set -e
test "$code" -eq 2
test ! -e "$dirty"
rm -f "$tmp/resources/untracked-de10-nano.yaml"

mkdir -p "$tmp/resources/boards" "$tmp/resources/socs" "$tmp/resources/registers" "$tmp/resources/toolchains" "$tmp/resources/software"
printf '%s\n' 'type: board.de10_nano' > "$tmp/resources/boards/de10_nano.yaml"
printf '%s\n' 'type: soc.cyclone_v' > "$tmp/resources/socs/cyclone_v.yaml"
printf '%s\n' 'register: []' > "$tmp/resources/registers/cyclone_v.yaml"
printf '%s\n' 'target: arm-none-linux-gnueabihf' > "$tmp/resources/toolchains/arm-none-linux-gnueabihf.yaml"
printf '%s\n' 'program: Main_MiSTer' > "$tmp/resources/software/main_mister.yaml"
git -C "$tmp/resources" add .
git -C "$tmp/resources" commit -q -m complete-fixture
write_lock

ready="$tmp/ready.json"
HOME="$home" GIT_CONFIG_NOSYSTEM=1 "$probe" \
    --overlord "$tmp/overlord" \
    --resources "$tmp/resources" \
    --lock "$tmp/overlord.lock.toml" \
    --output "$ready"
grep -Fq '"status":"ready-for-generation"' "$ready"
grep -Fq '"blockers":[]' "$ready"

printf '%s\n' 'stage-a0-overlord-probe_test: PASS'
