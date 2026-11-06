# Regenerated vendor PLL SDC owns the 50MHz references and 100/74.25MHz clocks.
set hdmi_config_clock [get_registers {fes_video_inst|hdmi_config|mI2C_CTRL_CLK}]
if {[get_collection_size $hdmi_config_clock] != 1} {error "DE25 computer I2C clock register changed"}
create_generated_clock -name adv7513_config -source [get_ports CLOCK0_50] -divide_by 5002 $hdmi_config_clock
set computer_aclr [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|aclr fes_video_inst|pixel_reset|sync_regs*|aclr}]
set computer_clrn [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|clrn fes_video_inst|pixel_reset|sync_regs*|clrn}]
set computer_clear_count [expr {[get_collection_size $computer_aclr] + [get_collection_size $computer_clrn]}]
if {$computer_clear_count != 4} {error "DE25 computer requires four HDMI reset ingress pins"}
if {[get_collection_size $computer_aclr] > 0} {set_false_path -to $computer_aclr}
if {[get_collection_size $computer_clrn] > 0} {set_false_path -to $computer_clrn}
post_message -type info "DE25 computer HDMI reset ingress pins: $computer_clear_count (expected4)"
# The computer runs on the preserved peripheral clock. No new GP control CDC,
# clock groups or FIFO cuts are introduced. Receiver-only HDMI limits are below;
# external I2C timing and the board electrical/skew/load contract remain open.

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
# The caller owns reset qualification. System owner recovery/removal is timed
# from the existing 100 MHz peripheral reset. The pixel token owner shares the
# readiness release below; do not restore separate video release chains or cuts.
set removed_video_releases [get_registers -nowarn {*|video|sys_release[*] *|video|pix_release[*]}]
if {[get_collection_size $removed_video_releases] != 0} {error "DE25 video has independent reset release chains"}
post_message -type info "DE25 video: 2 first-stage crossings, 9 bounded metadata bits, shared qualified resets"

# Only readiness first-stage data and raw reset-release ingress are cut.
# The source is registered; destination reset is released in its own domain.
# Stage-to-stage paths and both generated clocks stay timed; no broad clock groups are introduced.
set computer_pixel_ready_source [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_source}]
set computer_pixel_ready_stage0 [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_sync[0]}]
set computer_pixel_ready_stage1 [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_sync[1]}]
set computer_pixel_ready_gate [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|video_ready_frame}]
set computer_pixel_ready_stage0_d [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_sync[0]|D}]
set computer_pixel_ready_aclr [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_reset_release[0]|aclr soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_reset_release[1]|aclr}]
set computer_pixel_ready_clrn [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_reset_release[0]|clrn soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_reset_release[1]|clrn}]
foreach computer_pixel_ready_nodes [list $computer_pixel_ready_source $computer_pixel_ready_stage0 $computer_pixel_ready_stage1 $computer_pixel_ready_gate $computer_pixel_ready_stage0_d] {
    if {[get_collection_size $computer_pixel_ready_nodes] != 1} {
        error "DE25 computer requires both pixel readiness synchronizer stages and the first-stage D pin"
    }
}
set computer_pixel_ready_clear_count [expr {[get_collection_size $computer_pixel_ready_aclr] + [get_collection_size $computer_pixel_ready_clrn]}]
if {$computer_pixel_ready_clear_count != 2} {
    error "DE25 computer pixel readiness synchronizer must have exactly two asynchronous clear pins"
}
set_false_path -to $computer_pixel_ready_stage0_d
if {[get_collection_size $computer_pixel_ready_aclr] > 0} {set_false_path -to $computer_pixel_ready_aclr}
if {[get_collection_size $computer_pixel_ready_clrn] > 0} {set_false_path -to $computer_pixel_ready_clrn}
post_message -type info "DE25 computer pixel readiness crossing: one first-stage ingress, timed stage-to-stage and frame-gate paths"

# ADV7513 Rev B, Table 2: input video setup 1.8 ns, hold 1.3 ns.
# https://www.analog.com/media/en/technical-documentation/data-sheets/ADV7513.pdf
# Forward the actual PLL clock to the clock pad, retaining its insertion delay
# and uncertainty. Time every RGB/DE/HS/VS launch, including reset mask logic.
# These are receiver-only minima at a zero board-skew reference, not a measured
# PCB budget. The inherited clock voltage/path and output loads are unqualified;
# meeting this requirement alone cannot establish board timing or acceptance.
set computer_hdmi_pixel [get_clocks {fes_video_inst|video_clock|iopll_0_outclk0}]
if {[get_collection_size $computer_hdmi_pixel] != 1} {error "DE25 HDMI requires one pixel clock"}
foreach_in_collection computer_hdmi_clock $computer_hdmi_pixel {
    set computer_hdmi_source [get_clock_info -targets $computer_hdmi_clock]
    set computer_hdmi_master [get_clock_info -name $computer_hdmi_clock]
}
set computer_hdmi_clock_pad [get_ports HDMI_TX_CLK]
if {[get_collection_size $computer_hdmi_source] != 1 || [get_collection_size $computer_hdmi_clock_pad] != 1} {
    error "DE25 HDMI requires one PLL target and one forwarded-clock pad"
}
create_generated_clock -name adv7513_video -source $computer_hdmi_source -master_clock $computer_hdmi_master -divide_by 1 $computer_hdmi_clock_pad
set computer_hdmi_data [get_ports {HDMI_TX_D[*] HDMI_TX_DE HDMI_TX_HS HDMI_TX_VS}]
if {[get_collection_size $computer_hdmi_data] != 27} {error "DE25 HDMI requires all 27 RGB/control pads"}
set_output_delay -clock adv7513_video -max 1.800 $computer_hdmi_data
set_output_delay -clock adv7513_video -min -1.300 $computer_hdmi_data
derive_clock_uncertainty
post_message -type info "DE25 HDMI: 27 pads with receiver-only 1.8 ns setup/1.3 ns hold; board clock path, skew and load UNQUALIFIED"
