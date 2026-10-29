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
# Readiness is a monotonic control level crossing from the 100 MHz endpoint to
# its pixel-domain frame gate. Cut only its asynchronous synchronizer ingress.
set computer_video_ready_head [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_meta|d}]
if {[get_collection_size $computer_video_ready_head] != 1} {error "DE25 computer video readiness synchronizer ingress changed"}
set_false_path -to $computer_video_ready_head
post_message -type info "DE25 computer video readiness CDC ingress: one stage0 D pin"
# Keep synchronizer stage-to-stage, frame-gate and output paths timed. Do not
# add clock groups, FIFO cuts or external HDMI/I2C delay assumptions.
