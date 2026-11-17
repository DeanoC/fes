# Standard Atari ST uses the same qualified board/reset/fabric shell as
# MENU and the standard ZX81. Select only its memory-fault hierarchy in that
# shared constraint. One PLL on CLOCK1_50 makes the machine clock
# (52.222222 MHz) and the audio master clock (12.287582 MHz); the PLL IP
# declares both. The design treats the machine clock as unrelated to the
# peripheral and pixel clocks, and the PCM crossing as unrelated to the audio
# clock it shares a PLL with: every path between them is one of the crossings
# named here. No clock group is cut, so an unnamed crossing is timed.
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
        error "DE25 Atari ST $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}

# PROVISIONAL, and owed before any export: the ST video adapter and native
# video paths cross between the machine and pixel clocks with request/done
# toggles, held configuration and job bundles and small line caches. They are
# not yet named one by one as the crossings above are. Until they are, every
# machine<->pixel path that no named exception covers is bounded as a held
# bundle (ten nanoseconds, less than either period) instead of being timed
# against an arbitrary edge alignment of two unrelated PLLs. This is a bound,
# not a cut, but it would also pass an unsynchronized crossing shorter than
# ten nanoseconds; naming each crossing removes that.
set st_machine_clock [get_clocks {fes_machine_pll|machine_pll_outclk0}]
set st_pixel_clock [get_clocks {fes_video_inst|video_clock|iopll_0_outclk0}]
if {[get_collection_size $st_machine_clock] != 1 || [get_collection_size $st_pixel_clock] != 1} {
    error "DE25 Atari ST machine or pixel clock changed"
}
foreach {from to} [list $st_machine_clock $st_pixel_clock $st_pixel_clock $st_machine_clock] {
    set_max_delay 10.000 -from $from -to $to
    set_min_delay 0.000 -from $from -to $to
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
        error "DE25 Atari ST held $label bundle endpoints changed"
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
    error "DE25 Atari ST held gp_reply bundle endpoints changed"
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
if {[get_collection_size $machine_release] != 2} {error "DE25 Atari ST machine reset release endpoints changed"}
set_false_path -to $machine_release

# Coherent PCM: the sample is held in the machine clock until the audio clock
# has copied it, and the request/acknowledge toggles and the mute level each
# enter one first stage.
set audio_sample [get_registers {fes_audio_inst|audio|held_sample[*]}]
set audio_pcm [get_registers {fes_audio_inst|audio|pcm[*]}]
if {[get_collection_size $audio_sample] == 0 || [get_collection_size $audio_sample] > 32 ||
    [get_collection_size $audio_pcm] == 0 || [get_collection_size $audio_pcm] > 32} {
    error "DE25 Atari ST PCM bundle endpoints changed"
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
        error "DE25 Atari ST $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}
# Mute is execution hold or a scanout clock fault, each a registered level
# from its own clock, into one first stage.
set audio_hold [get_pins -compatibility_mode -nocase {fes_audio_inst|audio|hold_meta|d}]
if {[get_collection_size $audio_hold] != 1} {error "DE25 Atari ST PCM mute ingress changed"}
set_false_path -to $audio_hold
# PLL lock asynchronously asserts the two-register audio reset-release chain.
# Its output -> serializer reset and all later recovery/removal stay timed.
set audio_release [get_registers {fes_audio_inst|audio|lock_meta fes_audio_inst|audio|lock_sync}]
set audio_clear [get_pins -compatibility_mode -nocase -nowarn {fes_audio_inst|audio|lock_meta|aclr fes_audio_inst|audio|lock_meta|clrn fes_audio_inst|audio|lock_sync|aclr fes_audio_inst|audio|lock_sync|clrn}]
if {[get_collection_size $audio_release] != 2 || [get_collection_size $audio_clear] != 2} {
    error "DE25 Atari ST audio reset-release endpoints changed"
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
        error "DE25 Atari ST I2S $label endpoints changed"
    }
    create_generated_clock -name "de25_i2s_${label}" -source $source_clock -edges $edges $phase
    create_generated_clock -name "de25_i2s_${label}_output" -master_clock "de25_i2s_${label}" \
        -source $divided_clock -divide_by 1 $target
}

# The SDRAM clock pin is the inverted machine clock: the chips sample half a
# machine period after the fabric launches a command. No guessed external
# input/output delays; board flight time remains a physical qualification.
set machine_clock_source [get_pins {fes_machine_pll|machine_pll|tennm_ph2_iopll|out_clk[0]}]
if {[get_collection_size $machine_clock_source] != 1} {error "DE25 Atari ST machine clock source changed"}
create_generated_clock -name DRAM_CLK -source $machine_clock_source -invert [get_ports {DRAM_CLK}]
