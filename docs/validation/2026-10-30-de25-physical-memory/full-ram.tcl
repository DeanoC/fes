set design [design_load /home/deano/.codex/worktrees/c134/fes/out/dev/de25-physical-memory/out/hardware/ramtest-with-vendor-spl.sof]
set device {/devices/A5E(A013BB23B|B013BB23BCS)|..@1#3-11.3-iface0#DE25-Nano}
if {[lsearch -exact [get_service_paths device] $device] < 0} {error "claimed device absent"}
design_link $design $device
set service {/devices/A5E(A013BB23B|B013BB23BCS)|..@1#3-11.3-iface0#DE25-Nano/(link)/JTAG/alt_sld_fab_0_alt_sld_fab_0_sldfabric.node_0/phy_0/subsys_debug_fpga_m.master}
if {[lsearch -exact [get_service_paths master] $service] < 0} {error "explicit fabric CSR service absent"}
source /home/deano/.codex/worktrees/c134/fes/out/dev/de25-physical-memory/sources/misteross/boards/de25nano/ramtest/jtag.tcl
foreach {bank length} {fpga 1073741824 sdram 134217728} {
 puts "FULL_BEGIN $bank length=$length"
 set result [de25ramtest::run $service 6ee668470911276a9e553d0025c7ef0b $bank 900000 100 $length]
 puts [list FULL_RESULT $bank $result]
 if {[dict get $result outcome] ne "PASS"} {error "full-bank $bank diagnostic failed; preserve snapshot"}
}
puts "BOTH_LOCAL_BANK_FULL_SCANS_PASS"
exit
