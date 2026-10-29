# One-slow-model mapped path check; production SDC and reports stay unchanged.
if {[llength $quartus(args)] != 2} {error "expected project.qpf and audit directory"}
set project [file normalize [lindex $quartus(args) 0]]
set output [file normalize [lindex $quartus(args) 1]]
file mkdir $output
cd [file dirname $project]
project_open [file rootname [file tail $project]]
create_timing_netlist -model slow
read_sdc
update_timing_netlist
foreach domain {config_reset pixel_reset} {
    set head [get_registers *${domain}*sync_regs?0?]
    set tail [get_registers *${domain}*sync_regs?1?]
    if {[get_collection_size $head] != 1 || [get_collection_size $tail] != 1} {
        error "expected two mapped registers for $domain"
    }
    report_timing -setup -from $head -to $tail -npaths 1 -detail full_path \
        -file [file join $output ${domain}-setup.rpt]
    report_timing -hold -from $head -to $tail -npaths 1 -detail full_path \
        -file [file join $output ${domain}-hold.rpt]
}
project_close
