#!/bin/bash
# zx81q_fit.sh <project-dir>: prototype Quartus Lite compile, run through
# run_quartus_job.sh (30 GB preflight, 1 h cap, heartbeat, finish/fail line).
# Exits with quartus_sh's status (124 on timeout); the status is also logged.
export QUARTUS_ROOTDIR=${QUARTUS_ROOTDIR:-/home/deano/intelFPGA_lite/17.0/quartus}
here=$(cd "$(dirname "$0")" && pwd)
cd "$1" || exit 1
exec "$here/run_quartus_job.sh" compile.log "$QUARTUS_ROOTDIR/bin/quartus_sh" --flow compile top
