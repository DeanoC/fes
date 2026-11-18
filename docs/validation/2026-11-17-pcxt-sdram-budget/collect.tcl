# Diagnostic only. Run on a private copy of the existing production fit.
package require ::quartus::project
package require ::quartus::sta
project_open top
create_timing_netlist
read_sdc
set source_pin [get_pins {memory_clock|general[0].gpll~PLL_OUTPUT_COUNTER|divclk}]
if {[get_collection_size $source_pin] != 1} {error "SDRAM clock source binding failed"}
set clock_port [get_ports SDRAM_CLK]
if {[get_collection_size $clock_port] != 1} {error "SDRAM clock pad binding failed"}
# Controller altddio_out drives 0 on the rising half, 1 on the falling half.
create_generated_clock -name SDRAM_FWD -source $source_pin -divide_by 1 -invert $clock_port
set dq [get_ports {SDRAM_DQ[*]}]
if {[get_collection_size $dq] != 16} {error "SDRAM DQ binding failed"}
set outputs [get_ports {SDRAM_A[*] SDRAM_BA[*] SDRAM_DQ[*] SDRAM_nCS SDRAM_nRAS SDRAM_nCAS SDRAM_nWE SDRAM_CKE SDRAM_DQML SDRAM_DQMH}]
if {[get_collection_size $outputs] != 38} {error "SDRAM output binding failed"}
# Etron EM63B165 -6 CL2 reference, zero PCB flight/skew. Not kit bounds.
set_input_delay -clock SDRAM_FWD -max 6.0 $dq
set_input_delay -clock SDRAM_FWD -min 2.5 $dq
set_output_delay -clock SDRAM_FWD -max 1.5 $outputs
set_output_delay -clock SDRAM_FWD -min -0.8 $outputs
# Re-derive including the newly declared pin clock; keep uncertainty active.
derive_clock_uncertainty
update_timing_netlist
file mkdir conditional
report_clocks -file conditional/clocks.rpt
report_sdc -file conditional/constraints.rpt
report_sdc -ignored -file conditional/ignored.rpt
set data_regs [get_registers {dq_out[*]}]
if {[get_collection_size $data_regs] < 1 || [get_collection_size $data_regs] > 16} {error "DQ data register binding failed"}
set out [open conditional/channels.tsv w]
puts $out "corner\trole\tkind\tport\tcount\tfrom\tto\tslack_ns\tdata_delay_ns\trelationship_ns\tlaunch_time_ns\tarrival_time_ns\trequired_time_ns\tclock_skew_ns"
proc emit {out corner role kind port paths} {
 set count [get_collection_size $paths]
 if {$count == 0} {puts $out "$corner\t$role\t$kind\t$port\t0\t\t\t\t\t"; return}
 foreach_in_collection p $paths {
  puts $out [join [list $corner $role $kind $port $count [get_node_info -name [get_path_info $p -from]] [get_node_info -name [get_path_info $p -to]] [get_path_info $p -slack] [get_path_info $p -data_delay] [get_path_info $p -clock_relationship] [get_path_info $p -launch_time] [get_path_info $p -arrival_time] [get_path_info $p -required_time] [get_path_info $p -clock_skew]] \t]
 }
}
foreach corner [get_available_operating_conditions] {
 set_operating_conditions $corner
 update_timing_netlist
 file mkdir conditional/$corner
 foreach_in_collection port $dq {
  set name [get_port_info -name $port]
  foreach kind {setup hold} {
   emit $out $corner read $kind $name [get_timing_paths -$kind -from $port -npaths 1 -nworst 1]
  }
 }
 foreach_in_collection port $outputs {
  set name [get_port_info -name $port]
  foreach kind {setup hold} {
   if {[string first {SDRAM_DQ[} $name] == 0} {
    emit $out $corner write $kind $name [get_timing_paths -$kind -from $data_regs -to $port -npaths 1 -nworst 1]
   } else {
    emit $out $corner write $kind $name [get_timing_paths -$kind -to $port -npaths 1 -nworst 1]
   }
  }
 }
 foreach_in_collection port $dq {
  set name [get_port_info -name $port]
  foreach kind {setup hold} {
   emit $out $corner output_enable $kind $name [get_timing_paths -$kind -from [get_registers {dq_oe*}] -to $port -npaths 1 -nworst 1]
  }
 }
 foreach kind {setup hold} {
  report_timing -$kind -from $dq -npaths 16 -nworst 1 -detail full_path -file conditional/$corner/read-$kind.rpt
  report_timing -$kind -to $outputs -npaths 38 -nworst 1 -detail full_path -file conditional/$corner/write-$kind.rpt
 }
 report_min_pulse_width -nworst 1000 -file conditional/$corner/internal-pulse.rpt
}
# Conditional second-rank inverter reference only: 1..4.2ns at 3.3V +/-0.3V,
# CL=50pF, -40..85C. This is not a fitted-part/load/flight measurement.
set cs [get_ports SDRAM_nCS]
set_output_delay -clock SDRAM_FWD -max 5.7 $cs
set_output_delay -clock SDRAM_FWD -min 0.2 $cs
foreach corner [get_available_operating_conditions] {
 set_operating_conditions $corner
 update_timing_netlist
 foreach kind {setup hold} {
  emit $out $corner second_chip_select $kind SDRAM_nCS [get_timing_paths -$kind -to $cs -npaths 1 -nworst 1]
  report_timing -$kind -to $cs -npaths 1 -detail full_path -file conditional/$corner/second-chip-select-$kind.rpt
 }
}
close $out
delete_timing_netlist
project_close
