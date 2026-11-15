#!/bin/bash
# zx81q_fit.sh <project-dir>: prototype Quartus Lite compile with a 1h cap
export QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus
cd "$1" && timeout 3600 nice -n 10 $QUARTUS_ROOTDIR/bin/quartus_sh --flow compile top > compile.log 2>&1; echo "exit $?" >> compile.log
