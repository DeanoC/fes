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
# The registered calibration status uses independent CLOCK1_50. Its existing
# two-stage level synchronizer receives the 100MHz CLOCK2_50-derived clock.
# Cut only this asynchronous ingress; stage0->stage1 and all stage1 consumers
# retain ordinary timing. Do not infer a phase relationship from equal rates.
set ram_cal_status_source [get_registers {fes_ram_calibration|r_cal_done_rst_n}]
set ram_cal_status_head [get_registers {soc_inst|subsys_periph|fes_ramtest_fpga|fes_ramtest_fpga|calibration[0]}]
set ram_cal_status_tail [get_registers {soc_inst|subsys_periph|fes_ramtest_fpga|fes_ramtest_fpga|calibration[1]}]
set ram_cal_status_d [get_pins -compatibility_mode -nocase -nowarn {soc_inst|subsys_periph|fes_ramtest_fpga|fes_ramtest_fpga|calibration[0]|D}]
foreach ram_cal_status_nodes [list $ram_cal_status_source $ram_cal_status_head $ram_cal_status_tail $ram_cal_status_d] {
    if {[get_collection_size $ram_cal_status_nodes] != 1} {
        error "DE25 RAM calibration status requires one source, two distinct stages and one first-stage D pin"
    }
}
puts "DE25 RAM calibration asynchronous ingress: 1 source, 1 first stage, 1 second stage, 1 first-stage D pin"
set_false_path -from $ram_cal_status_source -to $ram_cal_status_d
# No guessed external input/output delays or blanket clock-domain exceptions.
