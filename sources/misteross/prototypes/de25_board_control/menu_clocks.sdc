# Regenerated vendor PLL SDC owns the 50MHz references and 100/74.25MHz clocks.
set hdmi_config_clock [get_registers {fes_video_inst|hdmi_config|mI2C_CTRL_CLK}]
if {[get_collection_size $hdmi_config_clock] != 1} {error "DE25 computer I2C clock register changed"}
create_generated_clock -name adv7513_config -source [get_ports CLOCK0_50] -divide_by 5002 $hdmi_config_clock
# Only the raw asynchronous interrupt's first D pin is exempt. The second
# stage and the related divided-clock configuration consumer remain timed.
set hdmi_irq_pin [get_ports HDMI_TX_INT]
set hdmi_irq_first [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|hdmi_irq_sync[0]|D}]
set hdmi_irq_stages [get_registers {fes_video_inst|hdmi_irq_sync[*]}]
if {[get_collection_size $hdmi_irq_pin] != 1 || [get_collection_size $hdmi_irq_first] != 1 || [get_collection_size $hdmi_irq_stages] != 2} {
    error "DE25 HDMI interrupt requires exactly two stages and one raw first-stage D pin"
}
set_false_path -from $hdmi_irq_pin -to $hdmi_irq_first
set computer_aclr [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|aclr fes_video_inst|pixel_reset|sync_regs*|aclr}]
set computer_clrn [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|clrn fes_video_inst|pixel_reset|sync_regs*|clrn}]
set computer_clear_count [expr {[get_collection_size $computer_aclr] + [get_collection_size $computer_clrn]}]
if {$computer_clear_count != 4} {error "DE25 computer requires four HDMI reset ingress pins"}
if {[get_collection_size $computer_aclr] > 0} {set_false_path -to $computer_aclr}
if {[get_collection_size $computer_clrn] > 0} {set_false_path -to $computer_clrn}
post_message -type info "DE25 computer HDMI reset ingress pins: $computer_clear_count (expected4)"
# Menu CSR and pixel clocks are unrelated. Only first-stage control D pins
# are asynchronous ingress; stage-to-stage and every ordinary path stay timed.
foreach {label launch first second} {
    request request_valid {request_sync[0]} {request_sync[1]}
    acknowledge acknowledge {acknowledge_sync[0]} {acknowledge_sync[1]}
    online pixel_online {online_sync[0]} {online_sync[1]}
} {
    set menu_launch [get_registers "*|csr_crossing|$launch"]
    set menu_first [get_registers "*|csr_crossing|$first"]
    set menu_second [get_registers "*|csr_crossing|$second"]
    set menu_first_d [get_pins -compatibility_mode -nocase -nowarn "*|csr_crossing|$first|D"]
    foreach nodes [list $menu_launch $menu_first $menu_second $menu_first_d] {
        if {[get_collection_size $nodes] != 1} {error "DE25 menu $label crossing endpoints changed"}
    }
    set_false_path -from $menu_launch -to $menu_first_d
}
# Both packet banks stay stable throughout the four-phase request/return.
# Flight and skew fit 10 ns, below a pixel period and below either two-stage
# control synchronizer's sampling interval. No whole-clock cut is used.
foreach {launch sink bits} {request_packet command_packet 45 response_packet reply_packet 33} {
    set menu_source [get_registers "*|csr_crossing|$launch\[*\]"]
    set menu_sink [get_registers "*|csr_crossing|$sink\[*\]"]
    if {[get_collection_size $menu_source] != $bits || [get_collection_size $menu_sink] != $bits} {
        error "DE25 menu stable packet width changed"
    }
    set_net_delay -max 10.000 -from $menu_source -to $menu_sink
    set_data_delay 10.000 -from $menu_source -to $menu_sink
    set_max_skew 10.000 -from $menu_source -to $menu_sink
    set_max_delay 10.000 -from $menu_source -to $menu_sink
    set_min_delay 0.000 -from $menu_source -to $menu_sink
}
# Raw reset assertion reaches only two release flops per clock. Deassertion
# passes both stages; downstream resets and recovery/removal remain timed.
set menu_reset_ingress [get_pins -compatibility_mode -nocase -nowarn {
    *|csr_crossing|pixel_release[*]|aclr *|csr_crossing|pixel_release[*]|clrn *|csr_crossing|pixel_release[*]|prn
    *|csr_crossing|pixel_reset_seen[*]|aclr *|csr_crossing|pixel_reset_seen[*]|clrn *|csr_crossing|pixel_reset_seen[*]|prn
}]
if {[get_collection_size $menu_reset_ingress] != 4} {error "DE25 menu requires exactly four raw reset ingress pins"}
set_false_path -to $menu_reset_ingress
post_message -type info "DE25 menu: 3 first-stage crossings, 45/33 stable packet bits bounded to 10 ns, 4 raw reset ingress pins"

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
