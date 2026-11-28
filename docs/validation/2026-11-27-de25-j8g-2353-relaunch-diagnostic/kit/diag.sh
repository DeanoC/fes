#!/bin/bash
# Read-only J8G probe for #2353. Never touches 0x10d21400; no register reads, no writes.
D=/data/de25-2353-diag; tag=$1
/home/deano/lander-hilq/kits/kssh D25 'T=$(grep "^token" /media/fat/fogcast/agent.toml | cut -d= -f2 | xargs); A="curl -fsS -m 10 -H Authorization:Bearer\ $T"
echo "uptime=$(cut -d" " -f1 /proc/uptime) date=$(date -Is)"
for p in /v1/status /v1/local/status /v1/health /v1/kit/lease; do echo "--- $p"; eval $A http://127.0.0.1:8182$p; echo; done
echo "--- events"; tail -c 6000 /run/mister-runtime.events.json; echo
p=$(pidof mister-runtime); echo "--- runtime pid $p"; ls -l /proc/$p/fd 2>/dev/null | grep -e de25 -e window -e scanout -e "2 ->" -e "1 ->"
e=$(readlink /proc/$p/fd/2); echo "stderr=$e"; case "$e" in /*) tail -n 80 "$e";; esac
echo "--- dmesg tail"; dmesg | tail -n 40
echo "--- ps"; ps | grep -v "\[" | grep -i -e tenfoot -e fogcast -e mister -e launcher | grep -v grep
echo "--- meminfo"; grep -e MemFree -e CmaFree /proc/meminfo' > $D/out/diag-$tag.txt 2>&1
wc -c $D/out/diag-$tag.txt
