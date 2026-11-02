# Vendor regenerated PLLs own the 50/74.25/100 MHz clocks. The exact pinned
# initializer toggles after 2501 reference edges, hence divide5002.
set hdmi_config_clock [get_registers {fes_video_inst|hdmi_config|mI2C_CTRL_CLK}]
if {[get_collection_size $hdmi_config_clock] != 1} {error "DE25 scanout I2C clock register changed"}
create_generated_clock -name adv7513_config -source [get_ports CLOCK0_50] -divide_by 5002 $hdmi_config_clock
# The two external inputs terminate only at their first 50 MHz synchronizer
# stages. Keep stage0->stage1 and all controller/reset fanout timed.
foreach {port head} {HDMI_I2C_SDA hdmi_sda_sync HDMI_TX_INT hdmi_int_sync} {
    set hdmi_input [get_ports $port]
    set hdmi_input_name [format {fes_video_inst|%s[0]|d} $head]
    set hdmi_input_head [get_pins -compatibility_mode -nocase -nowarn $hdmi_input_name]
    if {[get_collection_size $hdmi_input] != 1 || [get_collection_size $hdmi_input_head] != 1} {
        error "DE25 HDMI input synchronizer ingress changed: $port"
    }
    set_false_path -from $hdmi_input -to $hdmi_input_head
}
# Configuration release, PLL-lock monitoring, visibility CDC and output gate.
# Loss of pixel lock does not reset either live PD crossing or the endpoint's
# outstanding accounting. Qsys emits the two domain reset synchronizers as
# top-level reset controllers; the reset-bridge modules themselves are
# pass-through cells. The pixel-domain visibility release, which lock loss
# clears at once, completes the asynchronous clear set.
set scanout_aclr [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|aclr fes_video_inst|lock_qualifier|sync_regs*|aclr soc_inst|rst_controller_001|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[0]|aclr soc_inst|rst_controller_001|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[1]|aclr soc_inst|rst_controller_002|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[0]|aclr soc_inst|rst_controller_002|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[1]|aclr fes_video_inst|visible_release|sync_regs*|aclr}]
set scanout_clrn [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|config_reset|sync_regs*|clrn fes_video_inst|lock_qualifier|sync_regs*|clrn soc_inst|rst_controller_001|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[0]|clrn soc_inst|rst_controller_001|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[1]|clrn soc_inst|rst_controller_002|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[0]|clrn soc_inst|rst_controller_002|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[1]|clrn fes_video_inst|visible_release|sync_regs*|clrn}]
set scanout_clear_count [expr {[get_collection_size $scanout_aclr] + [get_collection_size $scanout_clrn]}]
post_message -type info "DE25 scanout qualifier/reset-bridge/visibility clear pins: $scanout_clear_count (expected 10)"
if {$scanout_clear_count != 10} {error "DE25 scanout requires exactly ten qualifier/reset-bridge/visibility clear pins"}
if {[get_collection_size $scanout_aclr] > 0} {set_false_path -to $scanout_aclr}
if {[get_collection_size $scanout_clrn] > 0} {set_false_path -to $scanout_clrn}
# The visibility synchronizer clears from the release's pixel-clock output and
# the registered data/DE gate from visible_sync[1], so their recovery and
# removal stay timed. Only check that the clear pins exist.
set scanout_visible_clear [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|visible_sync[0]|aclr fes_video_inst|visible_sync[1]|aclr fes_video_inst|visible_sync[0]|clrn fes_video_inst|visible_sync[1]|clrn}]
if {[get_collection_size $scanout_visible_clear] != 2} {error "DE25 scanout visibility synchronizer clear pins changed"}
set scanout_gate_clear [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|video_active|aclr fes_video_inst|video_active|clrn}]
if {[get_collection_size $scanout_gate_clear] != 1} {error "DE25 scanout data/DE gate clear pin changed"}
# A monotonic 100 MHz fault level crosses through its dedicated two-flop chain.
# Cut only asynchronous ingress to stage0 D. Stage0->1 and fanout stay timed.
set scanout_fault_source [get_registers {fes_video_inst|fault}]
set scanout_fault_head [get_pins -compatibility_mode -nocase {soc_inst|subsys_periph|fes_scanout|fes_scanout|clock_fault_meta|d}]
if {[get_collection_size $scanout_fault_source] != 1 || [get_collection_size $scanout_fault_head] != 1} {error "DE25 scanout fault synchronizer ingress changed"}
set_false_path -from $scanout_fault_source -to $scanout_fault_head
post_message -type info "DE25 scanout fault CDC ingress: one source and one stage0 D pin"
# The registered system-domain visibility request crosses into the
# pixel-clock HDMI gate. Cut only its path to the first synchronizer D input;
# the next stage and line-boundary gate remain timed in the pixel domain.
set scanout_visible_source [get_registers {fes_video_inst|visible_request}]
set scanout_visible_head [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|visible_sync[0]|d}]
if {[get_collection_size $scanout_visible_source] != 1 || [get_collection_size $scanout_visible_head] != 1} {error "DE25 scanout visibility synchronizer ingress changed"}
set_false_path -from $scanout_visible_source -to $scanout_visible_head
post_message -type info "DE25 scanout visibility CDC ingress: one source and one stage0 D pin"
# HDMI configuration uses CLOCK0_50; the 100 MHz system PLL uses CLOCK2_50.
# These are independent sources. Only the registered readiness level enters
# stage0 asynchronously; stage0->1 and all system-domain consumers stay timed.
set scanout_ready_source [get_registers {fes_video_inst|hdmi_ready_source}]
set scanout_ready_head [get_pins -compatibility_mode -nocase -nowarn {fes_video_inst|hdmi_ready_sync[0]|d}]
if {[get_collection_size $scanout_ready_source] != 1 || [get_collection_size $scanout_ready_head] != 1} {error "DE25 scanout HDMI-ready synchronizer ingress changed"}
set_false_path -from $scanout_ready_source -to $scanout_ready_head
post_message -type info "DE25 scanout HDMI-ready CDC ingress: one source and one stage0 D pin"
# No clock groups or FIFO pointer cuts: mm_ccb owns its CDC constraints.
# External HDMI/I2C delay and electrical contracts remain unqualified.
