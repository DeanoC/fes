# Generated IOPLL SDC owns the 50 MHz reference and exact 74.25/52.224 MHz clocks.
# The pinned initializer toggles after counting 0..2500: divide by 5002.
set hdmi_config_clock [get_registers {hdmi_config|mI2C_CTRL_CLK}]
if {[get_collection_size $hdmi_config_clock] != 1} {error "DE25 vendor I2C clock register changed"}
create_generated_clock -name adv7513_config -source [get_ports CLOCK0_50] -divide_by 5002 $hdmi_config_clock

# Cut only asynchronous assertion into the three dedicated release chains.
set zx81_aclr [get_pins -compatibility_mode -nocase -nowarn {config_reset|sync_regs*|aclr system_reset|sync_regs*|aclr pixel_reset|sync_regs*|aclr}]
set zx81_clrn [get_pins -compatibility_mode -nocase -nowarn {config_reset|sync_regs*|clrn system_reset|sync_regs*|clrn pixel_reset|sync_regs*|clrn}]
set zx81_reset_pins [expr {[get_collection_size $zx81_aclr] + [get_collection_size $zx81_clrn]}]
if {$zx81_reset_pins != 6} {error "DE25 ZX81 requires exactly six reset synchronizer clear pins"}
if {[get_collection_size $zx81_aclr] > 0} {set_false_path -to $zx81_aclr}
if {[get_collection_size $zx81_clrn] > 0} {set_false_path -to $zx81_clrn}
post_message -type info "DE25 ZX81 reset ingress pins: $zx81_reset_pins (expected 6)"

# The physical buttons are asynchronous. Only the first-stage D pins are cut;
# the first-to-second-stage and all debounce/machine paths remain timed.
set zx81_key_ingress [get_pins -compatibility_mode -nocase {local_keys|key_meta*|d}]
if {[get_collection_size $zx81_key_ingress] != 2} {error "DE25 ZX81 requires exactly two button ingress pins"}
set_false_path -from [get_ports {KEY[*]}] -to $zx81_key_ingress

# No complete ADV7513 setup/hold/flight-time budget is provided by the vendor
# reference. Keep missing HDMI/I2C external timing visible; do not invent delays.
