#!/bin/bash
# Holds the Kit D25 claim (both ledger locks + UART lock) for the #2353 diagnostic run and records the J8G UART receive-only.
set -u
R=/data/de25-2353-diag; KS=/home/deano/kit-status
DEV=/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8J8G-if02-port0
exec 8>"$KS/de25-TRWU8J8G-STATUS.lock" 9>"$KS/de25-TRWU8J8G-STATUS.md.lock" 7>"$KS/de25-TRWU8J8G-UART.lock"
flock -n 8 && flock -n 9 && flock -n 7 || { echo "BUSY $(date -Is)" > "$R/logs/claim.log"; exit 3; }
last=$(awk '/^## /{line=$0} END{print line}' "$KS/de25-TRWU8J8G-STATUS.md")
[[ "$last" == *"KIT D25 FREE (baseline verified)"* ]] || { echo "NOT_FREE: $last" > "$R/logs/claim.log"; exit 3; }
cp /home/deano/fes-hil/baselines/current-restore-de25.json "$R/out/baseline-record.json"
printf "\n## KIT D25 IN USE (de25-j8g) — owner: Claude session practical-lewin-bbdef5 (for Deano; Deano OK \"OK, run it\" 2026-11-27); scope: #2353 diagnostic — in-session relaunch black HDMI (lease vs time-since-Stop vs prior input), read-only probes; no image change, no reboot; head: ebe9d522eee31beb1ddc25be5ea9a6662e08e060; bundle: 69d7cfdcedd0d247fe5bc72c7b9f016ea88d79f4887452d6bb39f24bc8283518 (restore to cbf468031a31e6af75f385ced40b03f7d255bdf580c44353fb5518bf9736a1f6); package: 36540290d89653c33de09e9e3600cc88adb5b316da2442cdc981ccc256b876e1; started: %s; evidence: %s.\n" "$(date -Is)" "$R" >> "$KS/de25-TRWU8J8G-STATUS.md"
echo "CLAIMED $(date -Is)" > "$R/logs/claim.log"
stty -F "$DEV" 115200 raw -echo -echoe -echok -echoctl -echoke 2>>"$R/logs/claim.log"
cat "$DEV" >> "$R/logs/uart-j8g.log" &
echo $! > "$R/uart.pid"
exec sleep infinity
