#!/bin/bash
# Holds the Kit D25 claim (both ledger locks + UART lock) for the #2353 confirm run and records the J8G UART receive-only.
set -u
R=/data/de25-2353-confirm; KS=/home/deano/kit-status
DEV=/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8J8G-if02-port0
exec 8>"$KS/de25-TRWU8J8G-STATUS.lock" 9>"$KS/de25-TRWU8J8G-STATUS.md.lock" 7>"$KS/de25-TRWU8J8G-UART.lock"
flock -n 8 && flock -n 9 && flock -n 7 || { echo "BUSY $(date -Is)" > "$R/logs/claim.log"; exit 3; }
last=$(awk '/^## /{line=$0} END{print line}' "$KS/de25-TRWU8J8G-STATUS.md")
[[ "$last" == *"KIT D25 FREE (baseline verified)"* ]] || { echo "NOT_FREE: $last" > "$R/logs/claim.log"; exit 3; }
cp /home/deano/fes-hil/baselines/current-restore-de25.json "$R/out/baseline-record.json"
printf "\n## KIT D25 IN USE (de25-j8g) — owner: Claude session practical-lewin-bbdef5 (for Deano; Deano OK \"OK, run it\" 2026-11-27); scope: PR 2366 confirm (#2353 BIOS fix): 486SX package at b7a74aba4e, loads restoring CMOS 0Fh=09h must be live; no image change, no reboot; head: b7a74aba4e9b853c5a4dc6521b36031bc4471ede; PR: 2366; bundle: 69d7cfdcedd0d247fe5bc72c7b9f016ea88d79f4887452d6bb39f24bc8283518 (restore to cbf468031a31e6af75f385ced40b03f7d255bdf580c44353fb5518bf9736a1f6); package: cc021eee70db6b30fa57f9611a44387e92c54edaa43570d16bec97adcddb1e20 (sha256 98fb32ed4fa7b82d125c605cde909559ebe73514a42cbd0c603aae01ffdafe8b, BIOS rom_hex abe6f29c); started: %s; evidence: %s.\n" "$(date -Is)" "$R" >> "$KS/de25-TRWU8J8G-STATUS.md"
echo "CLAIMED $(date -Is)" > "$R/logs/claim.log"
stty -F "$DEV" 115200 raw -echo -echoe -echok -echoctl -echoke 2>>"$R/logs/claim.log"
cat "$DEV" >> "$R/logs/uart-j8g.log" &
echo $! > "$R/uart.pid"
echo $$ > "$R/claim.pid"
exec sleep infinity
