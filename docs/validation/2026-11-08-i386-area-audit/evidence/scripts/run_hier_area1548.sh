#!/bin/bash
# run_hier_area1548.sh TAG: hierarchical analysis build of wt-TAG
A=~/fes-scratch/area1548; t=$1
export CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus
cd $A/wt-$t/sources/misteross
MAKEFLAGS= MFLAGS= nice -n 5 python3 $A/hier_build_area1548.py "$PWD" > $A/hier-$t.log 2>&1
echo "exit $? $(date -Is)" >> $A/hier-$t.log
