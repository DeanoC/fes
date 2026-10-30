# SPDX-License-Identifier: GPL-2.0-or-later
# Capture native timing without waiving clocks, paths or uncovered interfaces.
package require ::quartus::project
package require ::quartus::sta
project_open top
create_timing_netlist
read_sdc
update_timing_netlist
file mkdir audit
set clocks [get_clocks *]
set inv [open audit/clocks.tsv w]
puts $inv "name\tperiod_ns\twaveform_ns\ttype\tpositive_registers\tnegative_registers"
foreach_in_collection clock $clocks {
 puts $inv [join [list [get_clock_info -name $clock] [get_clock_info -period $clock] [get_clock_info -waveform $clock] [get_clock_info -type $clock] [get_clock_info -nreg_pos $clock] [get_clock_info -nreg_neg $clock]] \t]
}
close $inv
report_clocks -file audit/clocks.rpt
report_sdc -file audit/constraints.rpt
report_sdc -ignored -file audit/ignored-constraints.rpt
report_ucp -file audit/unconstrained.rpt
check_timing -include {no_clock multiple_clock generated_clock loops latches no_input_delay no_output_delay partial_input_delay partial_output_delay io_min_max_delay_consistency reference_pin pll_cross_check uncertainty} -file audit/check-timing.rpt
set counts [open audit/counts.tsv w]
puts $counts "corner\tkind\tfrom_clock\tto_clock\tcount"
set intervals [open audit/endpoints.tsv w]
puts $intervals "corner\tkind\tfrom_clock\tto_clock\tfrom_inverted\tto_inverted\tfrom\tto\tslack_ns\trelationship_ns\tdata_delay_ns"
set fm [open audit/fmax.tsv w]
puts $fm "corner\tclock\tfmax_mhz\trestricted_fmax_mhz"
set mailbox_contract [file exists mailbox.sdc]
if {$mailbox_contract} {
 set interstage [open audit/mailbox-interstage.tsv w]
 puts $interstage "corner\tkind\tfrom_clock\tto_clock\tfrom_inverted\tto_inverted\tfrom\tto\tslack_ns\trelationship_ns\tdata_delay_ns"
}
set limit 100000
proc emit_paths {paths corner kind stream} {
 foreach_in_collection p $paths {
  set clocks {}
  foreach side {from to} {
   set id [get_path_info $p -${side}_clock]
   if {$id eq ""} {lappend clocks ""} else {lappend clocks [get_clock_info -name $id]}
  }
  puts $stream [join [list $corner $kind {*}$clocks [get_path_info $p -from_clock_is_inverted] [get_path_info $p -to_clock_is_inverted] [get_node_info -name [get_path_info $p -from]] [get_node_info -name [get_path_info $p -to]] [get_path_info $p -slack] [get_path_info $p -clock_relationship] [get_path_info $p -data_delay]] \t]
 }
}
foreach corner [get_available_operating_conditions] {
 set_operating_conditions $corner
 update_timing_netlist
 set folder audit/$corner
 file mkdir $folder
 report_clock_transfers -file $folder/clock-transfers.rpt
 report_min_pulse_width -nworst $limit -detail full_path -file $folder/pulse-width.rpt
 foreach row [get_clock_fmax_info] {puts $fm [join [list $corner {*}$row] \t]}
 foreach kind {setup hold recovery removal} {
  report_timing -$kind -npaths 20 -nworst 1 -detail full_path -file $folder/worst-$kind.rpt
  if {$kind eq "setup" || $kind eq "hold"} {
   foreach_in_collection from $clocks {
    foreach_in_collection to $clocks {
     set paths [get_timing_paths -$kind -from_clock $from -to_clock $to -npaths $limit -nworst 1]
     set count [get_collection_size $paths]
     if {$count >= $limit} {error "Endpoint capture limit reached"}
     puts $counts [join [list $corner $kind [get_clock_info -name $from] [get_clock_info -name $to] $count] \t]
     emit_paths $paths $corner $kind $intervals
    }
   }
  } else {
   set paths [get_timing_paths -$kind -npaths $limit -nworst 1]
   set count [get_collection_size $paths]
   if {$count >= $limit} {error "Endpoint capture limit reached"}
   puts $counts [join [list $corner $kind * * $count] \t]
   emit_paths $paths $corner $kind $intervals
  }
 }
 if {$mailbox_contract} {
  foreach role {ack sent hold} {
   foreach kind {setup hold} {
    set paths [get_timing_paths -$kind -from system_video_adapter_${role}_meta -to system_video_adapter_${role}_sync -npaths 16 -nworst 1]
    if {[get_collection_size $paths] != 1} {error "Missing or duplicated mailbox interstage path: $role $kind"}
    emit_paths $paths $corner $kind $interstage
   }
  }
 }
}
close $counts
close $intervals
close $fm
if {$mailbox_contract} {close $interstage}
puts "PCXT_AUDIT_CAPTURE_COMPLETE"
delete_timing_netlist
project_close
