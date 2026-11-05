#!/bin/sh
# k863-idlecpu.sh: read-only idle CPU sample of kit processes over $1 seconds.
d=${1:-30}
snap() { for p in $(pidof fes-kit-software fogcast-tenfoot mister-agent mister-runtime); do echo "$p $(cat /proc/$p/comm) $(cut -d' ' -f14,15 /proc/$p/stat)"; done; head -1 /proc/stat; }
snap > /tmp/k863-a; sleep $d; snap > /tmp/k863-b
awk -v d=$d 'NR==FNR{a[$1]=$3+$4; next} ($1 in a) && $2!="cpu"{printf "%s %s %.0f%% of a core\n",$1,$2,($3+$4-a[$1])/d}' /tmp/k863-a /tmp/k863-b
awk 'NR==FNR&&/^cpu /{for(i=2;i<=NF;i++)t+=$i; idle=$5+$6; next} /^cpu /{for(i=2;i<=NF;i++)u+=$i; printf "system busy %.0f%% of 2 cores\n",100*(1-(($5+$6)-idle)/(u-t))}' /tmp/k863-a /tmp/k863-b
rm -f /tmp/k863-a /tmp/k863-b
