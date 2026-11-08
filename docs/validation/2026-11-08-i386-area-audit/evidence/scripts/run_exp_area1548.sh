#!/bin/bash
# run_exp_area1548.sh: seal-s1 netlist with Yosys pmux casez marked parallel_case for Quartus (analysis only)
export QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus
cd ~/fes-scratch/area1548/wt-exp/sources/misteross/build/fes-pc386-quartus/project
nice -n 5 $QUARTUS_ROOTDIR/bin/quartus_sh --flow compile top > ../quartus.log 2>&1; echo "exit $? $(date -Is)" >> ../quartus.log
