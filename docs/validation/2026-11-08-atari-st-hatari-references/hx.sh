#!/bin/bash
# hx.sh NAME TOS DISK [extra hatari args]: Hatari 2.4.1 520ST (same machine options as hstart.sh) in a detached
# container on its own Xvfb display :9 with a real SDL window, so xdotool can drive the host mouse and keys
# (joystick 1 = cursor keys + right Ctrl). Commands: docker exec st1528-x-NAME xdotool ...; screenshots via xwd/import.
N=$1; TOS=$2; DISK=$3; shift 3
cd /home/deano/fes-scratch/st1528; mkdir -p hat/$N; rm -f hat/$N/fifo
docker rm -f st1528-x-$N >/dev/null 2>&1
docker run -d --name st1528-x-$N -u $(id -u):$(id -g) -v $PWD:/w -w /w/hat/$N -e DISPLAY=:9 -e SDL_AUDIODRIVER=dummy -e HOME=/tmp st1528-x11 \
  sh -c "Xvfb :9 -screen 0 1024x768x24 -nolisten tcp >/dev/null 2>&1 & sleep 1; exec hatari --machine st --memsize 0 --cpuclock 8 --cpulevel 0 --compatible on --tos \"$TOS\" --patch-tos off --fast-boot off \
  --disk-a \"$DISK\" --drive-b off --protect-floppy off --fastfdc off --monitor rgb --tos-res low --sound 48000 --confirm-quit off \
  --cmd-fifo /w/hat/$N/fifo --statusbar off --drive-led off --borders off --zoom 1 $*" > /dev/null
for i in $(seq 50); do [ -p hat/$N/fifo ] && break; sleep 0.2; done; ls -la hat/$N/fifo
