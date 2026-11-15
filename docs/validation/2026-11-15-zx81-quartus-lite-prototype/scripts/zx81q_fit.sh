#!/bin/bash
# zx81q_fit.sh <project-dir>: prototype Quartus Lite compile with a 1 h cap.
# Exits with quartus_sh's status (124 on timeout); the status is also logged.
export QUARTUS_ROOTDIR=${QUARTUS_ROOTDIR:-/home/deano/intelFPGA_lite/17.0/quartus}
cd "$1" || exit 1
timeout 3600 nice -n 10 "$QUARTUS_ROOTDIR/bin/quartus_sh" --flow compile top > compile.log 2>&1
status=$?
echo "exit $status" >> compile.log
exit $status
