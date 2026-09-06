#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-native-runtime-smoke.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

smoke=$repo/scripts/native-runtime-smoke.sh
[ -x "$smoke" ] || {
  printf '%s\n' 'native runtime smoke runner is missing' >&2
  exit 1
}

fake_bin=$fixture/bin
mkdir -p "$fake_bin"

cat > "$fake_bin/curl" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "$*" >> "$FOGCAST_FAKE_CURL_LOG"
url=
for argument do
  url=$argument
done

case "$url" in
  */api/v1/session/stop)
    count=0
    [ ! -f "$FOGCAST_FAKE_STOP_COUNT" ] || count=$(cat "$FOGCAST_FAKE_STOP_COUNT")
    count=$((count + 1))
    printf '%s\n' "$count" > "$FOGCAST_FAKE_STOP_COUNT"
    [ "$FOGCAST_FAKE_MODE" != stop-request-error ] || exit 22
    printf '%s\n' '{"state":"idle","private":"STOP-RESPONSE-SECRET"}'
    ;;
  */v1/health)
    if [ -f "$FOGCAST_FAKE_REBOOTED" ]; then
      boot_id=22222222-2222-4222-8222-222222222222
      [ "$FOGCAST_FAKE_MODE" != unchanged-boot ] || \
        boot_id=11111111-1111-4111-8111-111111111111
      if [ "$FOGCAST_FAKE_MODE" = never-ready-after-reboot ]; then
        printf '%s\n' '{"ready":false,"boot_id":"22222222-2222-4222-8222-222222222222","private":"HEALTH-RESPONSE-SECRET"}'
      else
        printf '{"ready":true,"boot_id":"%s","private":"HEALTH-RESPONSE-SECRET"}\n' "$boot_id"
      fi
    elif [ "$FOGCAST_FAKE_MODE" = invalid-boot-newline ]; then
      printf '%s\n' '{"ready":true,"boot_id":"\n","private":"HEALTH-RESPONSE-SECRET"}'
    elif [ "$FOGCAST_FAKE_MODE" = invalid-boot-control ]; then
      printf '%s\n' '{"ready":true,"boot_id":"boot-\u0001-id","private":"HEALTH-RESPONSE-SECRET"}'
    elif [ "$FOGCAST_FAKE_MODE" = curl-stall ]; then
      exit 28
    elif [ "$FOGCAST_FAKE_MODE" = never-ready ]; then
      printf '%s\n' '{"ready":false,"boot_id":"11111111-1111-4111-8111-111111111111","private":"HEALTH-RESPONSE-SECRET"}'
    else
      printf '%s\n' '{"ready":true,"boot_id":"11111111-1111-4111-8111-111111111111","private":"HEALTH-RESPONSE-SECRET"}'
    fi
    ;;
  */api/v1/status)
    count=0
    [ ! -f "$FOGCAST_FAKE_STATUS_COUNT" ] || count=$(cat "$FOGCAST_FAKE_STATUS_COUNT")
    count=$((count + 1))
    printf '%s\n' "$count" > "$FOGCAST_FAKE_STATUS_COUNT"
    state=idle
    if [ "$FOGCAST_FAKE_MODE" = stop-not-idle ] && [ "$count" -eq 2 ]; then
      state=active
    elif [ "$FOGCAST_FAKE_MODE" = never-idle-after-reboot ] && [ -f "$FOGCAST_FAKE_REBOOTED" ]; then
      state=active
    fi
    printf '{"state":"%s","private":"STATUS-RESPONSE-SECRET"}\n' "$state"
    ;;
  *)
    exit 22
    ;;
esac
EOF

cat > "$fake_bin/sshpass" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "$*" >> "$FOGCAST_FAKE_SSH_LOG"
case "$*" in
  *FOGCAST_EXECUTABLES_BEGIN*)
    [ "$FOGCAST_FAKE_MODE" != inspection-error ] || exit 255
    case "$*" in
      *'/usr/bin/readlink '*) : ;;
      *) exit 127 ;;
    esac
    printf '%s\n' FOGCAST_EXECUTABLES_BEGIN
    printf '%s\n' /usr/sbin/mister-runtime-supervisor
    printf '%s\n' /usr/sbin/mister-agent-supervisor
    [ "$FOGCAST_FAKE_MODE" = missing-runtime ] || printf '%s\n' /usr/sbin/mister-runtime
    [ "$FOGCAST_FAKE_MODE" = missing-agent ] || printf '%s\n' /usr/sbin/mister-agent
    [ "$FOGCAST_FAKE_MODE" != duplicate-runtime ] || printf '%s\n' /usr/sbin/mister-runtime
    [ "$FOGCAST_FAKE_MODE" != main-process ] || printf '%s\n' /media/fat/MiSTer
    [ "$FOGCAST_FAKE_MODE" != deleted-main-process ] || printf '%s\n' '/media/fat/MiSTer (deleted)'
    printf '%s\n' FOGCAST_EXECUTABLES_END
    if [ "$FOGCAST_FAKE_MODE" = command-pipe ]; then
      printf '%s\n' FOGCAST_COMMAND_PIPE_FIFO=1
    else
      printf '%s\n' FOGCAST_COMMAND_PIPE_FIFO=0
    fi
    if [ "$FOGCAST_FAKE_MODE" = wrong-agent-identity ]; then
      printf '%s\n' FOGCAST_AGENT_SHA256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    else
      printf '%s\n' FOGCAST_AGENT_SHA256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    fi
    ;;
  *'/bin/cat /usr/share/mister-runtime/build-inputs'*)
    case "$FOGCAST_FAKE_MODE" in
      wrong-build-inputs)
        cat "$FOGCAST_FAKE_EXPECTED_INPUTS"
        printf '%s\n' unexpected_extra
        ;;
      missing-final-newline)
        input_size=$(wc -c < "$FOGCAST_FAKE_EXPECTED_INPUTS" | tr -d ' ')
        dd if="$FOGCAST_FAKE_EXPECTED_INPUTS" bs=1 count=$((input_size - 1)) 2>/dev/null
        ;;
      extra-blank-line)
        cat "$FOGCAST_FAKE_EXPECTED_INPUTS"
        printf '\n'
        ;;
      *)
        cat "$FOGCAST_FAKE_EXPECTED_INPUTS"
        ;;
    esac
    ;;
  *FOGCAST_REBOOT_STARTED*)
    count=0
    [ ! -f "$FOGCAST_FAKE_REBOOT_COUNT" ] || count=$(cat "$FOGCAST_FAKE_REBOOT_COUNT")
    count=$((count + 1))
    printf '%s\n' "$count" > "$FOGCAST_FAKE_REBOOT_COUNT"
    if [ "$FOGCAST_FAKE_MODE" = reboot-preexec-error ]; then
      exit 255
    fi
    : > "$FOGCAST_FAKE_REBOOTED"
    printf '%s\n' FOGCAST_REBOOT_STARTED
    # Model the expected SSH disconnect after reboot has begun.
    exit 255
    ;;
  *)
    exit 2
    ;;
esac
EOF

cat > "$fake_bin/timeout" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FOGCAST_FAKE_TIMEOUT_LOG"
[ "$#" -ge 2 ] || exit 2
shift
case "$FOGCAST_FAKE_MODE:$*" in
  inspection-stall:*FOGCAST_EXECUTABLES_BEGIN*|build-inputs-stall:*'/bin/cat /usr/share/mister-runtime/build-inputs'*|reboot-stall:*FOGCAST_REBOOT_STARTED*)
    exit 124
    ;;
esac
exec "$@"
EOF

cat > "$fake_bin/sleep" <<'EOF'
#!/bin/sh
set -eu
count=0
[ ! -f "$FOGCAST_FAKE_SLEEP_COUNT" ] || count=$(cat "$FOGCAST_FAKE_SLEEP_COUNT")
count=$((count + 1))
printf '%s\n' "$count" > "$FOGCAST_FAKE_SLEEP_COUNT"
EOF
chmod 0755 "$fake_bin/curl" "$fake_bin/sshpass" "$fake_bin/sleep" \
  "$fake_bin/timeout"
expected_inputs=$fixture/expected-build-inputs
cat > "$expected_inputs" <<'EOF'
format=1
mister_runtime_commit=93b369f7bf56757697cc5e59332545f5b4ee62f3
mister_agent_sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
idle_repository=https://github.com/MiSTer-devel/Distribution_MiSTer
idle_commit=f7bde4becb452ca28f604ad9802bbed5c6b58e01
idle_path=menu.rbf
idle_sha256=821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934
idle_size=2452588
idle_install_path=/usr/share/mister-runtime/idle.rbf
megadrive_origin=upstream
megadrive_abi=mister
megadrive_system=megadrive
megadrive_repository=https://github.com/MiSTer-devel/MegaDrive_MiSTer
megadrive_revision=7365a137cfd8fa6f041e964d8b953159c0ec42d9
megadrive_artifact=releases/MegaDrive_20260603.rbf
megadrive_sha256=0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
megadrive_size=4296864
megadrive_install_path=/usr/share/mister-runtime/cores/megadrive.rbf
EOF

selection=$fixture/megadrive.selection.toml
cat > "$selection" <<'EOF'
format = 1
origin = 'upstream'
abi = 'mister'
system = 'megadrive'
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
revision = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
artifact = 'releases/MegaDrive_20260603.rbf'
sha256 = '0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839'
size = 4296864
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
EOF
chmod 0444 "$selection"
upstream_label_selection=$fixture/upstream-label-selection.toml
sed "/install_path/a label = 'unexpected'" "$selection" > "$upstream_label_selection"
chmod 0444 "$upstream_label_selection"
wrong_selection=$fixture/wrong-selection.toml
cat > "$wrong_selection" <<'EOF'
format = 1
origin = 'source-built'
abi = 'mister'
system = 'megadrive'
repository = 'https://fixture.invalid/source-built-megadrive'
revision = '4444444444444444444444444444444444444444'
artifact = 'megadrive.rbf'
sha256 = '0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839'
size = 4296864
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
recipe = 'scripts/rebuild_core.py'
recipe_sha256 = '5555555555555555555555555555555555555555555555555555555555555555'
toolchain = 'fixture-toolchain'
EOF
chmod 0444 "$wrong_selection"

reset_case() {
  : > "$fixture/curl.log"
  : > "$fixture/ssh.log"
  : > "$fixture/timeout.log"
  rm -f "$fixture/stop.count" "$fixture/status.count" "$fixture/reboot.count" \
    "$fixture/rebooted" "$fixture/sleep.count"
}

run_smoke() {
  mode=$1
  output=$2
  selection_file=$selection
  [ "$mode" != upstream-label ] || selection_file=$upstream_label_selection
  [ "$mode" != wrong-selection ] || selection_file=$wrong_selection
  FOGCAST_FAKE_MODE=$mode \
  FOGCAST_FAKE_CURL_LOG=$fixture/curl.log \
  FOGCAST_FAKE_SSH_LOG=$fixture/ssh.log \
  FOGCAST_FAKE_TIMEOUT_LOG=$fixture/timeout.log \
  FOGCAST_FAKE_STOP_COUNT=$fixture/stop.count \
  FOGCAST_FAKE_STATUS_COUNT=$fixture/status.count \
  FOGCAST_FAKE_REBOOT_COUNT=$fixture/reboot.count \
  FOGCAST_FAKE_REBOOTED=$fixture/rebooted \
  FOGCAST_FAKE_SLEEP_COUNT=$fixture/sleep.count \
  FOGCAST_FAKE_EXPECTED_INPUTS=$expected_inputs \
  FOGCAST_HOST_API=http://host.test \
  FOGCAST_TARGET_API=http://target.test:8182 \
  FOGCAST_TARGET_HOST=target.test \
  FOGCAST_TARGET_USER=root \
  FOGCAST_TARGET_PASSWORD=fixture-password-secret \
  FOGCAST_POLL_ATTEMPTS=3 \
  FOGCAST_POLL_INTERVAL=0 \
  FOGCAST_CALL_TIMEOUT=2 \
  NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=$selection_file \
  PATH="$fake_bin:$PATH" \
    sh "$smoke" > "$output" 2>&1
}

assert_safe_output() {
  output=$1
  if grep -Eq 'fixture-password-secret|RESPONSE-SECRET|"ready"|"state"|"private"' "$output"; then
    printf '%s\n' 'native runtime smoke exposed private fixture data' >&2
    cat "$output" >&2
    exit 1
  fi
}

reset_case
success_output=$fixture/success.out
if ! run_smoke success "$success_output"; then
  printf '%s\n' 'native runtime smoke rejected the success fixture' >&2
  cat "$success_output" >&2
  exit 1
fi
success_expected=$fixture/success.expected
printf '%s\n' \
  'native runtime smoke passed: boot 11111111-1111-4111-8111-111111111111 -> 22222222-2222-4222-8222-222222222222, idle -> idle' \
  > "$success_expected"
cmp -s "$success_expected" "$success_output" || {
  printf '%s\n' 'native runtime smoke did not produce the exact success record' >&2
  cat "$success_output" >&2
  exit 1
}
test "$(cat "$fixture/stop.count")" -eq 1
test "$(cat "$fixture/reboot.count")" -eq 1
test "$(grep -Fc -- 'http://host.test/api/v1/session/stop' "$fixture/curl.log")" -eq 1
test "$(grep -Fc -- 'FOGCAST_REBOOT_STARTED' "$fixture/timeout.log")" -eq 1
test "$(grep -Ec '^2 sshpass -p ' "$fixture/timeout.log")" -eq 3
if grep -Fv -- '--connect-timeout 2 --max-time 2' "$fixture/curl.log" >/dev/null; then
  printf '%s\n' 'native runtime smoke issued curl without both timeouts' >&2
  exit 1
fi
grep -Fq -- 'http://target.test:8182/v1/health' "$fixture/curl.log"
grep -Fq -- 'http://host.test/api/v1/status' "$fixture/curl.log"
grep -Fq -- 'root@target.test' "$fixture/ssh.log"
assert_safe_output "$success_output"

count_file() {
  count_path=$1
  if [ -f "$count_path" ]; then
    cat "$count_path"
  else
    printf '%s\n' 0
  fi
}

assert_lifecycle() {
  expected_stops=$1
  expected_reboots=$2
  test "$(count_file "$fixture/stop.count")" -eq "$expected_stops"
  test "$(grep -Fc -- 'FOGCAST_REBOOT_STARTED' "$fixture/timeout.log" || true)" -eq \
    "$expected_reboots"
}

run_failure() {
  mode=$1
  expected_error=$2
  expected_stops=$3
  expected_reboots=$4
  reset_case
  output=$fixture/$mode.out
  if run_smoke "$mode" "$output"; then
    printf 'native runtime smoke accepted failure mode: %s\n' "$mode" >&2
    exit 1
  fi
  expected_output=$fixture/$mode.expected
  printf '%s\n' "$expected_error" > "$expected_output"
  cmp -s "$expected_output" "$output" || {
    printf 'native runtime smoke returned an unstable error for %s\n' "$mode" >&2
    cat "$output" >&2
    exit 1
  }
  assert_safe_output "$output"
  assert_lifecycle "$expected_stops" "$expected_reboots"
}

run_failure never-ready \
  'native-runtime-smoke: initial ready idle state was not observed' 0 0
test "$(cat "$fixture/sleep.count")" -eq 2
run_failure curl-stall \
  'native-runtime-smoke: initial ready idle state was not observed' 0 0
run_failure invalid-boot-newline \
  'native-runtime-smoke: initial ready idle state was not observed' 0 0
run_failure invalid-boot-control \
  'native-runtime-smoke: initial ready idle state was not observed' 0 0
run_failure missing-runtime \
  'native-runtime-smoke: expected exactly one mister-runtime executable' 0 0
run_failure missing-agent \
  'native-runtime-smoke: expected exactly one mister-agent executable' 0 0
run_failure duplicate-runtime \
  'native-runtime-smoke: expected exactly one mister-runtime executable' 0 0
run_failure main-process \
  'native-runtime-smoke: conventional MiSTer executable is running' 0 0
run_failure deleted-main-process \
  'native-runtime-smoke: conventional MiSTer executable is running' 0 0
run_failure command-pipe \
  'native-runtime-smoke: /dev/MiSTer_cmd is a FIFO' 0 0
run_failure wrong-agent-identity \
  'native-runtime-smoke: installed build inputs differ from selection' 0 0
run_failure wrong-build-inputs \
  'native-runtime-smoke: installed build inputs differ from selection' 0 0
run_failure wrong-selection \
  'native-runtime-smoke: installed build inputs differ from selection' 0 0
run_failure upstream-label \
  'native-runtime-smoke: Mega Drive upstream selection is invalid' 0 0
run_failure missing-final-newline \
  'native-runtime-smoke: installed build inputs differ from selection' 0 0
run_failure extra-blank-line \
  'native-runtime-smoke: installed build inputs differ from selection' 0 0
run_failure inspection-error \
  'native-runtime-smoke: target inspection failed' 0 0
run_failure inspection-stall \
  'native-runtime-smoke: target inspection failed' 0 0
run_failure build-inputs-stall \
  'native-runtime-smoke: target inspection failed' 0 0
run_failure stop-request-error \
  'native-runtime-smoke: host stop request failed' 1 0
run_failure stop-not-idle \
  'native-runtime-smoke: host did not remain idle after stop' 1 0
run_failure reboot-preexec-error \
  'native-runtime-smoke: reboot command did not start' 1 1
run_failure reboot-stall \
  'native-runtime-smoke: reboot command did not start' 1 1
run_failure unchanged-boot \
  'native-runtime-smoke: fresh ready idle state with changed boot ID was not observed' 1 1
run_failure never-ready-after-reboot \
  'native-runtime-smoke: fresh ready idle state with changed boot ID was not observed' 1 1
run_failure never-idle-after-reboot \
  'native-runtime-smoke: fresh ready idle state with changed boot ID was not observed' 1 1

run_invalid_configuration() {
  attempts=$1
  interval=$2
  call_timeout=$3
  expected_error=$4
  reset_case
  output=$fixture/configuration.out
  if FOGCAST_POLL_ATTEMPTS=$attempts \
    FOGCAST_POLL_INTERVAL=$interval \
    FOGCAST_CALL_TIMEOUT=$call_timeout \
    PATH="$fake_bin:$PATH" sh "$smoke" > "$output" 2>&1; then
    printf '%s\n' 'native runtime smoke accepted invalid numeric configuration' >&2
    exit 1
  fi
  expected_output=$fixture/configuration.expected
  printf '%s\n' "$expected_error" > "$expected_output"
  cmp -s "$expected_output" "$output"
}

for invalid_attempts in 0 01 1_0 301 999999999999999999999999999999; do
  run_invalid_configuration "$invalid_attempts" 1 2 \
    'native-runtime-smoke: poll attempts must be an integer from 1 to 300'
done
for invalid_interval in 1_0 -0.0 NaN 60.1 999999999999999999999999999999; do
  run_invalid_configuration 3 "$invalid_interval" 2 \
    'native-runtime-smoke: poll interval must be a decimal from 0 to 60 seconds'
done
for invalid_timeout in 0 01 1_0 -0.0 NaN 60.1 999999999999999999999999999999; do
  run_invalid_configuration 3 1 "$invalid_timeout" \
    'native-runtime-smoke: call timeout must be a decimal greater than 0 and at most 60 seconds'
done

printf '%s\n' 'native runtime smoke tests passed'
