create_clock -name board50 -period 20.000 [get_ports CLOCK0_50]
derive_clock_uncertainty

# SDM initialization is asynchronous; only the dedicated release chain clears.
set release_clear [get_pins -compatibility_mode -nocase -nowarn {reset_sync*|clrn}]
if {[get_collection_size $release_clear] != 2} {
    error "link proof must retain exactly two reset release clear pins"
}
set_false_path -to $release_clear

# The virtual diagnostic freeze request is asynchronous. Only its first D pin
# is excepted; both stages and the drain/reset/clamp fanout remain timed.
set freeze_first [get_pins -compatibility_mode -nocase -nowarn {shell_inst|freeze_sync[0]|d}]
if {[get_collection_size $freeze_first] != 1} {
    error "link proof freeze request must enter exactly one first-stage D pin"
}
set_false_path -from [get_ports freeze_req] -to $freeze_first
post_message -type info "link proof: two reset clear pins, one freeze ingress D; stage2/fanout timed"

# Human-visible indicators have no synchronous external receiver. Keep this
# explicit port-only exception separate from the internal socket timing proof.
set led_ports [get_ports {LEDR[*]}]
if {[get_collection_size $led_ports] != 8} {
    error "link proof must retain exactly eight diagnostic LED ports"
}
set_false_path -to $led_ports
