#!/bin/bash
# eda_netlist.sh <fitted-project>: write the post-fit Verilog netlist
# (<project>/simulation/modelsim/top.vo) through run_quartus_job.sh, which
# gives quartus_eda the 30 GB preflight, one-hour cap, heartbeat and
# FINISHED/FAILED line. Exits with quartus_eda's status.
export QUARTUS_ROOTDIR=${QUARTUS_ROOTDIR:-/home/deano/intelFPGA_lite/17.0/quartus}
here=$(cd "$(dirname "$0")" && pwd)
cd "$1" || exit 1
exec "$here/run_quartus_job.sh" eda.log "$QUARTUS_ROOTDIR/bin/quartus_eda" top -c top \
    --simulation --tool=modelsim --format=verilog
