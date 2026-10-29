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
# Only the first stage and asynchronous reset pins of the pixel readiness
# synchronizer is cut. The stage-to-stage and frame-gate paths stay timed;
# no broad clock groups or HDMI/I2C delay assumptions are introduced.
set computer_pixel_ready_stage0 [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_meta}]
set computer_pixel_ready_stage1 [get_registers {soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_sync}]
set computer_pixel_ready_stage0_d [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_meta|D}]
set computer_pixel_ready_aclr [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_meta|aclr soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_sync|aclr}]
set computer_pixel_ready_clrn [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_meta|clrn soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_sync|clrn}]
foreach computer_pixel_ready_nodes [list $computer_pixel_ready_stage0 $computer_pixel_ready_stage1 $computer_pixel_ready_stage0_d] {
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
post_message -type info "DE25 computer pixel readiness crossing: one first-stage ingress, timed stage-to-stage and frame gate paths"
