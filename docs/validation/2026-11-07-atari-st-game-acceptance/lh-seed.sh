#!/bin/bash
# lh-seed.sh NAME [SEEDHEX]: lh-hatari.sh with a breakpoint on TOS 1.04 XBIOS Random (fc1c76) that logs _hz_200 and
# the seed, and (with SEEDHEX, 8 hex digits) forces the seed at the first call.
N=$1; S=$2; cd /home/deano/fes-scratch/st1528; mkdir -p hat/$N
{ echo 'm $4ba-$4be'; echo 'm $eb0-$eb4'; [ -n "$S" ] && echo "w \$eb0 ${S:0:2} ${S:2:2} ${S:4:2} ${S:6:2}"; echo 'm $eb0-$eb4'; } > hat/$N/hit.ini
echo "b pc = \$fc1c76 :trace :file /w/hat/$N/hit.ini" > hat/$N/bp.ini
cp "private/st/Lurking Horror, The (Europe) (r203).st" private/$N.st
tools/hstart.sh $N "/w/private/tos/TOS v1.04 (1989)(Atari Corp)(Mega ST)(GB)[Rainbow TOS].img" /w/private/$N.st --auto 'A:\LURKING.PRG' --parse /w/hat/$N/bp.ini
sleep 30; F=hat/$N/fifo; echo "hatari-event keypress 57" > $F; sleep 2; i=0
while IFS= read -r c; do i=$((i+1)); if [ "$c" = "<SPACE>" ]; then echo "hatari-event keypress 57" > $F; else tools/hty.sh $N "$c"; fi; sleep 5; done < tools/lh-script.txt
sleep 8; echo "hatari-debug screenshot /w/hat/$N/final.png" > $F; sleep 1; echo "hatari-shortcut quit" > $F; sleep 4
docker logs st1528-h-$N > hat/$N/dbg.log 2>&1; docker rm -f st1528-h-$N >/dev/null
MTOOLS_SKIP_CHECK=1 mcopy -n -i private/$N.st ::FES1528.SAV hat/$N/FES1528.SAV; sha256sum hat/$N/FES1528.SAV
