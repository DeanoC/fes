# Regenerated vendor PLL SDC owns the 50MHz references and 100/74.25MHz clocks.
set hdmi_config_clock [get_registers {fes_video_inst|hdmi_config|mI2C_CTRL_CLK}]
if {[get_collection_size $hdmi_config_clock] != 1} {error "DE25 computer I2C clock register changed"}
create_generated_clock -name adv7513_config -source [get_ports CLOCK0_50] -divide_by 5002 $hdmi_config_clock
set computer_aclr [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|aclr}]
set computer_clrn [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|clrn}]
set computer_clear_count [expr {[get_collection_size $computer_aclr] + [get_collection_size $computer_clrn]}]
if {$computer_clear_count != 2} {error "DE25 computer requires two reference HDMI reset ingress pins"}
if {[get_collection_size $computer_aclr] > 0} {set_false_path -to $computer_aclr}
if {[get_collection_size $computer_clrn] > 0} {set_false_path -to $computer_clrn}
post_message -type info "DE25 computer HDMI reset ingress pins: $computer_clear_count (expected2)"
# Analog Devices ADV7513 Hardware User's Guide Rev. 0, p. 12, AC Specifications:
# setup tVSU >= 1.0 ns and hold tVHLD >= 0.7 ns. No board trace delay is
# documented, so the pin budget assumes zero external flight time.
create_generated_clock -name HDMI_TX_CLK -source [get_ports {CLOCK0_50}] \
    -multiply_by 297 -divide_by 200 [get_ports {HDMI_TX_CLK}]
set hdmi_video_ports [get_ports {HDMI_TX_D[*] HDMI_TX_DE HDMI_TX_HS HDMI_TX_VS}]
set_output_delay -clock HDMI_TX_CLK -max 1.000 $hdmi_video_ports
set_output_delay -clock HDMI_TX_CLK -min -0.700 $hdmi_video_ports
# The computer runs on the preserved peripheral clock. No new GP control CDC,
# clock groups, FIFO cuts or external HDMI/I2C delay assumptions are introduced.

# Cut only each first synchronizer D, never the second stage or whole clocks.
foreach {label launch first} {
    request {*|video|request} {*|video|request_sync[0]}
    acknowledge {*|video|acknowledge} {*|video|acknowledge_sync[0]}
} {
    set owner_launch [get_registers $launch]
    set owner_first [get_registers $first]
    if {[get_collection_size $owner_launch] != 1 || [get_collection_size $owner_first] != 1} {
        error "DE25 video $label synchronizer endpoints changed"
    }
    set_false_path -from $owner_launch -to $owner_first
}
# Row count is held from before the two-stage request crossing until ACK.
# Its physical flight must fit one system period (less than two pixel periods).
set owner_rows_source [get_registers {*|video|published_rows[*]}]
set owner_rows_sink [get_registers {*|video|front_rows[*]}]
if {[get_collection_size $owner_rows_source] != 9 || [get_collection_size $owner_rows_sink] != 9} {
    error "DE25 video row-count endpoints changed"
}
set_net_delay -max 10.000 -from $owner_rows_source -to $owner_rows_sink
set_data_delay 10.000 -from $owner_rows_source -to $owner_rows_sink
set_max_skew 10.000 -from $owner_rows_source -to $owner_rows_sink
set_max_delay 10.000 -from $owner_rows_source -to $owner_rows_sink
set_min_delay 0.000 -from $owner_rows_source -to $owner_rows_sink
# Raw epoch reset enters two per-domain release chains. Recovery/removal from
# their locally synchronized outputs to owner state remain timed.
set owner_reset_pins [get_pins -compatibility_mode -nocase -nowarn {*|video|sys_release*|aclr *|video|pix_release*|aclr *|video|sys_release*|clrn *|video|pix_release*|clrn}]
if {[get_collection_size $owner_reset_pins] != 4} {error "DE25 video reset release ingress changed"}
set_false_path -to $owner_reset_pins
post_message -type info "DE25 video: 2 first-stage crossings, 9 bounded metadata bits, 4 reset ingress pins"

# Only readiness first-stage data is cut here. The renderer's shared pixel
# reset release owns its raw ingress exception in owner_reset_pins above.
# The source is registered; destination reset is released in its own domain.
# Stage-to-stage paths and both generated clocks stay timed; no broad clock groups or HDMI/I2C delay assumptions are introduced.
set computer_pixel_ready_source [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_source}]
set computer_pixel_ready_stage0 [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_sync[0]}]
set computer_pixel_ready_stage1 [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_sync[1]}]
set computer_pixel_ready_gate [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|video_ready_frame}]
set computer_pixel_ready_stage0_d [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_sync[0]|D}]
foreach computer_pixel_ready_nodes [list $computer_pixel_ready_source $computer_pixel_ready_stage0 $computer_pixel_ready_stage1 $computer_pixel_ready_gate $computer_pixel_ready_stage0_d] {
    if {[get_collection_size $computer_pixel_ready_nodes] != 1} {
        error "DE25 computer requires both pixel readiness synchronizer stages and the first-stage D pin"
    }
}
set_false_path -to $computer_pixel_ready_stage0_d
post_message -type info "DE25 computer pixel readiness crossing: one first-stage ingress, shared renderer reset release, timed stage-to-stage and frame-gate paths"

# The board qualifies PLL/configuration reset in the reference domain. Pixel
# visibility uses a retained token/reply handshake with ordinary data crossings;
# there is no second asynchronous reset release in the pixel domain. The token
# stays outstanding across repeated faults until allow-low and a closed line
# are acknowledged. Cut only each registered source-to-first-stage D path.
foreach {visibility_source visibility_stage0 visibility_stage1} {
    {fes_video_inst|visible_allow} {fes_video_inst|visible_sync[0]} {fes_video_inst|visible_sync[1]}
    {fes_video_inst|visibility_epoch} {fes_video_inst|visibility_epoch_sync[0]} {fes_video_inst|visibility_epoch_sync[1]}
    {fes_video_inst|visibility_ack} {fes_video_inst|visibility_ack_sync[0]} {fes_video_inst|visibility_ack_sync[1]}
    {fes_video_inst|hdmi_active} {fes_video_inst|active_sync[0]} {fes_video_inst|active_sync[1]}
} {
    set visibility_source_reg [get_registers $visibility_source]
    set visibility_stage0_reg [get_registers $visibility_stage0]
    set visibility_stage1_reg [get_registers $visibility_stage1]
    set visibility_stage0_d [get_pins -compatibility_mode -nocase -nowarn "${visibility_stage0}|D"]
    foreach visibility_nodes [list $visibility_source_reg $visibility_stage0_reg $visibility_stage1_reg $visibility_stage0_d] {
        if {[get_collection_size $visibility_nodes] != 1} {error "DE25 visibility requires one source, two stages and the first D pin"}
    }
    set_false_path -from $visibility_source_reg -to $visibility_stage0_d
}
post_message -type info "DE25 visibility: 4 registered sources and first-stage ingress exceptions; stage-to-stage and downstream paths timed"

# HDMI completion crosses its generated I2C clock into reference, then control
# readiness crosses reference -> control. Each source is
# registered and each destination is a two-stage synchronizer. Constrain only
# the asynchronous source-to-first-stage D path; all stage-to-stage data and
# locally released reset recovery/removal paths remain timed.
foreach {ready_source ready_stage0 ready_stage1} {
    {fes_video_inst|hdmi_config|READY} {fes_video_inst|hdmi_ready_sync[0]} {fes_video_inst|hdmi_ready_sync[1]}
    {fes_video_inst|video_ready_source} {soc_inst|subsys_periph|fes_computer|fes_computer|transport|video_ready_sync[0]} {soc_inst|subsys_periph|fes_computer|fes_computer|transport|video_ready_sync[1]}
} {
    set ready_source_reg [get_registers $ready_source]
    set ready_stage0_reg [get_registers $ready_stage0]
    set ready_stage1_reg [get_registers $ready_stage1]
    set ready_stage0_d [get_pins -compatibility_mode -nocase -nowarn "${ready_stage0}|D"]
    foreach ready_nodes [list $ready_source_reg $ready_stage0_reg $ready_stage1_reg $ready_stage0_d] {
        if {[get_collection_size $ready_nodes] != 1} {error "DE25 HDMI readiness requires one source, two stages and the first D pin"}
    }
    set_false_path -from $ready_source_reg -to $ready_stage0_d
}
post_message -type info "DE25 HDMI readiness: 2 registered sources, 2 first-stage ingress exceptions; stage-to-stage and reset paths timed"
