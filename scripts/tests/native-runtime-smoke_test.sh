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
    printf '%s\n' '{"state":"idle","private":"STOP-RESPONSE-SECRET"}'
    ;;
  */v1/health)
    if [ -f "$FOGCAST_FAKE_REBOOTED" ]; then
      boot_id=boot-after
      [ "$FOGCAST_FAKE_MODE" != unchanged-boot ] || boot_id=boot-before
      if [ "$FOGCAST_FAKE_MODE" = never-ready-after-reboot ]; then
        printf '%s\n' '{"ready":false,"boot_id":"boot-after","private":"HEALTH-RESPONSE-SECRET"}'
      else
        printf '{"ready":true,"boot_id":"%s","private":"HEALTH-RESPONSE-SECRET"}\n' "$boot_id"
      fi
    elif [ "$FOGCAST_FAKE_MODE" = never-ready ]; then
      printf '%s\n' '{"ready":false,"boot_id":"boot-before","private":"HEALTH-RESPONSE-SECRET"}'
    else
      printf '%s\n' '{"ready":true,"boot_id":"boot-before","private":"HEALTH-RESPONSE-SECRET"}'
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
    printf '%s\n' FOGCAST_EXECUTABLES_BEGIN
    printf '%s\n' /usr/sbin/mister-runtime-supervisor
    printf '%s\n' /usr/sbin/mister-agent-supervisor
    [ "$FOGCAST_FAKE_MODE" = missing-runtime ] || printf '%s\n' /usr/sbin/mister-runtime
    [ "$FOGCAST_FAKE_MODE" = missing-agent ] || printf '%s\n' /usr/sbin/mister-agent
    [ "$FOGCAST_FAKE_MODE" != duplicate-runtime ] || printf '%s\n' /usr/sbin/mister-runtime
    [ "$FOGCAST_FAKE_MODE" != main-process ] || printf '%s\n' /media/fat/MiSTer
    printf '%s\n' FOGCAST_EXECUTABLES_END
    if [ "$FOGCAST_FAKE_MODE" = command-pipe ]; then
      printf '%s\n' FOGCAST_COMMAND_PIPE_FIFO=1
    else
      printf '%s\n' FOGCAST_COMMAND_PIPE_FIFO=0
    fi
    ;;
  *'/bin/cat /usr/share/mister-runtime/build-inputs'*)
    if [ "$FOGCAST_FAKE_MODE" = wrong-build-inputs ]; then
      cat "$FOGCAST_FAKE_EXPECTED_INPUTS"
      printf '%s\n' FOGCAST_BUILD_INPUTS_END unexpected_extra
    else
      cat "$FOGCAST_FAKE_EXPECTED_INPUTS"
    fi
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

cat > "$fake_bin/sleep" <<'EOF'
#!/bin/sh
set -eu
count=0
[ ! -f "$FOGCAST_FAKE_SLEEP_COUNT" ] || count=$(cat "$FOGCAST_FAKE_SLEEP_COUNT")
count=$((count + 1))
printf '%s\n' "$count" > "$FOGCAST_FAKE_SLEEP_COUNT"
EOF
chmod 0755 "$fake_bin/curl" "$fake_bin/sshpass" "$fake_bin/sleep"
expected_inputs=$fixture/expected-build-inputs
cat > "$expected_inputs" <<'EOF'
format=1
mister_runtime_commit=1045306bf97d5e8f68fde7626cb954194f505ff5
idle_repository=https://github.com/MiSTer-devel/Distribution_MiSTer
idle_commit=f7bde4becb452ca28f604ad9802bbed5c6b58e01
idle_path=menu.rbf
idle_sha256=821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934
idle_size=2452588
idle_install_path=/usr/share/mister-runtime/idle.rbf
EOF

reset_case() {
  : > "$fixture/curl.log"
  : > "$fixture/ssh.log"
  rm -f "$fixture/stop.count" "$fixture/status.count" "$fixture/reboot.count" \
    "$fixture/rebooted" "$fixture/sleep.count"
}

run_smoke() {
  mode=$1
  output=$2
  FOGCAST_FAKE_MODE=$mode \
  FOGCAST_FAKE_CURL_LOG=$fixture/curl.log \
  FOGCAST_FAKE_SSH_LOG=$fixture/ssh.log \
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
run_smoke success "$success_output"
test "$(cat "$success_output")" = \
  'native runtime smoke passed: boot boot-before -> boot-after, idle -> idle'
test "$(cat "$fixture/stop.count")" -eq 1
test "$(cat "$fixture/reboot.count")" -eq 1
test "$(grep -Fc -- 'http://host.test/api/v1/session/stop' "$fixture/curl.log")" -eq 1
grep -Fq -- 'http://target.test:8182/v1/health' "$fixture/curl.log"
grep -Fq -- 'http://host.test/api/v1/status' "$fixture/curl.log"
grep -Fq -- 'root@target.test' "$fixture/ssh.log"
assert_safe_output "$success_output"

run_failure() {
  mode=$1
  expected_error=$2
  reset_case
  output=$fixture/$mode.out
  if run_smoke "$mode" "$output"; then
    printf 'native runtime smoke accepted failure mode: %s\n' "$mode" >&2
    exit 1
  fi
  grep -Fqx "$expected_error" "$output" || {
    printf 'native runtime smoke returned an unstable error for %s\n' "$mode" >&2
    cat "$output" >&2
    exit 1
  }
  assert_safe_output "$output"
}

run_failure never-ready \
  'native-runtime-smoke: initial ready idle state was not observed'
test "$(cat "$fixture/sleep.count")" -eq 2
run_failure missing-runtime \
  'native-runtime-smoke: expected exactly one mister-runtime executable'
run_failure missing-agent \
  'native-runtime-smoke: expected exactly one mister-agent executable'
run_failure duplicate-runtime \
  'native-runtime-smoke: expected exactly one mister-runtime executable'
run_failure main-process \
  'native-runtime-smoke: conventional MiSTer executable is running'
run_failure command-pipe \
  'native-runtime-smoke: /dev/MiSTer_cmd is a FIFO'
run_failure wrong-build-inputs \
  'native-runtime-smoke: installed build inputs differ from lock'
run_failure inspection-error \
  'native-runtime-smoke: target inspection failed'
run_failure stop-not-idle \
  'native-runtime-smoke: host did not remain idle after stop'
test "$(cat "$fixture/stop.count")" -eq 1
run_failure reboot-preexec-error \
  'native-runtime-smoke: reboot command did not start'
test "$(cat "$fixture/reboot.count")" -eq 1
run_failure unchanged-boot \
  'native-runtime-smoke: fresh ready idle state with changed boot ID was not observed'
run_failure never-ready-after-reboot \
  'native-runtime-smoke: fresh ready idle state with changed boot ID was not observed'
run_failure never-idle-after-reboot \
  'native-runtime-smoke: fresh ready idle state with changed boot ID was not observed'

printf '%s\n' 'native runtime smoke tests passed'
