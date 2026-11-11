#!/usr/bin/env bash
# S1c i386 leg: Yosys-elaborated netlist -> quartus_map -> quartus_eda functional .vo.
# usage: i386_vo.sh <top: fes_i386_cpu|spike_cpu> <work dir> [extra QSF line ...]
# env: REPO (sources/misteross), YOSYS (the i386 lock's yosys), QUARTUS_ROOTDIR (.../17.0/quartus)
set -euo pipefail
top=$1; work=$2; shift 2
RTL=$REPO/cores/fes-common/rtl/i386
SPIKE=$REPO/cores/fes-common/sim/i386/spike
# benchmark_fes_i386.CPU_RTL
CPU_RTL="fes_i386_pkg.sv fes_i386_oplen.sv fes_i386_lendec.sv fes_i386_lstage.sv fes_i386_btb.sv fes_i386_ras.sv
 fes_i386_fetchbuf.sv fes_i386_qalign.sv fes_i386_frontend.sv fes_i386_gov_pkg.sv fes_i386_amode.sv fes_i386_dfwd.sv
 fes_i386_agu.sv fes_i386_ucode_pkg.sv fes_i386_ucode_rom.sv fes_i386_decode.sv fes_i386_seq.sv fes_i386_dops.sv
 fes_i386_shift.sv fes_i386_shflags.sv fes_i386_exctl.sv fes_i386_muldiv.sv fes_i386_ex.sv fes_i386_tlb.sv
 fes_i386_l1i.sv fes_i386_l1d.sv fes_i386_biu.sv fes_i386_gov_rom.sv fes_i386_governor.sv fes_i386_cpu.sv"
files=""
for f in $CPU_RTL; do files="$files $RTL/$f"; done
pins=""
clk=clk
if [ "$top" = spike_cpu ]; then
  files="$files $SPIKE/spike_stimulus.sv $SPIKE/spike_cpu.sv"
  pins=$(cat "$SPIKE/spike.qsf")
  clk=FPGA_CLK1_50
fi
mkdir -p "$work"
cd "$work"
# The netlist program of quartus_fes_i386.py and test_fes_i386_frontend.synthesize_netlist.
"$YOSYS" -q -p "read_verilog -sv $files; hierarchy -top $top; proc -norom; flatten; opt -purge; write_verilog -noattr $top.v" > yosys.log 2>&1
echo "PROJECT_REVISION = \"$top\"" > "$top.qpf"
{
  echo 'set_global_assignment -name FAMILY "Cyclone V"'
  echo 'set_global_assignment -name DEVICE 5CSEBA6U23I7'
  echo "set_global_assignment -name TOP_LEVEL_ENTITY $top"
  echo 'set_global_assignment -name NUM_PARALLEL_PROCESSORS 2'
  echo "set_global_assignment -name VERILOG_FILE $top.v"
  echo "set_global_assignment -name SDC_FILE $top.sdc"
  for line in "$@"; do echo "$line"; done
  echo "$pins"
} > "$top.qsf"
echo "create_clock -name clk -period 10.000 [get_ports {$clk}]" > "$top.sdc"
B=$QUARTUS_ROOTDIR/bin
"$B/quartus_map" "$top" > map.log 2>&1
"$B/quartus_eda" "$top" --simulation --tool=modelsim --format=verilog --functional=on > eda.log 2>&1
ls -l simulation/modelsim/
