# Inspect the existing fitted diagnostic; do not compile or program it.
# One slow-model path inventory supplements the complete multicorner reports.
if {[llength $quartus(args)] != 2} {error "expected project.qpf and output directory"}
set project [file normalize [lindex $quartus(args) 0]]
set output [file normalize [lindex $quartus(args) 1]]
file mkdir $output
cd [file dirname $project]
project_open [file rootname [file tail $project]]
create_timing_netlist -model slow
read_sdc
update_timing_netlist
set first_stage [get_registers {*fes_top_reset_inst*sync_regs?0?}]
set last_stage [get_registers {*fes_top_reset_inst*sync_regs?1?}]
set consumers [get_registers {*debounce_inst*counter* *heartbeat_count*}]
set controller_heads [get_registers {*soc_inst*rst_controller_001*altera_reset_synchronizer_int_chain?1?}]
if {[get_collection_size $first_stage] != 1 || [get_collection_size $last_stage] != 1} {
    error "expected both mapped reset synchronizer registers"
}
if {[get_collection_size $consumers] != 89} {error "expected 89 top reset consumers"}
if {[get_collection_size $controller_heads] != 2} {error "expected two generated synchronous-reset heads"}
post_message -type info "DE25 fitted reset audit: 2 stages, 89 top consumers"
report_timing -setup -from $first_stage -to $last_stage -npaths 1 -detail full_path \
    -file [file join $output reset-stage-setup.rpt]
report_timing -hold -from $first_stage -to $last_stage -npaths 1 -detail full_path \
    -file [file join $output reset-stage-hold.rpt]
report_timing -recovery -from $last_stage -to $consumers -npaths 5 -detail full_path \
    -file [file join $output reset-release-recovery.rpt]
report_timing -removal -from $last_stage -to $consumers -npaths 5 -detail full_path \
    -file [file join $output reset-release-removal.rpt]
report_timing -setup -from $last_stage -to $controller_heads -npaths 2 -detail full_path \
    -file [file join $output reset-controller-setup.rpt]
report_timing -hold -from $last_stage -to $controller_heads -npaths 2 -detail full_path \
    -file [file join $output reset-controller-hold.rpt]
report_exceptions -file [file join $output exceptions.rpt]
report_clocks -file [file join $output clocks.rpt]
project_close
