# Vendor regenerated PLLs own the 50/74.25/100 MHz clocks. The exact pinned
# initializer toggles after 2501 reference edges, hence divide5002.
set hdmi_config_clock [get_registers {fes_video_inst|hdmi_config|mI2C_CTRL_CLK}]
if {[get_collection_size $hdmi_config_clock] != 1} {error "DE25 scanout I2C clock register changed"}
create_generated_clock -name adv7513_config -source [get_ports CLOCK0_50] -divide_by 5002 $hdmi_config_clock
# Configuration release, PLL-lock monitoring, visibility CDC and output gate.
# Loss of pixel lock does not reset either live PD crossing or endpoint state.
set scanout_aclr [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|aclr fes_video_inst|lock_qualifier|sync_regs*|aclr fes_video_inst|fes_scanout_pixel_reset|sync_regs*|aclr fes_video_inst|fes_scanout_system_reset|sync_regs*|aclr fes_video_inst|visible_sync[0]|aclr fes_video_inst|visible_sync[1]|aclr fes_video_inst|video_active|aclr}]
set scanout_clrn [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|clrn fes_video_inst|lock_qualifier|sync_regs*|clrn fes_video_inst|fes_scanout_pixel_reset|sync_regs*|clrn fes_video_inst|fes_scanout_system_reset|sync_regs*|clrn fes_video_inst|visible_sync[0]|clrn fes_video_inst|visible_sync[1]|clrn fes_video_inst|video_active|clrn}]
set scanout_clear_count [expr {[get_collection_size $scanout_aclr] + [get_collection_size $scanout_clrn]}]
if {$scanout_clear_count != 11} {error "DE25 scanout requires exactly eleven reset/visibility clear pins"}
if {[get_collection_size $scanout_aclr] > 0} {set_false_path -to $scanout_aclr}
if {[get_collection_size $scanout_clrn] > 0} {set_false_path -to $scanout_clrn}
post_message -type info "DE25 scanout reset/visibility clear pins: $scanout_clear_count (expected 11)"
# A monotonic 100 MHz fault level crosses through its dedicated two-flop chain.
# Cut only asynchronous ingress to stage0 D. Stage0->1 and fanout stay timed.
set scanout_fault_source [get_registers {fes_video_inst|fault}]
set scanout_fault_head [get_pins -compatibility_mode -nocase {soc_inst|subsys_periph|fes_scanout|fes_scanout|clock_fault_sync[0]|d}]
if {[get_collection_size $scanout_fault_source] != 1 || [get_collection_size $scanout_fault_head] != 1} {error "DE25 scanout fault synchronizer ingress changed"}
set_false_path -from $scanout_fault_source -to $scanout_fault_head
post_message -type info "DE25 scanout fault CDC ingress: one source and one stage0 D pin"
# The system/PLL visibility request crosses into the pixel-clock HDMI gate.
# Cut only the first synchronizer D input; its next stage and line-boundary
# gate remain timed in the pixel domain.
set scanout_visible_head [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|visible_sync[0]|d}]
if {[get_collection_size $scanout_visible_head] != 1} {error "DE25 scanout visibility synchronizer ingress changed"}
set_false_path -to $scanout_visible_head
post_message -type info "DE25 scanout visibility CDC ingress: one stage0 D pin"
# No clock groups or FIFO pointer cuts: mm_ccb owns its CDC constraints.
# External HDMI/I2C delay and electrical contracts remain unqualified.
