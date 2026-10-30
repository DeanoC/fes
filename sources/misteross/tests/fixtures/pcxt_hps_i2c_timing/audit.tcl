package require ::quartus::project
package require ::quartus::sta
project_open top
create_timing_netlist
read_sdc
update_timing_netlist
set out [open pin-inventory.txt w]
foreach_in_collection pin [get_pins -hierarchical *] {puts $out [get_pin_info -name $pin]}
close $out
foreach corner [get_available_operating_conditions] {
 set_operating_conditions $corner
 update_timing_netlist
 file mkdir reports/$corner
 report_clocks -file reports/$corner/clocks.rpt
 report_ucp -file reports/$corner/unconstrained.rpt
 foreach {source target tag} {out_clk HDMI_I2C_SCL SCL out_data HDMI_I2C_SDA SDA} {
  set start [get_pins -hierarchical *hdmi_i2c*|$source]
  foreach edge {rise fall} {
   report_path -from $start -${edge}_to [get_ports $target] -npaths 10 -show_routing -file reports/$corner/$tag-out-$edge-late.rpt
   report_path -min_path -from $start -${edge}_to [get_ports $target] -npaths 10 -show_routing -file reports/$corner/$tag-out-$edge-early.rpt
  }
 }
 foreach {source target tag} {HDMI_I2C_SCL scl SCL HDMI_I2C_SDA sda SDA} {
  set end [get_pins -hierarchical *hdmi_i2c*|$target]
  foreach edge {rise fall} {
   report_path -${edge}_from [get_ports $source] -to $end -npaths 10 -show_routing -file reports/$corner/$tag-in-$edge-late.rpt
   report_path -min_path -${edge}_from [get_ports $source] -to $end -npaths 10 -show_routing -file reports/$corner/$tag-in-$edge-early.rpt
  }
 }
}
project_close
