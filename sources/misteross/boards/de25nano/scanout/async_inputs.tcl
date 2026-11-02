# Read the final fitted netlist. Do not add clocks, exceptions or toggle rates.
set project_dir [lindex $quartus(args) 0]
set output [lindex $quartus(args) 1]
cd $project_dir
project_open golden_top
create_timing_netlist
read_sdc
update_timing_netlist
proc names {collection} {
    set result {}
    foreach_in_collection node $collection {lappend result [get_node_info -name $node]}
    return [lsort $result]
}
set f [open [file join $output inventory.tsv] w]
foreach {port head} {HDMI_I2C_SDA hdmi_sda_sync HDMI_TX_INT hdmi_int_sync} {
    set input [get_ports $port]
    set zero [get_registers [format {fes_video_inst|%s[0]} $head]]
    set one [get_registers [format {fes_video_inst|%s[1]} $head]]
    foreach collection [list $input $zero $one] {
        if {[get_collection_size $collection] != 1} {error "missing unique HDMI input/stage"}
    }
    puts $f [join [list $port input-fanout [join [names [get_fanouts $input]] ,]] "\t"]
    puts $f [join [list $port stage0-fanout [join [names [get_fanouts $zero]] ,]] "\t"]
    puts $f [join [list $port stage1-fanin-synch [join [names [get_fanins $one -synch]] ,]] "\t"]
    puts $f [join [list $port stage1-fanout [join [names [get_fanouts $one]] ,]] "\t"]
    foreach_in_collection node $zero {puts $f [join [list $port stage0-location [get_node_info -location $node]] "\t"]}
    foreach_in_collection node $one {puts $f [join [list $port stage1-location [get_node_info -location $node]] "\t"]}
    report_timing -from $zero -to $one -setup -npaths 1 -file [file join $output $head-setup.rpt]
    report_timing -from $zero -to $one -hold -npaths 1 -file [file join $output $head-hold.rpt]
}
close $f
report_metastability -nchains 1000 -file [file join $output metastability.rpt]
delete_timing_netlist
project_close
