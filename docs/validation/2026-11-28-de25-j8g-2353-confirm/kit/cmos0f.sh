#!/bin/bash
# Read-only: CMOS 0Fh (record.bin byte 128) of the most recently written pc386 game record on J8G, with its path and mtime.
/home/deano/lander-hilq/kits/kssh D25 'f=$(ls -t /media/fat/fogcast/core-data/games/*/record.bin 2>/dev/null | head -1); [ -n "$f" ] && echo "$(od -A n -t x1 -j 128 -N 1 $f | tr -d " ") $(date -r $f -Iseconds) $f" || echo none'
