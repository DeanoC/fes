# Diagnostic tighter bounds on the two routed synchronous controller inputs.
# Production SDC, clocks and uncertainty stay unchanged on disk.
if {[llength $quartus(args)] != 2} {error "expected project.qpf and output directory"}
set project [file normalize [lindex $quartus(args) 0]]
set output [file normalize [lindex $quartus(args) 1]]
file mkdir $output
cd [file dirname $project]
project_open [file rootname [file tail $project]]
create_timing_netlist -model slow
read_sdc
set source [get_registers {*fes_top_reset_inst*sync_regs?1?}]
set destinations [get_registers {*soc_inst*rst_controller_001*altera_reset_synchronizer_int_chain?1?}]
if {[get_collection_size $source] != 1 || [get_collection_size $destinations] != 2} {
    error "expected one FES output stage and two generated controller input registers"
}
set_max_delay -from $source -to $destinations 10.0
set_min_delay -from $source -to $destinations 0.0
update_timing_netlist
report_timing -setup -from $source -to $destinations -npaths 2 -detail full_path \
    -file [file join $output controller-10ns-setup.rpt]
report_timing -hold -from $source -to $destinations -npaths 2 -detail full_path \
    -file [file join $output controller-0ns-hold.rpt]
project_close
