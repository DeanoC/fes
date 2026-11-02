# The board/audio PLL outputs are unrelated. Only request/ack and hold source
# launches to the first synchronizer stage are asynchronous. Interstage paths,
# all other paths and PLL lock recovery/removal remain normally timed.
proc audio_regs {pattern minimum maximum} {
 set regs [get_registers $pattern]
 set count [get_collection_size $regs]
 if {$count < $minimum || $count > $maximum} {error "Audio endpoint binding failed: $pattern: $count"}
 foreach_in_collection r $regs {
  set n [get_node_info -name $r]
  if {[regexp {~DUPLICATE[0-9]*$} $n]} {error "Duplicated audio endpoint: $n"}
 }
 return $regs
}
# The Cyclone V router may duplicate this *launch* register. Constrain the
# original and every router-created copy, but reject any other match. The
# post-fit audit verifies every accepted copy against the fitter's duplicate
# provenance and still rejects duplicated synchronizer stages and payload.
set request_name speaker_output_audio_output_stage_request
set request [get_registers $request_name]
set originals 0
set copies 0
foreach_in_collection reg $request {
 set n [get_node_info -name $reg]
 if {$n eq $request_name} {
  incr originals
 } elseif {[regexp {^speaker_output_audio_output_stage_request~DUPLICATE[0-9]*$} $n]} {
  incr copies
 } else {error "Unexpected audio request register: $n"}
}
if {$originals != 1 || $copies > 16} {error "Audio request binding failed: $originals originals, $copies copies"}
set acknowledge [audio_regs {speaker_output_audio_output_stage_acknowledge} 1 1]
set request_head [audio_regs {speaker_output_audio_output_stage_request_meta} 1 1]
set ack_head [audio_regs {speaker_output_audio_output_stage_ack_meta} 1 1]
set hold_head [audio_regs {speaker_output_audio_output_stage_hold_meta} 1 1]
set_false_path -from $request -to $request_head
set_false_path -from $acknowledge -to $ack_head
foreach name {system_machine_reset {sys_reset[1]} fes_computer_runtime_pcxt_floppy_gp_mailbox_core_exec_reset} {
 set source [audio_regs $name 1 1]
 set_false_path -from $source -to $hold_head
}
# held_sample is published with the ack; pcm captures only after two
# destination synchronizer edges. Bound all physical payload paths to one
# audio clock period, leaving the second period of protocol headroom.
set held [audio_regs {speaker_output_audio_output_stage_held_sample[*]} 1 32]
set pcm [audio_regs {speaker_output_audio_output_stage_pcm[*]} 1 32]
set clocks [get_clocks {speaker_output_audio_clock_pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk}]
if {[get_collection_size $clocks] != 1} {error "Audio PLL clock binding failed"}
set period [get_clock_info -period $clocks]
set_max_delay $period -from $held -to $pcm
set_min_delay 0 -from $held -to $pcm
puts "AUDIO_CONTRACT: five asynchronous source pairs; bounded held payload; max delay $period ns"
