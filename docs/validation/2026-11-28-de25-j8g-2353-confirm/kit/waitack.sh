#!/bin/bash
# waitack.sh RUNDIR N [timeout_s]: wait until RUNDIR/acks.jsonl has N lines; print the last ack.
R=$1; N=$2; T=${3:-300}; for i in $(seq 1 $T); do [ "$(wc -l < $R/acks.jsonl 2>/dev/null || echo 0)" -ge $N ] && { tail -1 $R/acks.jsonl | cut -c1-300; exit 0; }; [ -f $R/result.json ] && grep -q '"error"' $R/result.json && { echo DRIVER-ERROR; tail -3 $R/driver.log; exit 2; }; sleep 1; done; echo TIMEOUT; tail -3 $R/driver.log; exit 1
