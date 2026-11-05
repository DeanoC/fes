#!/bin/sh
# k863-sampler.sh OUT: 1 s memory/CPU/write samples for the #863 slice 2 kit re-measure.
OUT=$1
pidof1(){ pidof "$1" 2>/dev/null | cut -d' ' -f1; }
rss(){ [ -n "$1" ] && awk '/^VmRSS/{print $2}' /proc/$1/status 2>/dev/null || echo 0; }
ticks(){ [ -n "$1" ] && awk '{print $14+$15}' /proc/$1/stat 2>/dev/null || echo 0; }
echo "t	memavail_kb	tmp_used_kb	api_rss	api_ticks	tenfoot_rss	agent_rss	agent_ticks	runtime_rss	cpu_busy	cpu_total	mmc_wr_sectors" > $OUT
while :; do
  A=$(pidof1 fogcast-api); T=$(pidof1 fogcast-tenfoot); G=$(pidof1 mister-agent); R=$(pidof1 mister-runtime)
  MA=$(awk '/^MemAvailable/{print $2}' /proc/meminfo)
  TU=$(df -k /tmp | awk 'NR==2{print $3}')
  CPU=$(awk '/^cpu /{b=$2+$3+$4+$7+$8; t=b+$5+$6; print b"\t"t}' /proc/stat)
  W=$(awk '$3=="mmcblk0"{print $10}' /proc/diskstats)
  printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n" "$(cut -d' ' -f1 /proc/uptime)" "$MA" "$TU" "$(rss $A)" "$(ticks $A)" "$(rss $T)" "$(rss $G)" "$(ticks $G)" "$(rss $R)" "$CPU" "$W" >> $OUT
  sleep 1
done
