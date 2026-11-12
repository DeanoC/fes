# Read all final fitted output paths without changing their constraints.
cd [lindex $quartus(args) 0]
set output [lindex $quartus(args) 1]
project_open golden_top
create_timing_netlist
read_sdc
update_timing_netlist
set ports {HDMI_TX_DE HDMI_TX_HS HDMI_TX_VS}
for {set bit 0} {$bit < 24} {incr bit} {lappend ports [format {HDMI_TX_D[%d]} $bit]}
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] != 4} {error "DE25 video requires four fitted operating models"}
set inventory [open [file join $output inventory.tsv] w]
set index 0
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set model [get_operating_conditions_info -display_name $corner]
    foreach port $ports {
        set node [get_ports $port]
        if {[get_collection_size $node] != 1} {error "missing unique video output $port"}
        foreach kind {setup hold} {
            set file [format {path-%03d.rpt} $index]
            set result [report_timing -$kind -to $node -npaths 1 -file [file join $output $file]]
            if {[lindex $result 0] != 1} {error "missing $kind path to $port in $model"}
            puts $inventory [join [list $file $model $port $kind [lindex $result 1]] "\t"]
            incr index
        }
    }
}
close $inventory
delete_timing_netlist
project_close
