#!/bin/bash
set -eu
label="$1"
ffmpeg -nostdin -hide_banner -loglevel warning -f v4l2 -input_format yuyv422 -video_size 1920x1080 -framerate 30 -i /dev/v4l/by-id/fes-ms2109-hil -t 3 -c:v ffv1 "out/hdmi/${label}.mkv" > "out/hdmi/${label}-capture.log" 2>&1
ffmpeg -nostdin -hide_banner -loglevel error -ss 2 -i "out/hdmi/${label}.mkv" -frames:v 1 "out/hdmi/${label}.png"
