package require ::quartus::project
package require ::quartus::sta
project_open top
create_timing_netlist
read_sdc
# Synthetic output constraint makes every preserved FF's Q observable in the
# analysis. Its phase is not a board claim; only the fitted local FF arc is read.
create_clock -name ff_characterizer_only -period 20.0
set_output_delay -clock ff_characterizer_only -max 0.0 [get_ports probes*]
set_output_delay -clock ff_characterizer_only -min 0.0 [get_ports probes*]
foreach corner [get_available_operating_conditions] {
 set_operating_conditions $corner
 update_timing_netlist
 file mkdir ff-clock-to-q/$corner
 foreach analysis {setup hold} {
  foreach edge {rise fall} {
   report_timing -$analysis -from [get_registers sink*] -${edge}_to [get_ports probes*] -npaths 1000 -nworst 1 -detail full_path -show_routing -file ff-clock-to-q/$corner/$analysis-$edge.rpt
  }
 }
}
project_close
