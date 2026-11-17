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

# The ST video adapter and native capture cross between the machine and pixel
# clocks with request/done toggles and bundles held across each round trip.
# Every such path in the fitted design is named below; nothing else crosses.
# Toggles and levels: one launch register per bit into its own first stage.
foreach {label source head bits} {
    video_hold {core|machine_reset} {core|video|hold_meta} 1
    config_request {core|video|config_request*} {core|video|config_req_meta} 1
    config_ack {core|video|config_ack} {core|video|config_ack_meta} 1
    job_request {core|video|job_request[*]} {core|video|job_req_meta[*]} 2
    job_done {core|video|job_done[*]} {core|video|job_done_meta[*]} 2
    published {core|video|native_capture.capture|published[*]*} {core|video|native_capture.capture|pub_meta[*]} 3
    released {core|video|native_capture.capture|released[*]} {core|video|native_capture.capture|release_meta[*]} 3
} {
    set launches [get_registers "${base}|${source}"]
    set first_d [get_pins -compatibility_mode -nocase "${base}|${head}|d"]
    # Physical synthesis may duplicate a launch register; it never drops one.
    if {[get_collection_size $launches] < $bits || [get_collection_size $first_d] != $bits} {
        error "DE25 Atari ST $label crossing endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}

# Held bundles into the pixel clock. Each is written on the machine edge that
# toggles its handshake, and the pixel side reads it no sooner than two pixel
# edges after the toggle's first stage captured it: 2 x 13.468 = 26.94 ns
# after the launch at the least. One pixel period, half of that window, is
# the bound. It counts the skew between the two PLL networks, which is why
# the ten nanoseconds used for same-source bundles elsewhere is too little.
#   config      the system snapshot, taken with config_ack, loaded when the
#               acknowledge has synchronized or at the next frame
#   job_result  job_ok, written with job_done
foreach {label source sink} {
    config {core|video|config_bundle_sys[*]}
        {core|video|pending_config[*] core|video|active_base[*] core|video|active_resolution[*]
         core|video|active_palette[*]}
    job_result {core|video|job_ok[*]} {core|video|cache_valid[*]}
} {
    set patterns {}
    foreach name $source {lappend patterns "${base}|${name}"}
    set launches [get_registers $patterns]
    set patterns {}
    foreach name $sink {lappend patterns "${base}|${name}"}
    set captures [get_registers $patterns]
    if {[get_collection_size $launches] == 0 || [get_collection_size $captures] == 0} {
        error "DE25 Atari ST held $label bundle endpoints changed"
    }
    set_net_delay -max 13.400 -from $launches -to $captures
    set_data_delay 13.400 -from $launches -to $captures
    set_max_skew 13.400 -from $launches -to $captures
    set_max_delay 13.400 -from $launches -to $captures
    set_min_delay 0.000 -from $launches -to $captures
}
# A line job's resolution is written with job_request and its start word one
# pixel edge later. The system side acts two machine periods after the launch
# at the least, so ten nanoseconds is well inside the window.
set job_launches [get_registers [list "${base}|core|video|job_high[*]" "${base}|core|video|job_start[*][*]"]]
set job_captures [get_registers [list "${base}|core|video|memory_words[*]" "${base}|core|video|legacy_addr[*]"]]
if {[get_collection_size $job_launches] == 0 || [get_collection_size $job_captures] == 0} {
    error "DE25 Atari ST held job bundle endpoints changed"
}
set_net_delay -max 10.000 -from $job_launches -to $job_captures
set_data_delay 10.000 -from $job_launches -to $job_captures
set_max_skew 10.000 -from $job_launches -to $job_captures
set_max_delay 10.000 -from $job_launches -to $job_captures
set_min_delay 0.000 -from $job_launches -to $job_captures

# A published frame's height, standard and publication number are written on
# the machine edge that toggles published. The pixel side chooses among them
# only at a start of frame where pub_sync shows the toggle, two pixel edges
# after pub_meta captured it: 2 x 13.468 = 26.94 ns after the launch at the
# least, as for the bundles above; one pixel period is the bound.
set publication_launches [get_registers [list "${base}|core|video|native_capture.capture|height[*][*]" \
    "${base}|core|video|native_capture.capture|pal_banks[*]" \
    "${base}|core|video|native_capture.capture|publication_number[*][*]"]]
set publication_captures [get_registers [list "${base}|core|video|native_capture.capture|output_height[*]" \
    "${base}|core|video|native_capture.capture|output_pal" \
    "${base}|core|video|native_capture.capture|front_bank[*]" \
    "${base}|core|video|native_capture.capture|released[*]" \
    "${base}|core|video|native_capture.capture|borders|next_pointer[*]" \
    "${base}|core|video|native_capture.capture|borders|remaining[*]"]]
if {[get_collection_size $publication_launches] == 0 || [get_collection_size $publication_captures] == 0} {
    error "DE25 Atari ST held publication bundle endpoints changed"
}
set_net_delay -max 13.400 -from $publication_launches -to $publication_captures
set_data_delay 13.400 -from $publication_launches -to $publication_captures
set_max_skew 13.400 -from $publication_launches -to $publication_captures
set_max_delay 13.400 -from $publication_launches -to $publication_captures
set_min_delay 0.000 -from $publication_launches -to $publication_captures

# job_start_ok is the last of a line job's metadata: st_video_adapter registers
# it two pixel edges after it launches job_request. The system side acts on
# the job on the second machine edge after job_req_meta captures the toggle,
# which is no sooner than two machine periods after the launch. That leaves
# 2 x 19.149 - 2 x 13.468 = 11.36 ns from job_start_ok to the machine
# registers it steers, and eleven is the bound.
set job_ok_launches [get_registers "${base}|core|video|job_start_ok[*]"]
set job_ok_captures [get_registers [list "${base}|core|video|job_done[*]" "${base}|core|video|job_ok[*]" \
    "${base}|core|video|legacy_addr[*]" "${base}|core|video|legacy_req" "${base}|core|video|memory_state.*"]]
if {[get_collection_size $job_ok_launches] != 2 || [get_collection_size $job_ok_captures] == 0} {
    error "DE25 Atari ST job bounds bundle endpoints changed"
}
set_net_delay -max 11.000 -from $job_ok_launches -to $job_ok_captures
set_data_delay 11.000 -from $job_ok_launches -to $job_ok_captures
set_max_skew 11.000 -from $job_ok_launches -to $job_ok_captures
set_max_delay 11.000 -from $job_ok_launches -to $job_ok_captures
set_min_delay 0.000 -from $job_ok_launches -to $job_ok_captures

# Bundles are held throughout their toggle round trip. A ten-nanosecond flight
# is at most one fast system period, less than either two-stage control delay.
# Keep physical net, data, skew and min/max path bounds, rather than cutting
# the payloads or declaring the complete clocks asynchronous.
foreach {label source sink expected} {
    command {display|crossing|payload[*]}
        {display|crossing|pixel_opcode[*] display|crossing|pixel_index[*] display|crossing|pixel_argument[*]} 31
    reply {display|crossing|returned_payload[*]}
        {display|crossing|response_error display|crossing|response_data[*]} 17
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
# The two GP words. Each toggle bit leaves its launch register only into its
# synchronizer (cut above); the receiver rebuilds that bit from the
# synchronizer, so the held bundle is the other 31 bits. The reply word's
# signature and unused bits are constants, so synthesis keeps fewer capture
# registers than launch registers: the error flag and sixteen response bits
# at least.
foreach {label source sink toggle launches_expected captures_minimum} {
    gp_request request_payload machine_gpo 31 31 31
    gp_reply reply_payload gpi 23 31 17
} {
    set launches [remove_from_collection [get_registers "${base}|gp_crossing|${source}[*]"] \
        [get_registers "${base}|gp_crossing|${source}[$toggle]"]]
    set captures [remove_from_collection [get_registers "${base}|gp_crossing|${sink}[*]"] \
        [get_registers "${base}|gp_crossing|${sink}[$toggle]"]]
    if {[get_collection_size $launches] != $launches_expected ||
        [get_collection_size $captures] < $captures_minimum || [get_collection_size $captures] > 31} {
        error "DE25 Atari ST held $label bundle endpoints changed"
    }
    set_net_delay -max 10.000 -from $launches -to $captures
    set_data_delay 10.000 -from $launches -to $captures
    set_max_skew 10.000 -from $launches -to $captures
    set_max_delay 10.000 -from $launches -to $captures
    set_min_delay 0.000 -from $launches -to $captures
}

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
# The scanout clock fault is a registered level from the peripheral clock into
# one machine-clock first stage; mute, execution hold joined with that
# synchronized fault, is one machine-clock register into the audio clock's.
foreach {label source head} {
    scanout_fault {fes_video_inst|fault} {fes_audio_inst|fault_sync[0]}
    pcm_mute {fes_audio_inst|mute} {fes_audio_inst|audio|hold_meta}
} {
    set launches [get_registers $source]
    set first_d [get_pins -compatibility_mode -nocase "${head}|d"]
    if {[get_collection_size $launches] != 1 || [get_collection_size $first_d] != 1} {
        error "DE25 Atari ST $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}
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

# I2S data changes on the falling bit clock and the ADV7513 samples it on the
# rising one, two master-clock periods later. Analog Devices ADV7513 data
# sheet, audio AC specifications: I2S setup tASU >= 2 ns and hold tAHLD >= 2 ns.
# No board trace delay is documented, so the pin budget assumes zero external
# flight time. Setup is left at the tool's one master period, half the real
# interval; hold is checked against the launch one master period after the
# capture edge, where the real one is two.
set i2s_data [get_ports {HDMI_I2S}]
set i2s_launch [get_registers {fes_audio_inst|audio|serializer|sdata}]
if {[get_collection_size $i2s_data] != 1 || [get_collection_size $i2s_launch] != 1} {
    error "DE25 Atari ST I2S data endpoints changed"
}
set_output_delay -clock de25_i2s_bit_clock_output -max 2.000 $i2s_data
set_output_delay -clock de25_i2s_bit_clock_output -min -2.000 $i2s_data
set_multicycle_path -hold -start 1 -from $i2s_launch -to $i2s_data

# The SDRAM clock pin is the inverted machine clock: the chips sample half a
# machine period after the fabric launches a command.
set machine_clock_source [get_pins {fes_machine_pll|machine_pll|tennm_ph2_iopll|out_clk[0]}]
if {[get_collection_size $machine_clock_source] != 1} {error "DE25 Atari ST machine clock source changed"}
create_generated_clock -name DRAM_CLK -source $machine_clock_source -invert [get_ports {DRAM_CLK}]
# ISSI IS42/45VM16320G data sheet Rev. 0A, Table 11, taking the worst of the
# -5, -6 and -75 grades: command, address and data-in setup 2.0 ns and hold
# 1.0 ns; at CAS latency 2, access time tAC <= 8 ns and data-out hold
# tOH >= 2.1 ns. No board trace delay is documented, so the pin budget assumes
# zero external flight time for the clock and for every signal.
set sdram_outputs [get_ports {DRAM_ADDR[*] DRAM_BA[*] DRAM_CS_n[*] DRAM_CKE DRAM_RAS_n DRAM_CAS_n DRAM_WE_n
    DRAM_LDQM DRAM_UDQM DRAM_DQ[*]}]
set sdram_data [get_ports {DRAM_DQ[*]}]
if {[get_collection_size $sdram_outputs] != 39 || [get_collection_size $sdram_data] != 16} {
    error "DE25 Atari ST SDRAM pins changed"
}
set_output_delay -clock DRAM_CLK -max 2.000 $sdram_outputs
set_output_delay -clock DRAM_CLK -min -1.000 $sdram_outputs
set_input_delay -clock DRAM_CLK -max 8.000 $sdram_data
set_input_delay -clock DRAM_CLK -min 2.100 $sdram_data
