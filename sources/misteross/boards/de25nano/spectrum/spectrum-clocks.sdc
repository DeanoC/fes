# Standard ZX Spectrum uses the same qualified board/reset/fabric shell as
# MENU and the standard ZX81. Select only its memory-fault hierarchy in that
# shared constraint. The machine runs on its own PLL clock (the 100 MHz system
# clock x 47/90, 52.222222 MHz); the PLL IP declares it. The design treats it
# as unrelated to the peripheral clock it is derived from and to the pixel
# clock: every path between those clocks is one of the crossings named here.
# No clock group is cut, so an unnamed crossing is timed, not hidden.
set fes_de25_standard_endpoint {soc_inst|subsys_periph|fes_scanout|fes_scanout}
source [file join [file dirname [info script]] clocks.sdc]
unset fes_de25_standard_endpoint
set base {soc_inst|subsys_periph|fes_scanout|fes_scanout}

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
    transport_health {transport_ready_source} 1
        {transport_ready_sync[0]} {transport_ready_sync[1]}
    gp_request {gp_crossing|request_payload[31]} 1
        {gp_crossing|request_sync[0]} {gp_crossing|request_sync[1]}
    gp_reply {gp_crossing|reply_payload[23]} 1
        {gp_crossing|reply_sync[0]} {gp_crossing|reply_sync[1]}
} {
    set launches [get_registers "${base}|${source}"]
    set first_d [get_pins -compatibility_mode -nocase "${base}|${head}|d"]
    set next_stage [get_registers "${base}|${second}"]
    if {[get_collection_size $launches] != $expected ||
        [get_collection_size $first_d] != 1 || [get_collection_size $next_stage] != 1} {
        error "DE25 Spectrum $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}

# The frame store hands the pixel side a three-bit {toggle, bank} word and
# takes back the two-bit bank on show. Each is one register per bit into its
# own first stage; later stages stay timed in the receiving clock.
foreach {label source head bits} {
    frame_ready {core|video|ready_toggle core|video|ready_bank[*]} {core|video|ready_meta[*]} 3
    frame_shown {core|video|shown_bank[*]} {core|video|shown_meta[*]} 2
} {
    set patterns {}
    foreach name $source {lappend patterns "${base}|${name}"}
    set launches [get_registers $patterns]
    set first_d [get_pins -compatibility_mode -nocase "${base}|${head}|d"]
    if {[get_collection_size $launches] != $bits || [get_collection_size $first_d] != $bits} {
        error "DE25 Spectrum $label crossing endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}

# Bundles are held throughout their toggle round trip. A ten-nanosecond flight
# is at most one fast system period, less than either two-stage control delay.
# Keep physical net, data, skew and min/max path bounds, rather than cutting
# the payloads or declaring the complete clocks asynchronous.
foreach {label source sink expected} {
    command {display|crossing|payload[*]}
        {display|crossing|pixel_opcode[*] display|crossing|pixel_index[*] display|crossing|pixel_argument[*]} 31
    reply {display|crossing|returned_payload[*]}
        {display|crossing|response_error display|crossing|response_data[*]} 17
    gp_request {gp_crossing|request_payload[*]} {gp_crossing|machine_gpo[*]} 32
} {
    set launches [get_registers "${base}|${source}"]
    set patterns {}
    foreach name $sink {lappend patterns "${base}|${name}"}
    set captures [get_registers $patterns]
    if {[get_collection_size $launches] != $expected || [get_collection_size $captures] != $expected} {
        error "DE25 Spectrum held $label bundle endpoints changed"
    }
    set_net_delay -max 10.000 -from $launches -to $captures
    set_data_delay 10.000 -from $launches -to $captures
    set_max_skew 10.000 -from $launches -to $captures
    set_max_delay 10.000 -from $launches -to $captures
    set_min_delay 0.000 -from $launches -to $captures
}
# The reply word's signature and unused bits are constants, so synthesis
# keeps fewer capture registers than launch registers: the acknowledge, the
# error flag and sixteen response bits at least.
set gp_reply_launches [get_registers "${base}|gp_crossing|reply_payload[*]"]
set gp_reply_captures [get_registers "${base}|gp_crossing|gpi[*]"]
if {[get_collection_size $gp_reply_launches] != 32 || [get_collection_size $gp_reply_captures] < 18} {
    error "DE25 Spectrum held gp_reply bundle endpoints changed"
}
set_net_delay -max 10.000 -from $gp_reply_launches -to $gp_reply_captures
set_data_delay 10.000 -from $gp_reply_launches -to $gp_reply_captures
set_max_skew 10.000 -from $gp_reply_launches -to $gp_reply_captures
set_max_delay 10.000 -from $gp_reply_launches -to $gp_reply_captures
set_min_delay 0.000 -from $gp_reply_launches -to $gp_reply_captures

# The peripheral reset asserts the machine-clock release chain at once. Its
# two clear pins are asynchronous ingress; the chain's data path and the
# SDRAM controller's recovery/removal from its output stay timed.
set machine_release [get_pins -compatibility_mode -nocase -nowarn "${base}|machine_release[*]|aclr ${base}|machine_release[*]|clrn"]
if {[get_collection_size $machine_release] != 2} {error "DE25 Spectrum machine reset release endpoints changed"}
set_false_path -to $machine_release

# Coherent PCM: the sample is held in the machine clock until the audio clock
# has copied it, and the request/acknowledge toggles and the mute level each
# enter one first stage.
set audio_sample [get_registers {fes_audio_inst|audio|held_sample[*]}]
set audio_pcm [get_registers {fes_audio_inst|audio|pcm[*]}]
if {[get_collection_size $audio_sample] == 0 || [get_collection_size $audio_sample] > 32 ||
    [get_collection_size $audio_pcm] == 0 || [get_collection_size $audio_pcm] > 32} {
    error "DE25 Spectrum PCM bundle endpoints changed"
}
set_net_delay -max 10.000 -from $audio_sample -to $audio_pcm
set_data_delay 10.000 -from $audio_sample -to $audio_pcm
set_max_skew 10.000 -from $audio_sample -to $audio_pcm
set_max_delay 10.000 -from $audio_sample -to $audio_pcm
set_min_delay 0.000 -from $audio_sample -to $audio_pcm
foreach {label source head} {
    pcm_request {fes_audio_inst|audio|request} {fes_audio_inst|audio|request_meta}
    pcm_acknowledge {fes_audio_inst|audio|acknowledge} {fes_audio_inst|audio|ack_meta}
} {
    set launches [get_registers $source]
    set first_d [get_pins -compatibility_mode -nocase "${head}|d"]
    if {[get_collection_size $launches] != 1 || [get_collection_size $first_d] != 1} {
        error "DE25 Spectrum $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}
# Mute is execution hold or a scanout clock fault, each a registered level
# from its own clock, into one first stage.
set audio_hold [get_pins -compatibility_mode -nocase {fes_audio_inst|audio|hold_meta|d}]
if {[get_collection_size $audio_hold] != 1} {error "DE25 Spectrum PCM mute ingress changed"}
set_false_path -to $audio_hold
# PLL lock asynchronously asserts the two-register audio reset-release chain.
# Its output -> serializer reset and all later recovery/removal stay timed.
set audio_release [get_registers {fes_audio_inst|audio|lock_meta fes_audio_inst|audio|lock_sync}]
set audio_clear [get_pins -compatibility_mode -nocase -nowarn {fes_audio_inst|audio|lock_meta|aclr fes_audio_inst|audio|lock_meta|clrn fes_audio_inst|audio|lock_sync|aclr fes_audio_inst|audio|lock_sync|clrn}]
if {[get_collection_size $audio_release] != 2 || [get_collection_size $audio_clear] != 2} {
    error "DE25 Spectrum audio reset-release endpoints changed"
}
set_false_path -to $audio_clear

# These output pins carry divided clocks, not synchronous payload data; see
# the standard ZX81 constraints for the edge derivation.
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
        error "DE25 Spectrum I2S $label endpoints changed"
    }
    create_generated_clock -name "de25_i2s_${label}" -source $source_clock -edges $edges $phase
    create_generated_clock -name "de25_i2s_${label}_output" -master_clock "de25_i2s_${label}" \
        -source $divided_clock -divide_by 1 $target
}

# The SDRAM clock pin is the inverted machine clock: the chips sample half a
# machine period after the fabric launches a command. No guessed external
# input/output delays; board flight time remains a physical qualification.
set machine_clock_source [get_pins {fes_machine_pll|iopll_0|tennm_ph2_iopll|out_clk[0]}]
if {[get_collection_size $machine_clock_source] != 1} {error "DE25 Spectrum machine clock source changed"}
create_generated_clock -name DRAM_CLK -source $machine_clock_source -invert [get_ports {DRAM_CLK}]
