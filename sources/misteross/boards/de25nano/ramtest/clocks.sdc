# New SDRAM is intentionally a fixed 50MHz diagnostic transport.
create_clock -name ramtest_sdram50 -period 20.000 [get_ports CLOCK1_50]
create_generated_clock -name ramtest_sdram_forwarded -source [get_ports CLOCK1_50] -invert [get_ports DRAM_CLK]
# The power-on/calibration reset helper has exactly two asynchronous assertion
# pins. Its synchronizer D inputs and synchronous release fanout remain timed.
set ram_cal_aclr [get_pins -compatibility_mode -nocase -nowarn {fes_ram_cal_reset|sync_regs*|aclr}]
set ram_cal_clrn [get_pins -compatibility_mode -nocase -nowarn {fes_ram_cal_reset|sync_regs*|clrn}]
if {[expr {[get_collection_size $ram_cal_aclr] + [get_collection_size $ram_cal_clrn]}] != 2} {
    error "DE25 RAM calibration reset helper must have exactly two asynchronous clear pins"
}
if {[get_collection_size $ram_cal_aclr] > 0} {set_false_path -to $ram_cal_aclr}
if {[get_collection_size $ram_cal_clrn] > 0} {set_false_path -to $ram_cal_clrn}
# No guessed external input/output delays or blanket clock-domain exceptions.
