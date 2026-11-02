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
set audio_contract [file exists audio-audit.enabled]
if {$audio_contract} {
 set payload_regs [open audit/audio-payload-registers.tsv w]
 puts $payload_regs "role\tname"
 foreach role {held_sample pcm} {
  set pattern [format {speaker_output_audio_output_stage_%s[*]} $role]
  set payload($role) [get_registers $pattern]
  set size [get_collection_size $payload($role)]
  if {$size < 1 || $size > 32} {error "Missing or duplicated audio payload: $role"}
  foreach_in_collection reg $payload($role) {
   set name [get_node_info -name $reg]
   if {[regexp {~DUPLICATE[0-9]*$} $name]} {error "Duplicated audio payload: $name"}
   puts $payload_regs "$role\t$name"
  }
 }
 close $payload_regs
 set audio_payload [open audit/audio-payload.tsv w]
 puts $audio_payload "corner\tkind\tfrom_clock\tto_clock\tfrom_inverted\tto_inverted\tfrom\tto\tslack_ns\trelationship_ns\tdata_delay_ns"
 set audio_interstage [open audit/audio-interstage.tsv w]
 puts $audio_interstage "corner\tkind\tfrom_clock\tto_clock\tfrom_inverted\tto_inverted\tfrom\tto\tslack_ns\trelationship_ns\tdata_delay_ns"
 foreach role {request ack hold lock} {
  foreach stage {meta sync} {
   set name speaker_output_audio_output_stage_${role}_${stage}
   set regs [get_registers ${name}*]
   if {[get_collection_size $regs] != 1} {error "Missing or duplicated audio synchronizer: $name"}
   foreach_in_collection reg $regs {
    if {[get_node_info -name $reg] ne $name} {error "Audio synchronizer binding changed: $name"}
   }
  }
 }
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
 if {$audio_contract} {
  foreach kind {setup hold} {
   set paths [get_timing_paths -$kind -from $payload(held_sample) -to $payload(pcm) -npaths 128 -nworst 1]
   if {[get_collection_size $paths] != [get_collection_size $payload(pcm)]} {error "Incomplete audio payload coverage: $kind"}
   emit_paths $paths $corner $kind $audio_payload
  }
  foreach role {request ack hold lock} {
   foreach kind {setup hold} {
    set paths [get_timing_paths -$kind -from speaker_output_audio_output_stage_${role}_meta -to speaker_output_audio_output_stage_${role}_sync -npaths 16 -nworst 1]
    if {[get_collection_size $paths] != 1} {error "Missing or duplicated audio interstage path: $role $kind"}
    emit_paths $paths $corner $kind $audio_interstage
   }
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
if {$audio_contract} {close $audio_interstage; close $audio_payload}
puts "PCXT_AUDIT_CAPTURE_COMPLETE"
delete_timing_netlist
project_close
