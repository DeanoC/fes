# The regenerated IOPLL SDC owns the 50 MHz reference and 74.25 MHz pixel clocks.
# The pinned initializer increments through 0..2500 before toggling its clock:
# 2 * (50000000 / 20000 + 1) reference edges, not a rounded 10 kHz assumption.
set hdmi_config_clock [get_registers {hdmi_config|mI2C_CTRL_CLK}]
if {[get_collection_size $hdmi_config_clock] != 1} {error "DE25 vendor I2C clock register changed"}
create_generated_clock -name adv7513_config -source [get_ports CLOCK0_50] -divide_by 5002 $hdmi_config_clock
# This only cuts asynchronous assertion into the two dedicated release chains.
set display_aclr [get_pins -compatibility_mode -nocase -nowarn {config_reset|sync_regs*|aclr pixel_reset|sync_regs*|aclr}]
set display_clrn [get_pins -compatibility_mode -nocase -nowarn {config_reset|sync_regs*|clrn pixel_reset|sync_regs*|clrn}]
set display_reset_pins [expr {[get_collection_size $display_aclr] + [get_collection_size $display_clrn]}]
if {$display_reset_pins != 4} {error "DE25 display requires exactly four reset synchronizer clear pins"}
if {[get_collection_size $display_aclr] > 0} {set_false_path -to $display_aclr}
if {[get_collection_size $display_clrn] > 0} {set_false_path -to $display_clrn}
post_message -type info "DE25 display reset ingress pins: $display_reset_pins (expected 4)"
# The vendor reference supplies no complete ADV7513 board flight-time/delay
# contract. Do not manufacture output delays or exempt the RGB/control ports.
# Unconstrained IO and transmitter/I2C timing remain explicit qualification gaps.
