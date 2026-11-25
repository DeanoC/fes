# quartus_sta -t loaded_route.tcl REVISION OUT: the LOADED basis's flag route, in a fitted project.
# signoff_bases.LOADED leaves the route machine_presenter_loaded_flag -> machine_presenter_loaded_sync[0]
# (cut by a false path) whatever of 5 machine clocks the 50.4 ns pixel-side need does not use. This
# reports that cut path's data delay and clock skew at every corner, -false_path so the cut is shown.
set revision [lindex $argv 0]
set out [lindex $argv 1]
project_open $revision -revision $revision
create_timing_netlist
read_sdc
update_timing_netlist
set from [get_registers {*|system|machine_presenter_loaded_flag}]
set to [get_registers {*|system|machine_presenter_loaded_sync[0]}]
if {[get_collection_size $from] != 1 || [get_collection_size $to] != 1} { error "LOADED flag endpoints" }
set f [open $out w]
foreach condition [get_available_operating_conditions] {
    set_operating_conditions $condition
    update_timing_netlist
    foreach_in_collection p [get_timing_paths -setup -false_path -from $from -to $to -npaths 1] {
        puts $f "$condition\tdata_delay=[get_path_info $p -data_delay]\tskew=[get_path_info $p -clock_skew]"
    }
}
close $f
delete_timing_netlist
project_close
