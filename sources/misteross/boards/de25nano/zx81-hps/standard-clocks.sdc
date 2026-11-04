# Standard ZX81 uses the same qualified board/reset/fabric shell as MENU.
# Select only its changed memory-fault hierarchy in that shared constraint.
set fes_de25_standard_endpoint {soc_inst|subsys_periph|fes_scanout|fes_scanout}
source [file join [file dirname [info script]] clocks.sdc]
unset fes_de25_standard_endpoint

# Explicit launch registers and first-stage D pins. Interstage paths and all
# downstream logic retain their ordinary receiving-clock setup/hold checks.
foreach {label source expected head second} {
    readiness {video_ready_source} 1
        {transport|video_ready_sync[0]} {transport|video_ready_sync[1]}
    execution {core|gp_mailbox|exec_reset} 1
        {display|execution_sync[0]} {display|execution_sync[1]}
    drained {display|quiesced_source} 1
        {display|quiesced_sync[0]} {display|quiesced_sync[1]}
    request {display|crossing|request_tag} 1
        {display|crossing|request_sync[0]} {display|crossing|request_sync[1]}
    response {display|crossing|response_tag} 1
        {display|crossing|response_sync[0]} {display|crossing|response_sync[1]}
} {
    set base {soc_inst|subsys_periph|fes_scanout|fes_scanout}
    set patterns {}
    foreach launch $source {
        if {![string match {fes_video_inst|*} $launch]} {set launch "${base}|${launch}"}
        lappend patterns $launch
    }
    set launches [get_registers $patterns]
    set first_d [get_pins -compatibility_mode -nocase "${base}|${head}|d"]
    set next_stage [get_registers "${base}|${second}"]
    if {[get_collection_size $launches] != $expected ||
        [get_collection_size $first_d] != 1 || [get_collection_size $next_stage] != 1} {
        error "DE25 standard $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}

# Bundles are held throughout their toggle round trip. A ten-nanosecond flight
# is at most one fast system period, less than either two-stage control delay.
# Keep physical net, data, skew and min/max path bounds, rather than cutting
# the payloads or declaring the complete clocks asynchronous.
foreach {label source sink expected} {
    command {payload[*]} {pixel_opcode[*] pixel_index[*] pixel_argument[*]} 31
    reply {returned_payload[*]} {response_error response_data[*]} 17
} {
    set base {soc_inst|subsys_periph|fes_scanout|fes_scanout|display|crossing}
    set launches [get_registers "${base}|${source}"]
    set patterns {}
    foreach name $sink {lappend patterns "${base}|${name}"}
    set captures [get_registers $patterns]
    if {[get_collection_size $launches] != $expected || [get_collection_size $captures] != $expected} {
        error "DE25 standard held $label bundle endpoints changed"
    }
    set_net_delay -max 10.000 -from $launches -to $captures
    set_data_delay 10.000 -from $launches -to $captures
    set_max_skew 10.000 -from $launches -to $captures
    set_max_delay 10.000 -from $launches -to $captures
    set_min_delay 0.000 -from $launches -to $captures
}

# The internal-16K base has no PSG persona: constant PCM removes the sample
# crossing. Adding a sound persona must supply and qualify its coherent bundle.
set audio_payload [get_registers -nowarn {fes_audio_inst|audio|held_sample[*] fes_audio_inst|audio|request_meta fes_audio_inst|audio|ack_meta fes_audio_inst|audio|hold_meta}]
if {[get_collection_size $audio_payload] != 0} {error "DE25 standard base unexpectedly contains a live PCM crossing"}
# PLL lock asynchronously asserts the two-register audio reset-release chain.
# Its output -> serializer reset and all later recovery/removal stay timed.
set audio_release [get_registers {fes_audio_inst|audio|lock_meta fes_audio_inst|audio|lock_sync}]
set audio_clear [get_pins -compatibility_mode -nocase -nowarn {fes_audio_inst|audio|lock_meta|aclr fes_audio_inst|audio|lock_meta|clrn fes_audio_inst|audio|lock_sync|aclr fes_audio_inst|audio|lock_sync|clrn}]
if {[get_collection_size $audio_release] != 2 || [get_collection_size $audio_clear] != 2} {
    error "DE25 standard audio reset-release endpoints changed"
}
set_false_path -to $audio_clear

# These output pins carry divided clocks, not synchronous payload data. Their
# source is the actual audio-domain counter clock. Starting with phase=0,
# the first increment is on source edge 1: phase[1] first rises on edge 3,
# falls on edge 7, and rises again on edge 11. Likewise phase[7] uses edges
# 255, 511, 767. Both periods are unchanged, but these edges retain the real
# relationship: LRCLK changes on a falling SCLK edge. Plain -divide_by would
# incorrectly align their rising edges. The base PCM data output is constant.
foreach {label bit edges port} {
    bit_clock 1 {3 7 11} HDMI_SCLK
    word_clock 7 {255 511 767} HDMI_LRCLK
} {
    set phase_name [format {fes_audio_inst|audio|serializer|phase[%d]} $bit]
    set phase [get_registers $phase_name]
    set source_clock [get_pins -compatibility_mode -nocase "${phase_name}|clk"]
    set divided_clock [get_pins -compatibility_mode -nocase "${phase_name}|q"]
    set target [get_ports $port]
    if {[get_collection_size $phase] != 1 || [get_collection_size $source_clock] != 1 ||
        [get_collection_size $divided_clock] != 1 || [get_collection_size $target] != 1} {
        error "DE25 standard I2S $label endpoints changed"
    }
    create_generated_clock -name "de25_i2s_${label}" -source $source_clock -edges $edges $phase
    # The counter divides the clock; the register-Q -> output-pin path only
    # forwards it. Model both so the physical output has a clock assignment.
    create_generated_clock -name "de25_i2s_${label}_output" -master_clock "de25_i2s_${label}" \
        -source $divided_clock -divide_by 1 $target
}
