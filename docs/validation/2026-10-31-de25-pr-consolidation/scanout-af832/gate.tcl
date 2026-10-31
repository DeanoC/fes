project_open golden_top -revision golden_top
create_timing_netlist
read_sdc
update_timing_netlist
foreach reg {{fes_video_inst|visible_sync[0]} {fes_video_inst|visible_sync[1]} {fes_video_inst|video_active}} {
  foreach {kind flag} {Recovery -recovery Removal -removal} {
    set r [report_timing $flag -to [get_registers $reg] -npaths 1 -detail summary -panel_name "$reg $kind"]
    puts "GATE $reg $kind paths=[lindex $r 0] slack=[lindex $r 1]"
  }
}
project_close
