package require ::quartus::project
package require ::quartus::sta
project_open top
create_timing_netlist
read_sdc
# Synthetic input constraint exposes each preserved FF DATAIN.
# Its phase is not a board claim; only the fitted local FF arcs are read.
create_clock -name ff_characterizer_only -period 20.0
set_input_delay -clock ff_characterizer_only -max 0.0 [get_ports din*]
set_input_delay -clock ff_characterizer_only -min 0.0 [get_ports din*]
foreach corner [get_available_operating_conditions] {
 set_operating_conditions $corner
 update_timing_netlist
 file mkdir ff-data/$corner
 foreach analysis {setup hold} {
  foreach edge {rise fall} {
   report_timing -$analysis -${edge}_from [get_ports din*] -to [get_registers sink*] -npaths 1000 -nworst 1 -detail full_path -show_routing -file ff-data/$corner/$analysis-$edge.rpt
  }
 }
}
project_close
