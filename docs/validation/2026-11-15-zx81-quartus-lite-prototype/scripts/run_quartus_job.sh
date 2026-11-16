#!/bin/bash
# run_quartus_job.sh <log> <command...>: run one Quartus step with the AGENTS.md
# job guards. It refuses to start with under 30 GB free in the working
# directory, caps the step at one hour, prints a heartbeat every minute, and
# ends with a FINISHED or FAILED line. It exits with the step's status (124 on
# the time cap).
log=$1; shift
free_kb=$(df -Pk . | awk 'NR == 2 { print $4 }')
if [ "${free_kb:-0}" -lt $((30 * 1000 * 1000)) ]; then
    echo "FAILED $1: under 30 GB free in $(pwd)" >&2
    exit 1
fi
timeout 3600 nice -n 10 "$@" > "$log" 2>&1 &
job=$!
start=$(date +%s)
beat=$start
while kill -0 "$job" 2>/dev/null; do
    sleep 1
    now=$(date +%s)
    if [ $((now - beat)) -ge 60 ] && kill -0 "$job" 2>/dev/null; then
        echo "  $(basename "$1") running $((now - start)) s ($(pwd))"
        beat=$now
    fi
done
wait "$job"
status=$?
echo "exit $status" >> "$log"
if [ "$status" -eq 0 ]; then echo "FINISHED $(basename "$1") in $(pwd)"; else echo "FAILED $(basename "$1") (exit $status); log: $log" >&2; fi
exit $status
