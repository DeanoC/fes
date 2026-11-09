#!/bin/bash
# tmplay3.sh RUN SECONDS TAG: tmplay2.sh reading runs/RUN/live.png (vrec, 960x540) instead of grabs, which fail while vrec records. Terramex play with restart; grab every burst; on the title (grey at 400,900) restart as Fortisque-Smithe.
R=$1; T=$2; G=$3; F=runs/$R/fr; end=$(( $(date +%s) + T )); i=0; play=0; restarts=0
while [ $(date +%s) -lt $end ]; do
  i=$((i+1)); n=$G$(printf %03d $i); cp runs/$R/live.png $F/$n.png
  px=$(ffmpeg -loglevel error -i $F/$n.png -vf "crop=1:1:200:450" -f rawvideo -pix_fmt rgb24 - | od -An -tu1 | tr -s ' ')
  set -- $px
  if [ "${1:-0}" -gt 20 ] && [ "$1" = "$2" ]; then
    restarts=$((restarts+1)); echo "$(date +%T) $n title (px $px) -> restart $restarts"
    python3 c.py $R '{"op":"joy","b":"left","hold":3}' '{"op":"joy","b":"up","hold":3}' '{"op":"joy","b":"right","hold":0.8}' '{"op":"joy","b":"fire","hold":0.3}' '{"op":"sleep","s":8}' > /dev/null
  else
    play=$((play+1)); echo "$(date +%T) $n play (px $px)"
    python3 c.py $R '{"op":"joy","b":"right","hold":1.2}' '{"op":"sleep","s":0.5}' '{"op":"joy","b":"up","hold":0.3}' '{"op":"joy","b":"right","hold":1.5}' '{"op":"joy","b":"fire","hold":0.2}' '{"op":"sleep","s":1}' '{"op":"joy","b":"left","hold":0.8}' '{"op":"joy","b":"right","hold":2.0}' '{"op":"sleep","s":2}' '{"op":"joy","b":"down","hold":0.3}' '{"op":"joy","b":"right","hold":1.5}' '{"op":"sleep","s":2}' > /dev/null
  fi
done; echo "bursts play=$play restarts=$restarts"
