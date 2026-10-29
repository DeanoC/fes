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
set_max_delay 10.000 -from $owner_rows_source -to $owner_rows_sink
set_min_delay 0.000 -from $owner_rows_source -to $owner_rows_sink
# Raw epoch reset enters two per-domain release chains. Recovery/removal from
# their locally synchronized outputs to owner state remain timed.
set owner_reset_pins [get_pins -compatibility_mode -nocase -nowarn {*|video|sys_release*|aclr *|video|pix_release*|aclr *|video|sys_release*|clrn *|video|pix_release*|clrn}]
if {[get_collection_size $owner_reset_pins] != 4} {error "DE25 video reset release ingress changed"}
set_false_path -to $owner_reset_pins
post_message -type info "DE25 video: 2 first-stage crossings, 9 bounded metadata bits, 4 reset ingress pins"
