# Quartus 17.0.2: extract paths from an existing fitted RAM-test project.
# Run on a private copy of the project/database; this does not compile RTL.
# Usage: quartus_sta -t ramtest_timing_paths.tcl /path/to/top.qpf /path/to/reports
if {[llength $quartus(args)] != 2} {
    error "expected project.qpf and report directory"
}
set project [file normalize [lindex $quartus(args) 0]]
set output [file normalize [lindex $quartus(args) 1]]
if {![file isfile $project]} {
    error "project does not exist: $project"
}
file mkdir $output
cd [file dirname $project]
project_open [file rootname [file tail $project]]
create_timing_netlist -model slow -temperature 100 -voltage 1100
read_sdc
update_timing_netlist

report_timing -setup -npaths 20 -nworst 1 -detail full_path -show_routing \
    -file [file join $output worst-paths.rpt]
report_timing -setup -from [get_registers {*hps_ddr*port2*skid}] \
    -to [get_registers {*ddr2_test*address*}] -npaths 30 -nworst 1 \
    -detail full_path -show_routing -file [file join $output memory-matched.rpt]
report_timing -setup -from [get_registers {*display*col_s*}] \
    -to [get_registers {*display*ddr_ch_t*}] -npaths 20 -nworst 1 \
    -detail full_path -show_routing -file [file join $output pixel-matched.rpt]

# The saved issue-264 seed-2 netlist resolves to these logical bits.
# A nextpnr generated cell suffix (e.g. _22) is not the RTL bit index.
# '?' matches each literal bracket in TimeQuest's collection patterns.
report_timing -setup -from [get_registers {*hps_ddr*port2*skid}] \
    -to [get_registers {*ddr2_test*address?23?}] -npaths 1 \
    -detail full_path -show_routing -file [file join $output memory-exact.rpt]
report_timing -setup -from [get_registers {*display*col_s?0?}] \
    -to [get_registers {*display*ddr_ch_t?2?}] -npaths 1 \
    -detail full_path -show_routing -file [file join $output pixel-exact.rpt]
report_clocks -file [file join $output clocks.rpt]
report_exceptions -file [file join $output exceptions.rpt]
project_close
