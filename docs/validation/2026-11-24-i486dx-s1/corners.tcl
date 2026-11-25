# quartus_sta -t corners.tcl: method.md G3's worst slack at each corner, for a compiled seed.
# Run in the seed's project directory (golden_top) under a Pro slot of the Quartus lease.
# Prints one line per corner and analysis: CORNER <corner> <analysis> <paths> <worst slack>.
project_open golden_top
create_timing_netlist
read_sdc
update_timing_netlist
foreach_in_collection oc [get_available_operating_conditions] {
    set_operating_conditions $oc
    update_timing_netlist
    set name [get_operating_conditions_info $oc -display_name]
    foreach kind {setup hold recovery removal} {
        set r [report_timing -$kind -npaths 1 -panel_name "corner $name $kind"]
        puts "CORNER {$name} $kind [lindex $r 0] [lindex $r 1]"
    }
    set r [report_min_pulse_width -nworst 1 -panel_name "corner $name mpw"]
    puts "CORNER {$name} mpw [lindex $r 0] [lindex $r 1]"
}
delete_timing_netlist
project_close
