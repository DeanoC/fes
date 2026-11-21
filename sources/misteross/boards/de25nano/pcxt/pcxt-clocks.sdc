# Standard PC/XT uses the same qualified board/reset/fabric shell as MENU and
# the standard ZX81. Select only its memory-fault hierarchy in that shared
# constraint. One PLL on CLOCK1_50 makes the machine clock (100 MHz), the
# audio master clock (12.295082 MHz) and the three SDRAM clocks (100 MHz:
# forward +1.25 ns, handoff +9.25 ns, capture +4.0 ns); the PLL IP declares
# them all. The design treats the machine clock as unrelated to the
# peripheral and pixel clocks, and the PCM crossing as unrelated to the audio
# clock it shares a PLL with: every path between them is one of the crossings
# named here. The SDRAM clocks are the machine clock's phases and every path
# between them is timed. No clock group is cut, so an unnamed crossing is
# timed.
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
        error "DE25 PC/XT $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}

# The PC/XT video crosses between the machine and pixel clocks through one
# ownership mailbox (pcxt_video): the sent and received toggles and the
# machine hold level, each one launch register into one first stage. No live
# pixel data crosses; the framebuffer is a dual-clock memory.
foreach {label source head} {
    video_sent {core|system|video|adapter|sent} {core|system|video|adapter|sent_meta}
    video_received {core|system|video|adapter|received} {core|system|video|adapter|ack_meta}
    video_hold {core|system|machine_reset} {core|system|video|adapter|hold_meta}
} {
    set launches [get_registers "${base}|${source}"]
    set first_d [get_pins -compatibility_mode -nocase "${base}|${head}|d"]
    if {[get_collection_size $launches] != 1 || [get_collection_size $first_d] != 1} {
        error "DE25 PC/XT $label crossing endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}

# The published bank is held from its sent toggle until the pixel clock's
# received toggle returns, and the pixel clock adopts it only at an output
# frame start at least two pixel periods after the toggle arrives. Into the
# pixel clock it reaches the front bank and the read address, which synthesis
# places in the framebuffer memories' address registers, each through the
# adoption select: there is no direct net for a net-delay bound. One pixel
# period bounds the data delay and skew into the front bank and every path
# into the pixel clock, as the DE10 mailbox constraint bounds it into the
# front bank; the bound counts the skew between the two PLL networks.
set published [get_registers "${base}|core|system|video|adapter|published_bank\[*\]"]
set front [get_registers "${base}|core|system|video|adapter|front\[*\]"]
set pixel_clock [get_clocks {fes_video_inst|video_clock|iopll_0_outclk0}]
if {[get_collection_size $published] != 2 || [get_collection_size $front] != 2 ||
    [get_collection_size $pixel_clock] != 1} {
    error "DE25 PC/XT held video bank endpoints changed"
}
set_data_delay 13.400 -from $published -to $front
set_max_skew 13.400 -from $published -to $front
set_max_delay 13.400 -from $published -to $pixel_clock
set_min_delay 0.000 -from $published -to $pixel_clock

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
        error "DE25 PC/XT held $label bundle endpoints changed"
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
    set launches [remove_from_collection [get_registers "${base}|gp_crossing|${source}\[*\]"] \
        [get_registers "${base}|gp_crossing|${source}\[$toggle\]"]]
    set captures [remove_from_collection [get_registers "${base}|gp_crossing|${sink}\[*\]"] \
        [get_registers "${base}|gp_crossing|${sink}\[$toggle\]"]]
    if {[get_collection_size $launches] != $launches_expected ||
        [get_collection_size $captures] < $captures_minimum || [get_collection_size $captures] > 31} {
        error "DE25 PC/XT held $label bundle endpoints changed"
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
set machine_release [get_pins -compatibility_mode -nocase -nowarn "${base}|machine_release\[*\]|aclr ${base}|machine_release\[*\]|clrn"]
if {[get_collection_size $machine_release] != 2} {error "DE25 PC/XT machine reset release endpoints changed"}
set_false_path -to $machine_release

# Coherent PCM: the sample is held in the machine clock until the audio clock
# has copied it, and the request/acknowledge toggles and the mute level each
# enter one first stage.
set audio_sample [get_registers {fes_audio_inst|audio|held_sample[*]}]
set audio_pcm [get_registers {fes_audio_inst|audio|pcm[*]}]
if {[get_collection_size $audio_sample] == 0 || [get_collection_size $audio_sample] > 32 ||
    [get_collection_size $audio_pcm] == 0 || [get_collection_size $audio_pcm] > 32} {
    error "DE25 PC/XT PCM bundle endpoints changed"
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
        error "DE25 PC/XT $label synchronizer endpoints changed"
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
        error "DE25 PC/XT $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}
# PLL lock asynchronously asserts the two-register audio reset-release chain.
# Its output -> serializer reset and all later recovery/removal stay timed.
set audio_release [get_registers {fes_audio_inst|audio|lock_meta fes_audio_inst|audio|lock_sync}]
set audio_clear [get_pins -compatibility_mode -nocase -nowarn {fes_audio_inst|audio|lock_meta|aclr fes_audio_inst|audio|lock_meta|clrn fes_audio_inst|audio|lock_sync|aclr fes_audio_inst|audio|lock_sync|clrn}]
if {[get_collection_size $audio_release] != 2 || [get_collection_size $audio_clear] != 2} {
    error "DE25 PC/XT audio reset-release endpoints changed"
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
        error "DE25 PC/XT I2S $label endpoints changed"
    }
    create_generated_clock -name "de25_i2s_${label}" -source $source_clock -edges $edges $phase
    create_generated_clock -name "de25_i2s_${label}_output" -master_clock "de25_i2s_${label}" \
        -source $divided_clock -divide_by 1 $target
}

# I2S data changes on the falling bit clock and the ADV7513 samples it on the
# rising one, two master-clock periods later. Analog Devices ADV7513 data
# sheet, audio AC specifications: I2S setup tASU >= 2 ns and hold tAHLD >= 2 ns.
# No board trace delay is documented, so the pin budget assumes zero external
# flight time. Setup is checked at the tool's one master period, half the real
# interval. Hold is met by construction: the data changes two master periods
# after the capture edge, never at it, and the tool's same-edge hold check
# describes a launch that does not exist, so that check alone is cut.
set i2s_data [get_ports {HDMI_I2S}]
set i2s_launch [get_registers {fes_audio_inst|audio|serializer|sdata}]
if {[get_collection_size $i2s_data] != 1 || [get_collection_size $i2s_launch] != 1} {
    error "DE25 PC/XT I2S data endpoints changed"
}
set_output_delay -clock de25_i2s_bit_clock_output -max 2.000 $i2s_data
set_output_delay -clock de25_i2s_bit_clock_output -min -2.000 $i2s_data
set_false_path -hold -from $i2s_launch -to $i2s_data
# PLL lock gates the data pin low until the clocks are valid. It is an
# asynchronous level from the PLL's own control register, not serial data.
set i2s_lock [get_registers {fes_machine_pll|machine_pll|tennm_ph2_iopll~pll_ctrl_reg}]
if {[get_collection_size $i2s_lock] != 1} {error "DE25 PC/XT I2S lock gate changed"}
set_false_path -from $i2s_lock -to $i2s_data

# The SDRAM clock pin is the inverted forward clock, +1.25 ns from the
# machine clock: the chips sample a command 6.25 ns after the fabric launches
# it, as on the DE10 board.
set forward_clock_source [get_pins {fes_machine_pll|machine_pll|tennm_ph2_iopll|out_clk[2]}]
if {[get_collection_size $forward_clock_source] != 1} {error "DE25 PC/XT SDRAM forward clock source changed"}
create_generated_clock -name DRAM_CLK -source $forward_clock_source -invert [get_ports {DRAM_CLK}]
# ISSI IS42/45VM16320G data sheet Rev. 0A, Table 11, taking the worst of the
# -5, -6 and -75 grades: command, address and data-in setup 2.0 ns and hold
# 1.0 ns; at CAS latency 3, access time tAC <= 6 ns and data-out hold
# tOH >= 2.1 ns. The -75 grade's 7.5 ns CAS-3 cycle admits the 10 ns clock.
# No board trace delay is documented, so the pin budget assumes zero external
# flight time for the clock and for every signal.
set sdram_outputs [get_ports {DRAM_ADDR[*] DRAM_BA[*] DRAM_CS_n[*] DRAM_CKE DRAM_RAS_n DRAM_CAS_n DRAM_WE_n
    DRAM_LDQM DRAM_UDQM DRAM_DQ[*]}]
set sdram_data [get_ports {DRAM_DQ[*]}]
if {[get_collection_size $sdram_outputs] != 39 || [get_collection_size $sdram_data] != 16} {
    error "DE25 PC/XT SDRAM pins changed"
}
set_output_delay -clock DRAM_CLK -max 2.000 $sdram_outputs
set_output_delay -clock DRAM_CLK -min -1.000 $sdram_outputs
set_input_delay -clock DRAM_CLK -max 6.000 $sdram_data
set_input_delay -clock DRAM_CLK -min 2.100 $sdram_data
# CAS latency 3: the chip launches a READ's word tAC after its second clock
# edge from the command and holds it tOH past the third. The capture
# registers sample it on the capture clock's edge 17.75 ns after the second
# chip edge, in sdram_cas3_capture's read window before the handoff: the
# second capture edge after the launch. The word of the next chip edge must
# not reach that same capture edge.
set sdram_capture [get_registers "${base}|core|dq_sample\[*\]"]
if {[get_collection_size $sdram_capture] != 16} {error "DE25 PC/XT SDRAM capture registers changed"}
set_multicycle_path -setup -end 2 -from [get_clocks DRAM_CLK] -to $sdram_capture
set_multicycle_path -hold -end 1 -from [get_clocks DRAM_CLK] -to $sdram_capture
