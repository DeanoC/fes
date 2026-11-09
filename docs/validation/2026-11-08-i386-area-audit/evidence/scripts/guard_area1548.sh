# guard_area1548.sh: sourced by the area-audit wrappers (AGENTS.md "Ops").
# guarded_run LOG CMD...: refuses under 30G free where the build runs,
# writes a heartbeat to LOG every HEARTBEAT_S (300) seconds, caps the job
# at CAP (3h, timeout(1) syntax), appends "exit N <time>" to LOG, runs
# $NOTIFY_CMD "<status line>" if it is set, and returns the job's status.
guarded_run() {
    local log=$1; shift
    local free_g
    free_g=$(df -BG --output=avail . | tail -n 1 | tr -dc '0-9')
    if [ "$free_g" -lt 30 ]; then
        echo "refused: ${free_g}G free under $PWD, need 30G" | tee -a "$log" >&2
        return 2
    fi
    ( while sleep "${HEARTBEAT_S:-300}"; do echo "heartbeat $(date -Is)" >> "$log.heartbeat"; done ) &
    local hb=$!
    timeout --signal=TERM --kill-after=5m "${CAP:-3h}" "$@" >> "$log" 2>&1
    local rc=$?
    kill "$hb" 2>/dev/null; wait "$hb" 2>/dev/null
    local msg="$(basename "$log"): exit $rc $(date -Is)"
    [ "$rc" -eq 124 ] && msg="$msg (time cap ${CAP:-3h} reached)"
    echo "exit $rc $(date -Is)" >> "$log"
    echo "$msg" >&2
    [ -n "${NOTIFY_CMD:-}" ] && $NOTIFY_CMD "$msg"
    return "$rc"
}
