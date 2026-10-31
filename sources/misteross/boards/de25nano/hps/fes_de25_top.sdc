# Only asynchronous ingress into the dedicated reset synchronizer is untimed.
# Keep both inter-stage D timing and synchronized release to functional resets.
# Match the async-pin convention used by Quartus's reset-controller SDC; never
# cut register-to-register data or the released reset's recovery/removal paths.
set fes_reset_aclr [get_pins -compatibility_mode -nocase -nowarn {fes_top_reset_inst|sync_regs*|aclr}]
set fes_reset_clrn [get_pins -compatibility_mode -nocase -nowarn {fes_top_reset_inst|sync_regs*|clrn}]
set fes_reset_pin_count [expr {[get_collection_size $fes_reset_aclr] + [get_collection_size $fes_reset_clrn]}]
if {$fes_reset_pin_count != 2} {
    error "DE25 top reset must have exactly two asynchronous clear pins; review synthesized synchronizer"
}
post_message -type info "DE25 top reset asynchronous clear pins: $fes_reset_pin_count (expected 2); D and release fanout remain timed"
if {[get_collection_size $fes_reset_aclr] > 0} {
    set_false_path -to $fes_reset_aclr
}
if {[get_collection_size $fes_reset_clrn] > 0} {
    set_false_path -to $fes_reset_clrn
}

# These four active outputs terminate at the pinned board's human-visible LEDs.
# They have no external synchronous receiver and no output-delay contract.
# Other ports and all internal paths retain their existing timing requirements.
set fes_led_outputs [get_ports {LED[0] LED[1] LED[2] LED[7]}]
if {[get_collection_size $fes_led_outputs] != 4} {
    error "DE25 asynchronous LED output selection changed"
}
post_message -type info "DE25 asynchronous human-visible LED output ports: [get_collection_size $fes_led_outputs] (expected 4)"
set_false_path -to $fes_led_outputs
